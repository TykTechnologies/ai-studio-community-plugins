package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
)

func initializedPlugin(t *testing.T, runtime plugin_sdk.RuntimeType) *LLMCachePlugin {
	t.Helper()
	p := NewLLMCachePlugin()
	if err := p.Initialize(plugin_sdk.Context{Runtime: runtime}, map[string]string{}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return p
}

// shutdownWithin fails the test when Shutdown does not return in time, which
// means a background loop ignored the stop signal.
func shutdownWithin(t *testing.T, p *LLMCachePlugin, d time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.Shutdown(plugin_sdk.Context{}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(d):
		t.Fatal("Shutdown did not return: a background loop is still running")
	}
}

func TestShutdownStopsBackgroundLoopsAndIsIdempotent(t *testing.T) {
	p := initializedPlugin(t, plugin_sdk.RuntimeStudio)
	// The gateway host initialises twice; the cleanup loop must start once
	if err := p.Initialize(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio}, map[string]string{}); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
	// The edge stats reporter (started by OnSessionReady on an edge, which
	// needs a broker) runs on the same lifecycle
	p.reporterOnce.Do(func() { p.goBackground(p.reportStatsToControl) })

	p.bgMu.Lock()
	started := p.bgStarted
	p.bgMu.Unlock()
	if started != 2 {
		t.Fatalf("%d background loops started, want 2 (one cleanup, one reporter)", started)
	}

	shutdownWithin(t, p, 2*time.Second)
	if p.bgCtx.Err() == nil {
		t.Fatal("background context not cancelled by Shutdown")
	}
	// A second Shutdown must not panic (it used to close a closed channel) or block
	shutdownWithin(t, p, 2*time.Second)
}

func TestShutdownWaitsForBackgroundLoops(t *testing.T) {
	p := initializedPlugin(t, plugin_sdk.RuntimeStudio)

	var finished atomic.Bool
	p.goBackground(func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(100 * time.Millisecond) // a loop finishing its last tick
		finished.Store(true)
	})

	shutdownWithin(t, p, 2*time.Second)
	if !finished.Load() {
		t.Fatal("Shutdown returned before a background loop finished")
	}
}

func TestNoBackgroundLoopStartsAfterShutdown(t *testing.T) {
	p := initializedPlugin(t, plugin_sdk.RuntimeStudio)
	shutdownWithin(t, p, 2*time.Second)

	started := make(chan struct{}, 1)
	p.goBackground(func(ctx context.Context) { started <- struct{}{} })
	select {
	case <-started:
		t.Fatal("goBackground started a loop after Shutdown")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCleanupDropsOldClearOperations(t *testing.T) {
	p := initializedPlugin(t, plugin_sdk.RuntimeStudio)
	defer shutdownWithin(t, p, 2*time.Second)

	now := time.Now().Unix()
	p.clearOperations["old"] = &ClearCacheOperation{OperationID: "old", StartTime: now - 301, AckEdges: map[string]int64{}}
	p.clearOperations["recent"] = &ClearCacheOperation{OperationID: "recent", StartTime: now - 10, AckEdges: map[string]int64{}}

	p.cleanup()

	if _, ok := p.clearOperations["old"]; ok {
		t.Error("clear operation older than 5 minutes was kept")
	}
	if _, ok := p.clearOperations["recent"]; !ok {
		t.Error("recent clear operation was dropped")
	}
}

func TestFallbackEdgeIDIsStableAndUnique(t *testing.T) {
	t.Setenv("EDGE_ID", "")
	a, b := NewLLMCachePlugin(), NewLLMCachePlugin()

	if a.getEdgeID() != a.getEdgeID() {
		t.Error("fallback edge ID changes between calls")
	}
	if a.getEdgeID() == b.getEdgeID() {
		t.Errorf("two plugin instances share fallback edge ID %q", a.getEdgeID())
	}

	t.Setenv("EDGE_ID", " edge-eu-1 ")
	if got := a.getEdgeID(); got != "edge-eu-1" {
		t.Errorf("getEdgeID() = %q, want EDGE_ID from the environment", got)
	}
}
