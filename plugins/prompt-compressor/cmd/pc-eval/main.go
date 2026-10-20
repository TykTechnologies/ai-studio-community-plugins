// Command pc-eval is the Prompt Compressor eval harness.
//
// Offline replay (no keys, no network; the CI smoke run):
//
//	pc-eval replay -levels 0,1,2 -out /tmp/pc-report        # the plugin's corpus
//	pc-eval replay -bodies ./captured -levels 0,1           # your own bodies: <format>/<name>.json
//
// It writes report.json and report.md: token reduction per tool kind and
// level, and the projected cost of the 30-turn sessions including prompt
// cache reads and writes, against compression off.
//
// Live suites (paid): plan and estimate first, then run with an approval
// that covers the estimate:
//
//	pc-eval plan -suite swebench -tasks verified.txt -n 200 -seed 7 -levels 1,2
//	pc-eval plan ... -run -approved-by lead -max-usd 400
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/eval"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "replay":
		err = replay(os.Args[2:])
	case "plan":
		err = plan(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pc-eval:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pc-eval replay|plan [flags]  (pc-eval <cmd> -h for flags)")
	os.Exit(2)
}

func levels(s string) ([]int, error) {
	var out []int
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > 3 {
			return nil, fmt.Errorf("bad level %q (0-3)", f)
		}
		out = append(out, n)
	}
	return out, nil
}

func replay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	dir := fs.String("corpus", "testdata/corpus", "corpus directory")
	bodies := fs.String("bodies", "", "replay captured bodies from <dir>/<format>/*.json instead of the corpus")
	lv := fs.String("levels", "0,1", "comma-separated levels")
	prices := fs.String("prices", "", "JSON price file overriding the defaults ({\"anthropic\": {\"cpit\": ...}})")
	outTok := fs.Int("output-tokens", 400, "response tokens billed per session turn")
	out := fs.String("out", "", "write report.json and report.md here (default: print Markdown)")
	fs.Parse(args)

	ls, err := levels(*lv)
	if err != nil {
		return err
	}
	var m corpus.Manifest
	if *bodies != "" {
		m, err = eval.LoadBodies(*bodies)
	} else {
		m, err = corpus.Load(*dir)
	}
	if err != nil {
		return err
	}
	p, err := eval.LoadPrices(*prices)
	if err != nil {
		return err
	}
	rep, err := eval.Replay(m, eval.Options{Levels: ls, Prices: p, OutputTokens: *outTok})
	if err != nil {
		return err
	}
	if *out == "" {
		fmt.Print(rep.Markdown())
		return nil
	}
	js, err := rep.JSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "report.json"), js, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "report.md"), []byte(rep.Markdown()), 0o644); err != nil {
		return err
	}
	fmt.Printf("pc-eval: wrote %s/report.json and report.md\n", *out)
	return nil
}

func plan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	suite := fs.String("suite", "bfcl", "bfcl | lcqa | swebench")
	tasks := fs.String("tasks", "", "file with one task id per line (default: synthetic ids task-00001..)")
	n := fs.Int("n", 0, "tasks to select (default: the gate minimum)")
	seed := fs.Uint64("seed", 1, "selection seed (same seed, same tasks)")
	lv := fs.String("levels", "1", "levels compared against off")
	format := fs.String("format", "anthropic", "request format, for pricing")
	prices := fs.String("prices", "", "JSON price file")
	conc := fs.Int("concurrency", 4, "parallel tasks")
	run := fs.Bool("run", false, "run the plan (paid): needs -approved-by and -max-usd covering the estimate")
	by := fs.String("approved-by", "", "who approved the spend")
	maxUSD := fs.Float64("max-usd", 0, "approved spending cap")
	note := fs.String("note", "", "approval note")
	fs.Parse(args)

	spec, ok := eval.Suites[*suite]
	if !ok {
		return fmt.Errorf("unknown suite %q", *suite)
	}
	ls, err := levels(*lv)
	if err != nil {
		return err
	}
	if *n == 0 {
		*n = spec.MinTasks
	}
	ids, err := taskIDs(*tasks, *n)
	if err != nil {
		return err
	}
	pl, err := eval.NewPlan(spec, ids, *n, *seed, ls, *format)
	if err != nil {
		return err
	}
	p, err := eval.LoadPrices(*prices)
	if err != nil {
		return err
	}
	est := pl.Estimate(p[*format], *conc)
	if *run {
		pl.Approve = &eval.Approval{By: *by, Note: *note, MaxUSD: *maxUSD}
	}
	out, _ := json.MarshalIndent(map[string]any{"plan": pl, "estimate": est, "estimate_human": map[string]string{
		"usd": fmt.Sprintf("$%.2f", est.USD), "duration": est.Duration.String(),
		"tokens": fmt.Sprintf("%.1fM in / %.1fM out", float64(est.InputTokens)/1e6, float64(est.OutputTokens)/1e6),
	}}, "", "  ")
	fmt.Println(string(out))
	if !*run {
		return nil
	}
	if err := pl.Authorize(est); err != nil {
		return err
	}
	return errors.Join(eval.ErrNoRunner, fmt.Errorf("suite %s", *suite))
}

func taskIDs(path string, n int) ([]string, error) {
	if path == "" {
		ids := make([]string, n)
		for i := range ids {
			ids[i] = fmt.Sprintf("task-%05d", i+1)
		}
		return ids, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ids []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if id := strings.TrimSpace(sc.Text()); id != "" && !strings.HasPrefix(id, "#") {
			ids = append(ids, id)
		}
	}
	return ids, sc.Err()
}
