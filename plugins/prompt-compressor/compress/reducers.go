package compress

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Level 2: reducers for tool output shapes a model mostly does not need in
// full. Each is a detector (does this text look like pytest output?) and a
// deterministic line filter that drops the lines a model can do without
// (passing tests, progress, downloads) and keeps every failure with its
// message and location, the totals, and every line a protect pattern
// matches. A cut leaves an inline marker (see marker), so the model knows
// text is missing.
//
// Reducers are ordered (the first that detects its shape wins) and
// versioned. The registry's fingerprint is pinned with config.StrategyVersion
// in a test, so a change to any reducer that is not followed by a strategy
// version bump (and so a new policy hash) fails the build.

// reducer is one level-2 reducer.
type reducer struct {
	name    string // also the marker's "<name> output"
	version int
	// detect reports whether text is this reducer's shape.
	detect func(lines []string) bool
	// drop reports whether a line can go. state carries what the reducer
	// has seen so far (a section, a suite's result).
	drop func(st *reducerState, line string) bool
	// failure reports whether a line marks one failure, for the marker.
	failure func(line string) bool
	// plan, when set, replaces drop and failure: it sees every line at
	// once (folds, caps per file, context windows).
	plan func(lines []string) cutPlan
	// keepErrors keeps every line that mentions an error, failure, panic or
	// exception, whatever the plan says.
	keepErrors bool
}

type reducerState struct {
	section string
}

// cutPlan is what a reducer drops. notes give a run of dropped lines its
// own marker ("previous line repeated 40 more times"); a note applies only
// when the run it starts is cut whole. failures < 0 leaves the count out of
// the marker.
type cutPlan struct {
	drop     []bool
	notes    map[int]note
	failures int
}

type note struct {
	n    int
	text string
}

// errorLine reports a line that mentions an error, failure, panic or
// exception, in any case.
var errorLine = regexp.MustCompile(`(?i)error|fail|panic|exception|fatal`)

// registry is the level-2 reducers, in order.
var registry = []reducer{
	pytestReducer, jestReducer, goTestReducer, cargoReducer, mavenReducer, gradleReducer,
	terraformReducer, kubectlReducer, gitDiffReducer, grepReducer, listingReducer, buildLogReducer,
	stackTraceReducer, repeatedReducer,
}

// registryFingerprint names every reducer and version, in order, then the
// JSON reducer (reducers_json.go) with its cut steps.
func registryFingerprint() string {
	parts := make([]string, len(registry), len(registry)+1)
	for i, r := range registry {
		parts[i] = r.name + "@" + strconv.Itoa(r.version)
	}
	parts = append(parts, fmt.Sprintf("%s@%d%v/d%d", jsonReducerName, jsonReducerVersion, jsonCuts, maxJSONReduceDepth))
	return strings.Join(parts, ",")
}

// minReducerLines is the smallest output a reducer is tried on.
const minReducerLines = 8

// reduce runs the first reducer that detects text's shape. keep reports
// lines that must survive whatever the reducer says (protect patterns). It
// returns text unchanged (and "") when no reducer applies or nothing would
// be cut.
func reduce(text string, keep func(string) bool) (string, string) {
	lines := strings.Split(text, "\n")
	if len(lines) < minReducerLines {
		return text, ""
	}
	for _, r := range registry {
		if !r.detect(lines) {
			continue
		}
		out, ok := r.apply(text, lines, keep)
		if !ok {
			return text, ""
		}
		return out, r.name
	}
	return text, ""
}

// apply drops the reducer's lines, replacing the first run of dropped lines
// with the full marker and each later run with a short one.
func (r reducer) apply(text string, lines []string, keep func(string) bool) (string, bool) {
	var pl cutPlan
	if r.plan != nil {
		pl = r.plan(lines)
	} else {
		st := &reducerState{}
		pl.drop = make([]bool, len(lines))
		for i, line := range lines {
			if r.failure(line) {
				pl.failures++
			}
			pl.drop[i] = r.drop(st, line)
		}
	}
	dropped := pl.drop
	cut := 0
	for i, line := range lines {
		if !dropped[i] {
			continue
		}
		if (keep != nil && keep(line)) || (r.keepErrors && errorLine.MatchString(line)) {
			dropped[i] = false
			continue
		}
		cut++
	}
	if cut == 0 {
		return text, false
	}
	var kept strings.Builder
	type run struct{ at, n int }
	var runs []run
	for i, line := range lines {
		if dropped[i] {
			if len(runs) == 0 || runs[len(runs)-1].at+runs[len(runs)-1].n != i {
				runs = append(runs, run{at: i})
			}
			runs[len(runs)-1].n++
			continue
		}
		kept.WriteString(line)
		kept.WriteByte('\n')
	}
	after := selectionTokens(kept.String())
	var b strings.Builder
	b.Grow(kept.Len() + 128)
	next := 0
	for ri, rn := range runs {
		for ; next < rn.at; next++ {
			if !dropped[next] {
				b.WriteString(lines[next])
				b.WriteByte('\n')
			}
		}
		nt, hasNote := pl.notes[rn.at]
		hasNote = hasNote && nt.n == rn.n
		switch {
		case ri == 0 && hasNote:
			b.WriteString(marker(r.name, selectionTokens(text), after, pl.failures))
			b.WriteByte('\n')
			b.WriteString(nt.text)
		case ri == 0:
			b.WriteString(marker(r.name, selectionTokens(text), after, pl.failures))
		case hasNote:
			b.WriteString(nt.text)
		default:
			b.WriteString(shortMarker(rn.n))
		}
		if rn.at+rn.n < len(lines) {
			b.WriteByte('\n')
		}
		next = rn.at + rn.n
	}
	for ; next < len(lines); next++ {
		b.WriteString(lines[next])
		if next < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	out := b.String()
	if len(out) >= len(text) {
		return text, false
	}
	return out, true
}

// marker is the inline marker a cut leaves, in a fixed format:
//
//	[tyk-compressed: pytest output, 18,400 → 1,150 tokens; 3 failures kept]
//
// Tokens are UTF-8 bytes over four, so the marker depends only on the text.
// A reducer without failures to count leaves the last part out.
func marker(name string, before, after, failures int) string {
	if failures < 0 {
		return fmt.Sprintf("[tyk-compressed: %s output, %s → %s tokens]", name, thousands(before), thousands(after))
	}
	noun := "failures"
	if failures == 1 {
		noun = "failure"
	}
	return fmt.Sprintf("[tyk-compressed: %s output, %s → %s tokens; %d %s kept]", name, thousands(before), thousands(after), failures, noun)
}

// shortMarker marks a later cut in the same output: [tyk-compressed: 12 lines].
func shortMarker(lines int) string {
	if lines == 1 {
		return "[tyk-compressed: 1 line]"
	}
	return fmt.Sprintf("[tyk-compressed: %d lines]", lines)
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// countMatches counts the lines any pattern matches.
func countMatches(lines []string, res ...*regexp.Regexp) int {
	n := 0
	for _, l := range lines {
		for _, re := range res {
			if re.MatchString(l) {
				n++
				break
			}
		}
	}
	return n
}

// pytest: progress lines (every failure is in the FAILURES section and the
// short summary) and verbose PASSED / SKIPPED lines go; the FAILURES and
// ERRORS sections, the short summary, warnings and the totals stay.
var (
	pytestHeader   = regexp.MustCompile(`^=+ (test session starts|FAILURES|ERRORS|short test summary info) =+$`)
	pytestTotals   = regexp.MustCompile(`^=+ .*\b(passed|failed|error|errors)\b.* in [\d.]+s.* =+$`)
	pytestProgress = regexp.MustCompile(`^\S+\.py [.sxXFE]+\s*\[\s*\d+%\]$`)
	pytestVerbose  = regexp.MustCompile(`^\S+\.py::\S.* (PASSED|SKIPPED|XFAIL|XPASS)\b`)
	pytestFailure  = regexp.MustCompile(`^(FAILED|ERROR) \S+`)
)

var pytestReducer = reducer{
	name: "pytest", version: 1,
	detect: func(lines []string) bool {
		return countMatches(lines, pytestHeader, pytestTotals) > 0 && countMatches(lines, pytestProgress, pytestVerbose) > 0
	},
	drop: func(_ *reducerState, l string) bool {
		return pytestProgress.MatchString(l) || pytestVerbose.MatchString(l)
	},
	failure: pytestFailure.MatchString,
}

// jest: passing suites, ✓ lines and the console output of passing suites
// go; failing suites with every ● block, and the totals, stay.
var (
	jestSuite   = regexp.MustCompile(`^\s*(PASS|FAIL)\s+\S`)
	jestTotals  = regexp.MustCompile(`^(Test Suites|Tests):\s+.*\btotal\b`)
	jestPass    = regexp.MustCompile(`^\s*✓ `)
	jestConsole = regexp.MustCompile(`^\s*● Console\s*$`)
	jestBlock   = regexp.MustCompile(`^\s*● `)
	jestFailure = regexp.MustCompile(`^\s*● \S.*›`)
)

var jestReducer = reducer{
	name: "jest", version: 1,
	detect: func(lines []string) bool {
		return countMatches(lines, jestTotals) > 0 && countMatches(lines, jestSuite) > 0
	},
	drop: func(st *reducerState, l string) bool {
		switch {
		case jestSuite.MatchString(l):
			st.section = strings.Fields(l)[0]
			return st.section == "PASS"
		case jestTotals.MatchString(l), strings.HasPrefix(l, "Snapshots:"), strings.HasPrefix(l, "Time:"), strings.HasPrefix(l, "Ran all"):
			st.section = "summary"
			return false
		case jestConsole.MatchString(l):
			if st.section == "PASS" || st.section == "PASS-console" {
				st.section = "PASS-console"
				return true
			}
			return false
		case jestBlock.MatchString(l):
			if st.section == "PASS-console" {
				st.section = "PASS"
			}
			return false
		case jestPass.MatchString(l):
			return true
		}
		return st.section == "PASS-console"
	},
	failure: jestFailure.MatchString,
}

// go test: === RUN / PAUSE / CONT and --- PASS / SKIP lines go; everything
// else (failure messages, --- FAIL, panics, ok / FAIL per package) stays.
var (
	goRun     = regexp.MustCompile(`^=== (RUN|PAUSE|CONT|NAME)\s+\S`)
	goPass    = regexp.MustCompile(`^\s*--- (PASS|SKIP): \S+ \([\d.]+s\)$`)
	goPkg     = regexp.MustCompile(`^(ok|FAIL|---)\s+\S`)
	goFailure = regexp.MustCompile(`^\s*--- FAIL: `)
)

var goTestReducer = reducer{
	name: "go test", version: 1,
	detect: func(lines []string) bool {
		return countMatches(lines, goRun, goPass) > 0 && countMatches(lines, goPkg) > 0
	},
	drop: func(_ *reducerState, l string) bool {
		return goRun.MatchString(l) || goPass.MatchString(l)
	},
	failure: goFailure.MatchString,
}

// cargo test: passing and ignored tests and Compiling lines go; failures,
// warnings and the test result lines stay.
var (
	cargoResult    = regexp.MustCompile(`^test result: (ok|FAILED)\. \d+ passed`)
	cargoPass      = regexp.MustCompile(`^test \S+ \.\.\. (ok|ignored)$`)
	cargoCompiling = regexp.MustCompile(`^\s+(Compiling|Checking|Fresh|Downloaded|Downloading) \S+ v\S+`)
	cargoFailure   = regexp.MustCompile(`^test \S+ \.\.\. FAILED$`)
)

var cargoReducer = reducer{
	name: "cargo test", version: 1,
	detect: func(lines []string) bool {
		return countMatches(lines, cargoResult) > 0 && countMatches(lines, cargoPass) > 0
	},
	drop: func(_ *reducerState, l string) bool {
		return cargoPass.MatchString(l) || cargoCompiling.MatchString(l)
	},
	failure: cargoFailure.MatchString,
}

// Maven (surefire): downloads, Running lines and per-class results with no
// failures or errors go; failing classes and tests, the results section and
// BUILD lines stay.
var (
	mavenTotals   = regexp.MustCompile(`^\[(INFO|ERROR|WARNING)\] Tests run: \d+, Failures: \d+, Errors: \d+`)
	mavenClean    = regexp.MustCompile(`^\[INFO\] Tests run: \d+, Failures: 0, Errors: 0, Skipped: \d+, Time elapsed: .* -+ in \S+$`)
	mavenRunning  = regexp.MustCompile(`^\[INFO\] Running \S+$`)
	mavenDownload = regexp.MustCompile(`^\[INFO\] (Downloading|Downloaded) from \S+: \S+`)
	mavenFailure  = regexp.MustCompile(`<<< (FAILURE|ERROR)!\s*$`)
)

var mavenReducer = reducer{
	name: "maven", version: 1,
	detect: func(lines []string) bool {
		return countMatches(lines, mavenTotals) > 0 && countMatches(lines, mavenClean, mavenRunning, mavenDownload) > 0
	},
	drop: func(_ *reducerState, l string) bool {
		return mavenClean.MatchString(l) || mavenRunning.MatchString(l) || mavenDownload.MatchString(l)
	},
	failure: func(l string) bool { return mavenFailure.MatchString(l) && !strings.Contains(l, "Tests run:") },
}

// Gradle: passing and skipped tests and up-to-date tasks go; failing tests
// with their exception lines, failing tasks and the totals stay.
var (
	gradleTotals  = regexp.MustCompile(`^\d+ tests completed`)
	gradleBuild   = regexp.MustCompile(`^BUILD (SUCCESSFUL|FAILED)\b`)
	gradlePass    = regexp.MustCompile(`^\S.* > .*\(\) (PASSED|SKIPPED)$`)
	gradleTask    = regexp.MustCompile(`^> Task \S+ (UP-TO-DATE|FROM-CACHE|NO-SOURCE|SKIPPED)$`)
	gradleFailure = regexp.MustCompile(`^\S.* > .*\(\) FAILED$`)
)

var gradleReducer = reducer{
	name: "gradle", version: 1,
	detect: func(lines []string) bool {
		return countMatches(lines, gradleTotals, gradleBuild) > 0 && countMatches(lines, gradlePass, gradleTask, gradleFailure) > 0
	},
	drop: func(_ *reducerState, l string) bool {
		return gradlePass.MatchString(l) || gradleTask.MatchString(l)
	},
	failure: gradleFailure.MatchString,
}
