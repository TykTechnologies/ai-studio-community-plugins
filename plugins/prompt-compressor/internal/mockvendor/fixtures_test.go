package mockvendor

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

// fixture is a single-turn live-suite task as testdata/live holds it.
type fixture struct {
	ID      string          `json:"id"`
	Format  string          `json:"format"`
	Request json.RawMessage `json:"request"`
	Expect  struct {
		Calls   []Call   `json:"calls"`
		Answers []string `json:"answers"`
	} `json:"expect"`
}

func loadFixtures(t *testing.T, suite string) ([]fixture, map[string]Task) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "live", suite)
	f, err := os.Open(filepath.Join(dir, "tasks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []fixture
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatalf("%s: %v", suite, err)
		}
		out = append(out, fx)
	}
	tasks, err := LoadOracle(filepath.Join(dir, "oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	return out, tasks
}

// correct reports whether the oracle's answer to body is the task's
// expected one.
func correct(t *testing.T, fx fixture, tasks map[string]Task, body []byte) bool {
	t.Helper()
	a, ok := answerFor(tasks, body)
	if !ok {
		t.Fatalf("%s: the oracle does not know the task", fx.ID)
	}
	if len(fx.Expect.Calls) > 0 {
		if a.Call == nil || a.Call.Name != fx.Expect.Calls[0].Name {
			return false
		}
		var got, want any
		_ = json.Unmarshal(a.Call.Arguments, &got)
		_ = json.Unmarshal(fx.Expect.Calls[0].Arguments, &want)
		return reflect.DeepEqual(got, want)
	}
	for _, w := range fx.Expect.Answers {
		if a.Text == w {
			return true
		}
	}
	return false
}

// The fixtures and the oracle agree: every single-turn task is answered
// right when its request reaches the vendor uncompressed and at level 1
// (lossless), and level 2 keeps exactly the evidence the reducers are meant
// to keep. Which tasks level 2 loses is pinned, so a reducer change that
// starts cutting evidence shows up here before a paid run.
func TestLiveFixturesAgainstTheOracle(t *testing.T) {
	c := compress.New(compress.Options{})
	at := func(fx fixture, level int) []byte {
		p := config.DefaultPolicy()
		p.Level = level
		out, rep := c.Compress(compress.Input{Format: fx.Format, Body: fx.Request, Policy: p})
		if rep.Outcome == compress.OutcomeRejected {
			t.Fatalf("%s level %d: rejected", fx.ID, level)
		}
		return out
	}
	// Tasks level 2 is expected to lose: their evidence sits in the middle
	// of a long JSON array (bfcl) or on an INFO line of a build log (lcqa).
	lostAtL2 := map[string]bool{"bfcl-003": true, "bfcl-004": true, "bfcl-006": true, "lcqa-002": true, "lcqa-004": true}
	for _, suite := range []string{"bfcl", "lcqa"} {
		fxs, tasks := loadFixtures(t, suite)
		if len(fxs) == 0 {
			t.Fatalf("%s: no tasks", suite)
		}
		for _, fx := range fxs {
			if !correct(t, fx, tasks, fx.Request) {
				t.Errorf("%s: wrong at level 0", fx.ID)
			}
			if !correct(t, fx, tasks, at(fx, 1)) {
				t.Errorf("%s: wrong at level 1 (lossless)", fx.ID)
			}
			if got := correct(t, fx, tasks, at(fx, 2)); got == lostAtL2[fx.ID] {
				t.Errorf("%s: correct at level 2 = %v, want %v", fx.ID, got, !lostAtL2[fx.ID])
			}
		}
	}
}
