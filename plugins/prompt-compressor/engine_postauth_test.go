package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
)

// recorded collects completed records.
type recorded struct{ reqs []records.Request }

func (r *recorded) Add(q records.Request) { r.reqs = append(r.reqs, q) }

// An engine that fails refuses the request with a 502 only under
// failure_mode closed in enforce mode; open and audit always forward.
func TestEngineFailureModesInPostAuth(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer down.Close()
	for _, tc := range []struct {
		mode, failure string
		blocked       bool
	}{
		{"enforce", "closed", true},
		{"enforce", "open", false},
		{"audit", "closed", false},
	} {
		t.Run(tc.mode+"/"+tc.failure, func(t *testing.T) {
			cfg := fmt.Sprintf(`{"mode":%q,"failure_mode":%q,"engine_main_url":%q,"default_policy":"p",
				"policies":{"p":{"level":1,"min_unit_tokens":0,"protect_recent_turns":0,"engine":{"name":"main","kind":"headroom","timeout_ms":500}}}}`,
				tc.mode, tc.failure, down.URL)
			p, ctx := hookPlugin(t, cfg)
			rec := &recorded{}
			p.SetRecorder(rec)
			resp, err := p.HandlePostAuth(ctx, request(noisyBody(t), nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.Block != tc.blocked {
				t.Fatalf("blocked %v: %+v", resp.Block, resp)
			}
			if tc.blocked {
				if resp.StatusCode != http.StatusBadGateway || len(rec.reqs) != 1 || rec.reqs[0].Outcome != records.OutcomeFailedClosed {
					t.Errorf("resp %d, records %+v", resp.StatusCode, rec.reqs)
				}
				if _, pending := p.pending.take("req-1"); pending {
					t.Error("a refused request left a pending record")
				}
				return
			}
			pend, _ := p.pending.take("req-1")
			if pend.Report.EngineFailed == 0 || pend.Outcome == OutcomeFailedClosed {
				t.Errorf("record %+v", pend)
			}
		})
	}
}
