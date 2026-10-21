package eval

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/mockvendor"
)

// The live suites end to end with no stack and no spend: the committed
// fixtures (testdata/live) through the runner, an in-process gateway that
// compresses per slug as the edge does, and the oracle mock. Levels 0 and 1
// solve every task; level 2 loses exactly the tasks whose evidence sits
// where its reducers cut (the same pins as mockvendor's fixture test), and
// sends fewer input tokens. This is the dry run CI does on every change.
func TestLiveSuitesDryRunOnTheFixtures(t *testing.T) {
	lostAtL2 := map[string]bool{"bfcl-003": true, "bfcl-004": true, "bfcl-006": true, "lcqa-002": true, "lcqa-004": true}
	for _, suite := range []string{"bfcl", "lcqa", "swebench"} {
		t.Run(suite, func(t *testing.T) {
			dir := filepath.Join("..", "..", "testdata", "live", suite)
			tasks, err := LoadTasks(filepath.Join(dir, "tasks.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			oracle, err := mockvendor.LoadOracle(filepath.Join(dir, "oracle.json"))
			if err != nil {
				t.Fatal(err)
			}
			l1, l2 := config.DefaultPolicy(), config.DefaultPolicy()
			l1.Level, l2.Level = 1, 2
			srv := httptest.NewServer(CompressingGateway(mockvendor.New(mockvendor.Options{Oracle: oracle}), map[string]*config.Policy{"l1": l1, "l2": l2}))
			t.Cleanup(srv.Close)
			g := Gateway{BaseURL: srv.URL, Slugs: map[int]string{0: "off", 1: "l1", 2: "l2"}}
			ctx := context.Background()
			if err := g.Preflight(ctx, tasks[0].Format, true); err != nil {
				t.Fatalf("preflight against the oracle: %v", err)
			}

			byID := map[string]Task{}
			var ids []string
			for _, tk := range tasks {
				byID[tk.ID] = tk
				ids = append(ids, tk.ID)
			}
			pl, err := NewPlan(Suites[suite], ids, len(ids), 1, []int{1, 2}, tasks[0].Format)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Run(ctx, g, pl, byID, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range res {
				want := 100.0
				if r.Level == 2 && lostAtL2[r.Task] {
					want = 0
				}
				if r.Error != "" || r.Score != want {
					t.Errorf("%s level %d seed %d: score %v (error %q), want %v", r.Task, r.Level, r.Seed, r.Score, r.Error, want)
				}
			}
			rep := Summarise(pl, res, DefaultPrices()[tasks[0].Format], true)
			if !rep.DryRun || len(rep.Comparisons) != 2 {
				t.Fatalf("report: dry run %v, %d comparisons", rep.DryRun, len(rep.Comparisons))
			}
			for _, c := range rep.Comparisons {
				if c.Level == 2 && c.InputChange >= 0 {
					t.Errorf("level 2 input change %.3f, want fewer tokens than off", c.InputChange)
				}
			}
		})
	}
}

// A dry run refuses an upstream that is not the oracle mock, so no
// misconfigured slug can reach a paid model.
func TestLiveDryRunRefusesANonOracleUpstream(t *testing.T) {
	srv := httptest.NewServer(CompressingGateway(mockvendor.New(mockvendor.Options{}), nil))
	t.Cleanup(srv.Close)
	g := Gateway{BaseURL: srv.URL, Slugs: map[int]string{0: "off", 2: "l2"}}
	if err := g.Preflight(context.Background(), "openai", true); err == nil {
		t.Fatal("preflight accepted an echo mock for a dry run")
	}
}
