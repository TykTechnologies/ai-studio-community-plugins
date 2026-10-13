package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
)

// flakyKV fails reads or writes of chosen keys.
type flakyKV struct {
	*fakeKV
	failRead  map[string]bool
	failWrite map[string]bool
}

var errUnavailable = errors.New("kv unavailable")

func (f *flakyKV) Read(ctx context.Context, key string) ([]byte, error) {
	if f.failRead[key] {
		return nil, errUnavailable
	}
	return f.fakeKV.Read(ctx, key)
}

func (f *flakyKV) Write(ctx context.Context, key string, value []byte, expireAt *time.Time) (bool, error) {
	if f.failWrite[key] {
		return false, errUnavailable
	}
	return f.fakeKV.Write(ctx, key, value, expireAt)
}

func newFlakyJobStore() (*JobStore, *flakyKV) {
	kv := &flakyKV{fakeKV: newFakeKV(), failRead: map[string]bool{}, failWrite: map[string]bool{}}
	return NewJobStore(NewKVStore(kv)), kv
}

func TestJobStoreCreateIndexesJob(t *testing.T) {
	store, _ := newFlakyJobStore()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := store.Create(ctx, &types.Job{RepoID: "repo-1"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, total, _ := store.ListByRepo(ctx, "repo-1", 10, 0); total != 2 {
		t.Errorf("repo index has %d jobs, want 2", total)
	}
	if _, total, _ := store.ListAll(ctx, 10, 0); total != 2 {
		t.Errorf("global index has %d jobs, want 2", total)
	}
}

// An index that fails to load must not be overwritten with just the new ID,
// and the job must not be left behind unindexed.
func TestJobStoreCreateFailsWhenIndexUnreadable(t *testing.T) {
	store, kv := newFlakyJobStore()
	ctx := context.Background()
	if err := store.Create(ctx, &types.Job{ID: "old", RepoID: "repo-1"}); err != nil {
		t.Fatal(err)
	}

	kv.failRead[jobsIndexKey] = true
	err := store.Create(ctx, &types.Job{ID: "new", RepoID: "repo-1"})
	if !errors.Is(err, errUnavailable) || !strings.Contains(err.Error(), "new") {
		t.Fatalf("Create = %v, want the index read error naming the job", err)
	}
	if _, ok := kv.data[jobKeyPrefix+"new"]; ok {
		t.Error("job left stored after its indexing failed")
	}

	kv.failRead[jobsIndexKey] = false
	jobs, total, _ := store.ListAll(ctx, 10, 0)
	if total != 1 || len(jobs) != 1 || jobs[0].ID != "old" {
		t.Errorf("global index = %d jobs %v, want only the existing job", total, jobs)
	}
}

func TestJobStoreCreateFailsWhenIndexUnwritable(t *testing.T) {
	store, kv := newFlakyJobStore()
	kv.failWrite[jobsByRepoPrefix+"repo-1"] = true
	err := store.Create(context.Background(), &types.Job{ID: "j1", RepoID: "repo-1"})
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("Create = %v, want the index write error", err)
	}
	if _, ok := kv.data[jobKeyPrefix+"j1"]; ok {
		t.Error("job left stored after its indexing failed")
	}
}

func TestJobStoreAddLogKeepsLogsWhenReadFails(t *testing.T) {
	store, kv := newFlakyJobStore()
	ctx := context.Background()
	if err := store.AddLog(ctx, &types.JobLog{JobID: "j1", Message: "first"}); err != nil {
		t.Fatal(err)
	}

	logsKey := jobLogsKeyPrefix + "j1" + jobLogsKeySuffix
	kv.failRead[logsKey] = true
	if err := store.AddLog(ctx, &types.JobLog{JobID: "j1", Message: "second"}); !errors.Is(err, errUnavailable) {
		t.Fatalf("AddLog = %v, want the read error", err)
	}

	kv.failRead[logsKey] = false
	logs, total, _ := store.GetLogs(ctx, "j1", "", 10, 0)
	if total != 1 || logs[0].Message != "first" {
		t.Errorf("logs = %v, want the first entry kept", logs)
	}
}

func TestIsNotFound(t *testing.T) {
	_, err := newFakeKV().Read(context.Background(), "missing")
	if !IsNotFound(err) {
		t.Errorf("gRPC NotFound not recognised: %v", err)
	}
	if !IsNotFound(ErrNotFound) {
		t.Error("ErrNotFound not recognised")
	}
	if IsNotFound(errUnavailable) || IsNotFound(nil) {
		t.Error("other errors treated as not found")
	}
}
