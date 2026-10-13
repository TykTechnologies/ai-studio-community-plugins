package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/ingestion"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/storage"
	"github.com/TykTechnologies/midsommar/v2/community/plugins/github-rag-ingest/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

func newCancelHandler(t *testing.T) (*Handler, *storage.JobStore, *ingestion.Engine) {
	t.Helper()
	kv := storage.NewKVStore(&memKV{data: map[string][]byte{}})
	jobs := storage.NewJobStore(kv)
	repos := storage.NewRepositoryStore(kv)
	engine := ingestion.NewEngine(nil, nil, jobs, repos)
	return NewHandler(repos, jobs, nil, nil, nil, engine), jobs, engine
}

func cancel(t *testing.T, h *Handler, id string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"id": id})
	out, err := h.CancelJob(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// A job left "running" by a plugin restart is not running in this engine:
// CancelJob records it as cancelled directly.
func TestCancelJobLeftoverRunningJob(t *testing.T) {
	h, jobs, _ := newCancelHandler(t)
	job := types.NewJob("repo-1", types.JobTypeFull, types.TriggerManual, false)
	job.Status = types.JobStatusRunning
	if err := jobs.Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	if out := cancel(t, h, job.ID); !strings.Contains(out, "job cancelled") {
		t.Errorf("response = %s", out)
	}
	stored, _ := jobs.Get(context.Background(), job.ID)
	if stored.Status != types.JobStatusCancelled {
		t.Errorf("stored status = %s, want cancelled", stored.Status)
	}
}

// A queued job cancelled before its goroutine starts is recorded as
// cancelled, and Run then refuses to start it.
func TestCancelJobQueuedJobNeverStarts(t *testing.T) {
	h, jobs, engine := newCancelHandler(t)
	job := types.NewJob("repo-1", types.JobTypeFull, types.TriggerManual, false)
	if err := jobs.Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	cancel(t, h, job.ID)
	if err := engine.Run(context.Background(), job); !errors.Is(err, ingestion.ErrJobCancelled) {
		t.Errorf("Run after cancel = %v, want ErrJobCancelled", err)
	}

	// Finished jobs cannot be cancelled
	if out := cancel(t, h, job.ID); !strings.Contains(out, "not running") {
		t.Errorf("cancel of a finished job: %s", out)
	}
}
