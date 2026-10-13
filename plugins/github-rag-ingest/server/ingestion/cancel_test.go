package ingestion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
	"github.com/go-git/go-billy/v5/memfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// memKV is a concurrency-safe in-memory plugin_sdk.KVService.
type memKV struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memKV) Read(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, status.Error(codes.NotFound, "key not found")
	}
	return v, nil
}

func (m *memKV) Write(_ context.Context, key string, value []byte, _ *time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return true, nil
}

func (m *memKV) WriteWithTTL(ctx context.Context, key string, value []byte, _ time.Duration) (bool, error) {
	return m.Write(ctx, key, value, nil)
}

func (m *memKV) Delete(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return true, nil
}

func (m *memKV) List(context.Context, string) ([]string, error) { return nil, nil }

// memRepoSource serves an in-memory repository with n markdown files.
type memRepoSource struct{ repo *gogit.Repository }

func newMemRepoSource(t *testing.T, files int) *memRepoSource {
	t.Helper()
	repo, err := gogit.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	for i := 0; i < files; i++ {
		f, err := wt.Filesystem.Create(fmt.Sprintf("docs/file%03d.md", i))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(f, "# File %d\n\nSome documentation text for file %d.\n", i, i)
		f.Close()
	}
	if err := wt.AddGlob("docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	return &memRepoSource{repo: repo}
}

func (s *memRepoSource) CloneOrFetch(context.Context, *types.Repository, *storage.Secret) (*gogit.Repository, error) {
	return s.repo, nil
}

func (s *memRepoSource) GetHead(gitRepo *gogit.Repository, _ string) (string, error) {
	ref, err := gitRepo.Head()
	if err != nil {
		return "", err
	}
	return ref.Hash().String(), nil
}

// fakeChunks records stored batches and runs onStore on each one.
type fakeChunks struct {
	mu      sync.Mutex
	batches int
	onStore func(batch int)
}

func (f *fakeChunks) ProcessAndStoreDocuments(ctx context.Context, _ uint32, chunks []*mgmtpb.DocumentChunk) (*mgmtpb.ProcessAndStoreResponse, error) {
	f.mu.Lock()
	f.batches++
	n := f.batches
	f.mu.Unlock()
	if f.onStore != nil {
		f.onStore(n)
	}
	return &mgmtpb.ProcessAndStoreResponse{Success: true, ProcessedCount: int32(len(chunks))}, nil
}

func (f *fakeChunks) DeleteDocumentsByMetadata(context.Context, uint32, map[string]string, string, bool) (int32, error) {
	return 0, nil
}

func (f *fakeChunks) stored() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.batches
}

type fixture struct {
	engine *Engine
	chunks *fakeChunks
	jobs   *storage.JobStore
	repos  *storage.RepositoryStore
	repo   *types.Repository
}

func newFixture(t *testing.T, files int) *fixture {
	t.Helper()
	kv := storage.NewKVStore(&memKV{data: map[string][]byte{}})
	f := &fixture{
		chunks: &fakeChunks{},
		jobs:   storage.NewJobStore(kv),
		repos:  storage.NewRepositoryStore(kv),
		repo:   &types.Repository{Name: "docs", URL: "https://example.com/org/docs.git", Branch: "master", DatasourceID: 7, ChunkingStrategy: types.ChunkingStrategySimple, ChunkSize: 1000, MaxFileSizeKB: 100},
	}
	if err := f.repos.Create(context.Background(), f.repo); err != nil {
		t.Fatal(err)
	}
	f.engine = newEngine(newMemRepoSource(t, files), f.chunks, nil, f.jobs, f.repos)
	return f
}

func (f *fixture) newJob(t *testing.T) *types.Job {
	t.Helper()
	job := types.NewJob(f.repo.ID, types.JobTypeFull, types.TriggerManual, false)
	if err := f.jobs.Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestRunCompletes(t *testing.T) {
	f := newFixture(t, 120)
	job := f.newJob(t)
	if err := f.engine.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if job.Status != types.JobStatusSuccess || f.chunks.stored() < 2 {
		t.Fatalf("status %s after %d batches", job.Status, f.chunks.stored())
	}
	repo, _ := f.repos.Get(context.Background(), f.repo.ID)
	if repo.LastSyncCommit == "" {
		t.Error("a completed sync did not record its commit")
	}
}

// Cancelling mid-run stops before the next batch, records the job as
// cancelled, and leaves the last-synced commit alone.
func TestCancelStopsRunningJob(t *testing.T) {
	f := newFixture(t, 120) // > 2 batches of 50
	job := f.newJob(t)
	f.chunks.onStore = func(batch int) {
		if batch == 1 && !f.engine.Cancel(job.ID) {
			t.Error("Cancel did not find the running job")
		}
	}

	err := f.engine.Run(context.Background(), job)
	if !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("Run = %v, want ErrJobCancelled", err)
	}
	if n := f.chunks.stored(); n != 1 {
		t.Errorf("%d batches stored, want the run to stop after the first", n)
	}

	stored, _ := f.jobs.Get(context.Background(), job.ID)
	if stored.Status != types.JobStatusCancelled || !strings.Contains(stored.ErrorMessage, "cancelled by user") {
		t.Errorf("stored job status %q (%q), want cancelled by user", stored.Status, stored.ErrorMessage)
	}
	repo, _ := f.repos.Get(context.Background(), f.repo.ID)
	if repo.LastSyncCommit != "" {
		t.Errorf("a cancelled sync advanced the last-synced commit to %s", repo.LastSyncCommit)
	}
}

// A job cancelled before Run picks it up never starts.
func TestCancelBeforeStart(t *testing.T) {
	f := newFixture(t, 5)
	job := f.newJob(t)
	if f.engine.Cancel(job.ID) {
		t.Fatal("Cancel reported a job that is not running")
	}
	if err := f.engine.Run(context.Background(), job); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("Run = %v, want ErrJobCancelled", err)
	}
	if f.chunks.stored() != 0 {
		t.Error("a job cancelled before it started stored chunks")
	}
}

// Cancelling a finished job does nothing; a host cancelling the context is
// recorded as stopped by the host.
func TestCancelFinishedJobAndHostCancel(t *testing.T) {
	f := newFixture(t, 5)
	job := f.newJob(t)
	if err := f.engine.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if f.engine.Cancel(job.ID) {
		t.Error("Cancel reported a finished job as running")
	}

	job2 := f.newJob(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.engine.Run(ctx, job2); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("Run with a cancelled context = %v", err)
	}
	stored, _ := f.jobs.Get(context.Background(), job2.ID)
	if stored.Status != types.JobStatusCancelled || !strings.Contains(stored.ErrorMessage, "stopped by the host") {
		t.Errorf("stored job %q (%q), want stopped by the host", stored.Status, stored.ErrorMessage)
	}
}
