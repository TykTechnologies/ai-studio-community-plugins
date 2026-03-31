package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestBucketEpoch(t *testing.T) {
	// 2024-04-01 00:01:30 UTC → window=60 → bucket starts at 00:01:00
	ts := time.Date(2024, 4, 1, 0, 1, 30, 0, time.UTC)
	epoch := BucketEpoch(ts, 60)
	expected := time.Date(2024, 4, 1, 0, 1, 0, 0, time.UTC).Unix()
	if epoch != expected {
		t.Errorf("expected %d, got %d", expected, epoch)
	}
}

func TestBucketEpoch_Aligned(t *testing.T) {
	// Exactly on boundary
	ts := time.Date(2024, 4, 1, 0, 2, 0, 0, time.UTC)
	epoch := BucketEpoch(ts, 60)
	if epoch != ts.Unix() {
		t.Errorf("expected %d, got %d", ts.Unix(), epoch)
	}
}

func TestPreviousBucketEpoch(t *testing.T) {
	ts := time.Date(2024, 4, 1, 0, 1, 30, 0, time.UTC)
	prev := PreviousBucketEpoch(ts, 60)
	expected := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC).Unix()
	if prev != expected {
		t.Errorf("expected %d, got %d", expected, prev)
	}
}

func TestSlidingWindowCount_MidWindow(t *testing.T) {
	// 25 seconds into 60s window
	// Previous: 100, Current: 20
	// Weight = (60-25)/60 = 0.583...
	// Effective = int(100 * 0.583) + 20 = 58 + 20 = 78
	base := time.Date(2024, 4, 1, 0, 1, 0, 0, time.UTC) // bucket start
	now := base.Add(25 * time.Second)                      // 25s in

	count := SlidingWindowCount(100, 20, now, 60)
	if count != 78 {
		t.Errorf("expected 78, got %d", count)
	}
}

func TestSlidingWindowCount_StartOfWindow(t *testing.T) {
	// At t=0 of current window, previous contributes 100%
	base := time.Date(2024, 4, 1, 0, 1, 0, 0, time.UTC)
	now := base // exactly at boundary

	count := SlidingWindowCount(100, 20, now, 60)
	// weight = (60-0)/60 = 1.0 → int(100*1.0) + 20 = 120
	if count != 120 {
		t.Errorf("expected 120, got %d", count)
	}
}

func TestSlidingWindowCount_EndOfWindow(t *testing.T) {
	// At t=59 of current window, previous contributes ~1.7%
	base := time.Date(2024, 4, 1, 0, 1, 0, 0, time.UTC)
	now := base.Add(59 * time.Second)

	count := SlidingWindowCount(100, 20, now, 60)
	// weight = (60-59)/60 = 0.0166... → int(100*0.0166) + 20 = 1 + 20 = 21
	if count != 21 {
		t.Errorf("expected 21, got %d", count)
	}
}

func TestSlidingWindowCount_NoPrevious(t *testing.T) {
	base := time.Date(2024, 4, 1, 0, 1, 0, 0, time.UTC)
	now := base.Add(30 * time.Second)

	count := SlidingWindowCount(0, 50, now, 60)
	if count != 50 {
		t.Errorf("expected 50, got %d", count)
	}
}

func TestSlidingWindowCount_NoCurrent(t *testing.T) {
	base := time.Date(2024, 4, 1, 0, 1, 0, 0, time.UTC)
	now := base.Add(30 * time.Second)

	count := SlidingWindowCount(100, 0, now, 60)
	// weight = (60-30)/60 = 0.5 → int(100*0.5) + 0 = 50
	if count != 50 {
		t.Errorf("expected 50, got %d", count)
	}
}

func TestSlidingWindowCount_LargeWindow(t *testing.T) {
	// 300s window, 150s in
	base := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	now := base.Add(150 * time.Second)

	count := SlidingWindowCount(200, 100, now, 300)
	// weight = (300-150)/300 = 0.5 → int(200*0.5) + 100 = 200
	if count != 200 {
		t.Errorf("expected 200, got %d", count)
	}
}

// --- In-memory store for testing ---

type memStore struct {
	data map[string][]byte
}

func newMemStore() *memStore {
	return &memStore{data: make(map[string][]byte)}
}

func (s *memStore) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := s.data[key]
	if !ok {
		return nil, ErrKeyNotFound
	}
	return v, nil
}

func (s *memStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.data[key] = value
	return nil
}

func (s *memStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func (s *memStore) IncrementIfBelow(_ context.Context, key string, limit int, _ time.Duration) (int, bool, error) {
	state := ConcurrentState{}
	if data, ok := s.data[key]; ok {
		json.Unmarshal(data, &state)
	}
	if state.Count >= limit {
		return state.Count, false, nil
	}
	preCount := state.Count
	state.Count++
	state.UpdatedAt = time.Now().Unix()
	out, _ := json.Marshal(state)
	s.data[key] = out
	return preCount, true, nil
}

func (s *memStore) DecrementCounter(_ context.Context, key string, _ time.Duration) (int, error) {
	state := ConcurrentState{}
	if data, ok := s.data[key]; ok {
		json.Unmarshal(data, &state)
	}
	state.Count--
	if state.Count < 0 {
		state.Count = 0
	}
	state.UpdatedAt = time.Now().Unix()
	out, _ := json.Marshal(state)
	s.data[key] = out
	return state.Count, nil
}

// EvalSlidingWindow returns the effective count BEFORE the increment,
// matching the semantics of the Redis Lua script and kvStore.
func (s *memStore) EvalSlidingWindow(_ context.Context, currentKey, previousKey string, windowSeconds int, nowUnix int64, incrDelta int, _ time.Duration) (int, error) {
	prevCount := 0
	if data, ok := s.data[previousKey]; ok {
		var st WindowState
		if json.Unmarshal(data, &st) == nil {
			prevCount = st.Count
		}
	}
	curCount := 0
	if data, ok := s.data[currentKey]; ok {
		var st WindowState
		if json.Unmarshal(data, &st) == nil {
			curCount = st.Count
		}
	}

	effective := SlidingWindowCountFromUnix(prevCount, curCount, nowUnix, windowSeconds)

	if incrDelta != 0 {
		curCount += incrDelta
		out, _ := json.Marshal(WindowState{Count: curCount, UpdatedAt: nowUnix})
		s.data[currentKey] = out
	}
	return effective, nil
}

func TestReadWriteWindowState(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	// Read missing key → zero state
	state := ReadWindowState(ctx, store, "test")
	if state.Count != 0 {
		t.Errorf("expected 0, got %d", state.Count)
	}

	// Write and read back
	state = WindowState{Count: 42, UpdatedAt: 1000}
	err := WriteWindowState(ctx, store, "test", state, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	got := ReadWindowState(ctx, store, "test")
	if got.Count != 42 || got.UpdatedAt != 1000 {
		t.Errorf("expected {42, 1000}, got %+v", got)
	}
}

func TestReadWriteConcurrentState(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	state := ReadConcurrentState(ctx, store, "test")
	if state.Count != 0 {
		t.Errorf("expected 0, got %d", state.Count)
	}

	state = ConcurrentState{Count: 5, UpdatedAt: 2000}
	err := WriteConcurrentState(ctx, store, "test", state, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	got := ReadConcurrentState(ctx, store, "test")
	if got.Count != 5 {
		t.Errorf("expected 5, got %d", got.Count)
	}
}

func TestReadWriteRequestState(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	_, err := ReadRequestState(ctx, store, "missing")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("expected ErrKeyNotFound, got %v", err)
	}

	rs := &RequestState{
		TokenRuleKeys: []TokenRuleRef{{RuleID: "r1", DimensionKey: "app_id:42"}},
		ConcRuleKeys:  []string{"rl:c:r2:app_id:42"},
		BucketEpoch:   1711900800,
		Timestamp:     1711900825,
	}
	err = WriteRequestState(ctx, store, "req1", rs, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequestState(ctx, store, "req1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TokenRuleKeys) != 1 || got.TokenRuleKeys[0].RuleID != "r1" {
		t.Errorf("unexpected token rule keys: %+v", got.TokenRuleKeys)
	}
	if len(got.ConcRuleKeys) != 1 {
		t.Errorf("unexpected conc keys: %+v", got.ConcRuleKeys)
	}
}
