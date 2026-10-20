package corpus

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Kind is a type of tool output the compressor has strategies for.
type Kind string

// Tool output kinds. Every kind appears in at least two formats.
const (
	Pytest        Kind = "pytest"
	Jest          Kind = "jest"
	GoTest        Kind = "gotest"
	Cargo         Kind = "cargo"
	Maven         Kind = "maven"
	Gradle        Kind = "gradle"
	DockerBuild   Kind = "docker_build"
	Kubectl       Kind = "kubectl"
	TerraformPlan Kind = "terraform_plan"
	GitDiff       Kind = "git_diff"
	Ripgrep       Kind = "ripgrep"
	DirListing    Kind = "dir_listing"
	JSONAPI       Kind = "json_api"
	CSV           Kind = "csv"
	HTML          Kind = "html"
	MCP           Kind = "mcp"
)

// AllKinds lists every kind, in a fixed order.
var AllKinds = []Kind{Pytest, Jest, GoTest, Cargo, Maven, Gradle, DockerBuild, Kubectl,
	TerraformPlan, GitDiff, Ripgrep, DirListing, JSONAPI, CSV, HTML, MCP}

// Output is one generated tool output.
type Output struct {
	Kind    Kind
	Variant string
	Text    string
	// Marker is a substring of Text that identifies this output, for tests
	// that check the output reached a unit intact.
	Marker string
	// IsError marks an output a tool would report as failed.
	IsError bool
	// Command is the shell command (or tool call) that produced it.
	Command string
	// JSON is set for outputs that are a JSON document (Text holds it
	// serialised), so formats with structured tool results can carry it
	// as an object.
	JSON any
}

// Make generates an output of kind with roughly size bytes of text: each
// kind grows its repetitive part to about size and then ends naturally, so
// the result can be a little under or over.
func Make(g *R, kind Kind, variant string, size int) Output {
	var o Output
	switch kind {
	case Pytest:
		o = pytest(g, size)
	case Jest:
		o = jest(g, size, variant == "ansi")
	case GoTest:
		o = goTest(g, size, variant == "panic")
	case Cargo:
		o = cargo(g, size)
	case Maven:
		o = maven(g, size)
	case Gradle:
		o = gradle(g, size)
	case DockerBuild:
		o = dockerBuild(g, size)
	case Kubectl:
		o = kubectl(g, variant, size)
	case TerraformPlan:
		o = terraformPlan(g, size)
	case GitDiff:
		o = gitDiff(g, size)
	case Ripgrep:
		o = ripgrep(g, size, variant == "heading")
	case DirListing:
		o = dirListing(g, variant, size)
	case JSONAPI:
		o = jsonAPI(g, size, variant == "compact")
	case CSV:
		o = csvExport(g, size)
	case HTML:
		o = htmlPage(g, size)
	case MCP:
		o = mcpResult(g, size)
	default:
		panic("gen.Make: unknown kind " + string(kind))
	}
	o.Kind = kind
	if o.Variant == "" {
		o.Variant = variant
	}
	if !strings.Contains(o.Text, o.Marker) || o.Marker == "" {
		panic(fmt.Sprintf("gen.Make: %s marker %q not in text", kind, o.Marker))
	}
	return o
}

func pytest(g *R, size int) Output {
	var b strings.Builder
	files := append([]string(nil), pyModules...)
	Shuffle(g, files)
	b.WriteString("============================= test session starts ==============================\n")
	b.WriteString("platform linux -- Python 3.12.6, pytest-8.3.3, pluggy-1.5.0 -- /home/dev/src/ledger/worker/.venv/bin/python\n")
	b.WriteString("cachedir: .pytest_cache\n")
	b.WriteString("rootdir: /home/dev/src/ledger/worker\nconfigfile: pyproject.toml\n")
	b.WriteString("plugins: asyncio-0.24.0, cov-5.0.0, xdist-3.6.1, mock-3.14.0\n")
	b.WriteString("asyncio: mode=Mode.STRICT, default_loop_scope=function\n")
	type fail struct{ file, fn string }
	var fails []fail
	passed, skipped, total := 0, 0, 0
	var body strings.Builder
	for i := 0; body.Len() < size/2 || i < 3; i++ {
		f := files[i%len(files)]
		if i >= len(files) {
			f = strings.Replace(f, ".py", fmt.Sprintf("_%d.py", i/len(files)), 1)
		}
		n := g.N(8, 60)
		var marks strings.Builder
		for j := 0; j < n; j++ {
			switch {
			case g.P(0.006):
				marks.WriteByte('F')
				fails = append(fails, fail{f, Pick(g, pyFuncs)})
			case g.P(0.03):
				marks.WriteByte('s')
				skipped++
			default:
				marks.WriteByte('.')
				passed++
			}
			total++
		}
		fmt.Fprintf(&body, "%s %s", f, marks.String())
		pad := 79 - len(f) - 1 - marks.Len() - 7
		if pad < 1 {
			pad = 1
		}
		fmt.Fprintf(&body, "%s [%3d%%]\n", strings.Repeat(" ", pad), 0)
	}
	if len(fails) == 0 {
		fails = append(fails, fail{files[0], pyFuncs[0]})
	}
	// Fix the progress percentages now the total is known.
	lines := strings.Split(strings.TrimRight(body.String(), "\n"), "\n")
	seen := 0
	for i, l := range lines {
		k := strings.LastIndex(l, " [")
		marks := l[strings.Index(l, " ")+1 : k]
		seen += len(strings.TrimSpace(marks))
		lines[i] = fmt.Sprintf("%s [%3d%%]", l[:k], seen*100/total)
	}
	fmt.Fprintf(&b, "collected %d items\n\n", total)
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n\n=================================== FAILURES ===================================\n")
	for i, f := range fails {
		if i > 0 && b.Len() > size*9/10 {
			break // with -x pytest stops early; long runs truncate
		}
		fmt.Fprintf(&b, "%s %s %s\n\n", strings.Repeat("_", 20), f.fn, strings.Repeat("_", 20))
		amt := g.N(100, 99999)
		fmt.Fprintf(&b, "store = <ledger_worker.store.MemoryStore object at 0x7f%s>\n", g.Hex(10))
		fmt.Fprintf(&b, "batch = Batch(id='bat_%s', tenant='t_%s', entries=[Entry(amount=Decimal('%d.%02d'), currency='EUR', ref='INV-%d')])\n\n", g.Lower(12), g.Lower(8), amt/100, amt%100, g.N(10000, 99999))
		fmt.Fprintf(&b, "    def %s(store, batch):\n", f.fn)
		b.WriteString("        result = reconcile(store, batch, strategy=\"reference\")\n")
		b.WriteString(">       assert result.matched == len(batch.entries)\n")
		fmt.Fprintf(&b, "E       AssertionError: assert 0 == 1\n")
		b.WriteString("E        +  where 0 = ReconcileResult(matched=0, unmatched=1, skipped=0).matched\n")
		b.WriteString("E        +  and   1 = len([Entry(...)])\n\n")
		fmt.Fprintf(&b, "%s:%d: AssertionError\n", f.file, g.N(20, 400))
		b.WriteString("------------------------------ Captured log call -------------------------------\n")
		for k := 0; k < g.N(2, 6); k++ {
			fmt.Fprintf(&b, "WARNING  ledger_worker.reconcile:reconcile.py:%d no candidate for ref INV-%d in window 2026-09-0%d..2026-09-1%d\n", g.N(40, 220), g.N(10000, 99999), g.N(1, 9), g.N(0, 9))
		}
		b.WriteString("\n")
	}
	b.WriteString("=============================== warnings summary ===============================\n")
	for k := 0; k < g.N(2, 5); k++ {
		fmt.Fprintf(&b, "%s::%s\n  /home/dev/src/ledger/worker/.venv/lib/python3.12/site-packages/sqlalchemy/orm/session.py:%d: SAWarning: Object of type <Entry> not in session, add operation along 'Batch.entries' will not proceed\n    warnings.warn(\n\n", Pick(g, pyModules), Pick(g, pyFuncs), g.N(2000, 4000))
	}
	b.WriteString("-- Docs: https://docs.pytest.org/en/stable/how-to/capture-warnings.html\n")
	b.WriteString("=========================== short test summary info ============================\n")
	for _, f := range fails {
		fmt.Fprintf(&b, "FAILED %s::%s - AssertionError: assert 0 == 1\n", f.file, f.fn)
	}
	fmt.Fprintf(&b, "============ %d failed, %d passed, %d skipped, %d warnings in %.2fs ============\n", len(fails), passed, skipped, g.N(2, 5), g.F(3, 40))
	return Output{Text: b.String(), Marker: "test session starts", IsError: true, Command: "cd worker && python -m pytest -q -x --no-header -rA"}
}

func jest(g *R, size int, ansi bool) Output {
	red, green, dim, bold, reset := "", "", "", "", ""
	if ansi {
		red, green, dim, bold, reset = "\x1b[31m", "\x1b[32m", "\x1b[2m", "\x1b[1m", "\x1b[39m\x1b[22m"
	}
	var b strings.Builder
	files := append([]string(nil), tsFiles...)
	Shuffle(g, files)
	suites, failedSuites, tests, failedTests := 0, 0, 0, 0
	b.WriteString("\n> ledger-web@0.42.0 test\n> jest --ci --colors\n\n")
	for i := 0; b.Len() < size || i < 2; i++ {
		f := files[i%len(files)]
		if i >= len(files) {
			f = strings.Replace(f, ".test.", fmt.Sprintf(".%d.test.", i/len(files)), 1)
		}
		suites++
		n := g.N(3, 14)
		tests += n
		if i == 1 || g.P(0.12) {
			failedSuites++
			failedTests++
			fmt.Fprintf(&b, "%s FAIL %s %s (%.3f s)\n", red+bold, reset, f, g.F(0.4, 9))
			name := Pick(g, []string{"renders totals with currency", "keeps sort order after refetch", "disables submit while pending", "formats negative amounts", "shows empty state"})
			comp := strings.TrimSuffix(f[strings.LastIndex(f, "/")+1:], ".test.tsx")
			comp = strings.TrimSuffix(comp, ".test.ts")
			fmt.Fprintf(&b, "  %s● %s › %s%s\n\n", red+bold, comp, name, reset)
			fmt.Fprintf(&b, "    %sexpect(%sreceived%s).toEqual(%sexpected%s) // deep equality%s\n\n", dim, red, dim, green, dim, reset)
			fmt.Fprintf(&b, "    %s- Expected  - 1%s\n    %s+ Received  + 1%s\n\n", green, reset, red, reset)
			fmt.Fprintf(&b, "      Object {\n    %s-   \"total\": \"€1,234.50\",%s\n    %s+   \"total\": \"€1,234.5\",%s\n        \"currency\": \"EUR\",\n      }\n\n", green, reset, red, reset)
			ln := g.N(20, 180)
			fmt.Fprintf(&b, "    %s %d |%s   it(\"%s\", async () => {\n", dim, ln-1, reset, name)
			fmt.Fprintf(&b, "    %s %d |%s     const { result } = renderHook(() => use%s());\n", dim, ln, reset, comp)
			fmt.Fprintf(&b, "    %s>%s %d |%s     expect(result.current).toEqual(expected);\n", red+bold, dim, ln+1, reset)
			fmt.Fprintf(&b, "    %s     |%s                            ^\n\n", dim, reset)
			fmt.Fprintf(&b, "      at Object.<anonymous> (%s:%d:%d)\n", f, ln+1, g.N(10, 40))
			for k := 0; k < g.N(2, 5); k++ {
				fmt.Fprintf(&b, "      at %s (node_modules/%s:%d:%d)\n", Pick(g, []string{"Promise.then.completed", "new Promise (<anonymous>)", "callAsyncCircusFn", "_callCircusTest", "processTicksAndRejections"}), Pick(g, []string{"jest-circus/build/utils.js", "jest-circus/build/run.js", "node:internal/process/task_queues"}), g.N(100, 400), g.N(5, 40))
			}
			b.WriteString("\n")
		} else {
			fmt.Fprintf(&b, "%s PASS %s %s (%.3f s)\n", green+bold, reset, f, g.F(0.2, 6))
		}
		if g.P(0.3) {
			fmt.Fprintf(&b, "  %s●%s Console\n\n    console.warn\n      Warning: An update to %s inside a test was not wrapped in act(...).\n\n      When testing, code that causes React state updates should be wrapped into act(...):\n\n      act(() => {\n        /* fire events that update state */\n      });\n      /* assert on the output */\n\n", dim, reset, Pick(g, []string{"InvoiceTable", "AccountPicker", "RefundDialog"}))
		}
	}
	fmt.Fprintf(&b, "\n%sTest Suites: %s%s%d failed%s, %s%d passed%s, %d total\n", bold, reset, red+bold, failedSuites, reset, green+bold, suites-failedSuites, reset, suites)
	fmt.Fprintf(&b, "%sTests:       %s%s%d failed%s, %s%d passed%s, %d total\n", bold, reset, red+bold, failedTests, reset, green+bold, tests-failedTests, reset, tests)
	fmt.Fprintf(&b, "%sSnapshots:   %s%d passed, %d total\n%sTime:        %s%.3f s\nRan all test suites.\n", bold, reset, g.N(0, 12), g.N(12, 12), bold, reset, g.F(6, 60))
	o := Output{Text: b.String(), Marker: "Test Suites:", IsError: true, Command: "npm test -- --ci"}
	if ansi {
		o.Variant = "ansi"
	}
	return o
}

func goTest(g *R, size int, panicking bool) Output {
	var b strings.Builder
	pkgs := append([]string(nil), goPackages...)
	Shuffle(g, pkgs)
	failPkg := pkgs[0]
	for i := 0; b.Len() < size || i < 2; i++ {
		pkg := pkgs[i%len(pkgs)]
		typ := Pick(g, goTypes)
		n := g.N(3, 12)
		failed := false
		for j := 0; j < n; j++ {
			name := fmt.Sprintf("Test%s_%s", typ, Pick(g, goVerbs))
			fmt.Fprintf(&b, "=== RUN   %s\n", name)
			subs := 0
			if g.P(0.4) {
				subs = g.N(2, 6)
			}
			for s := 0; s < subs; s++ {
				fmt.Fprintf(&b, "=== RUN   %s/case_%d\n", name, s)
			}
			if g.P(0.3) {
				fmt.Fprintf(&b, "=== PAUSE %s\n=== CONT  %s\n", name, name)
			}
			if pkg == failPkg && i < len(pkgs) && j == n/2 && !panicking {
				failed = true
				file := strings.ToLower(typ) + "_test.go"
				fmt.Fprintf(&b, "    %s:%d: \n", file, g.N(30, 300))
				fmt.Fprintf(&b, "        \tError Trace:\t%s/%s/%s:%d\n", workDir, pkg, file, g.N(30, 300))
				b.WriteString("        \tError:      \tNot equal: \n")
				b.WriteString("        \t            \texpected: \"1234.50\"\n        \t            \tactual  : \"1234.5\"\n")
				b.WriteString("        \t            \t\n        \t            \tDiff:\n        \t            \t--- Expected\n        \t            \t+++ Actual\n        \t            \t@@ -1 +1 @@\n        \t            \t-1234.50\n        \t            \t+1234.5\n")
				fmt.Fprintf(&b, "        \tTest:       \t%s\n", name)
				fmt.Fprintf(&b, "--- FAIL: %s (%.2fs)\n", name, g.F(0, 0.5))
			} else {
				fmt.Fprintf(&b, "--- PASS: %s (%.2fs)\n", name, g.F(0, 0.3))
			}
			for s := 0; s < subs; s++ {
				fmt.Fprintf(&b, "    --- PASS: %s/case_%d (0.00s)\n", name, s)
			}
		}
		if panicking && pkg == failPkg && i < len(pkgs) {
			fn := Pick(g, goFuncs)
			fmt.Fprintf(&b, "=== RUN   Test%s_%s\n", typ, fn)
			fmt.Fprintf(&b, "--- FAIL: Test%s_%s (0.00s)\n", typ, fn)
			b.WriteString("panic: runtime error: invalid memory address or nil pointer dereference [recovered]\n")
			b.WriteString("\tpanic: runtime error: invalid memory address or nil pointer dereference\n")
			fmt.Fprintf(&b, "[signal SIGSEGV: segmentation violation code=0x1 addr=0x18 pc=0x%s]\n\n", g.Hex(6))
			fmt.Fprintf(&b, "goroutine %d [running]:\n", g.N(20, 90))
			b.WriteString("testing.tRunner.func1.2({0x104d1e0a0, 0x105331c50})\n\t/usr/local/go/src/testing/testing.go:1734 +0x1ac\n")
			b.WriteString("testing.tRunner.func1()\n\t/usr/local/go/src/testing/testing.go:1737 +0x334\n")
			b.WriteString("panic({0x104d1e0a0?, 0x105331c50?})\n\t/usr/local/go/src/runtime/panic.go:792 +0x124\n")
			fmt.Fprintf(&b, "%s/%s.(*%s).%s(0x0, {0x14000126f00, 0x3, 0x4})\n\t%s/%s/%s.go:%d +0x1c\n", goModule, pkg, typ, fn, workDir, pkg, strings.ToLower(typ), g.N(40, 400))
			fmt.Fprintf(&b, "%s/%s.Test%s_%s(0x14000103a00)\n\t%s/%s/%s_test.go:%d +0x5c\n", goModule, pkg, typ, fn, workDir, pkg, strings.ToLower(typ), g.N(20, 200))
			b.WriteString("testing.tRunner(0x14000103a00, 0x104e0f2a8)\n\t/usr/local/go/src/testing/testing.go:1792 +0xe4\n")
			b.WriteString("created by testing.(*T).Run in goroutine 1\n\t/usr/local/go/src/testing/testing.go:1851 +0x374\n")
			for k := 0; k < g.N(3, 8); k++ {
				fmt.Fprintf(&b, "\ngoroutine %d [select]:\n", g.N(2, 19))
				fmt.Fprintf(&b, "%s/internal/queue.(*Worker).loop(0x140001a2%s)\n\t%s/internal/queue/worker.go:%d +0x%s\n", goModule, g.Hex(3), workDir, g.N(50, 300), g.Hex(3))
				fmt.Fprintf(&b, "created by %s/internal/queue.Start in goroutine %d\n\t%s/internal/queue/worker.go:%d +0x%s\n", goModule, g.N(1, 20), workDir, g.N(20, 50), g.Hex(3))
			}
			fmt.Fprintf(&b, "FAIL\t%s/%s\t%.3fs\n", goModule, pkg, g.F(0.1, 2))
			continue
		}
		if failed {
			fmt.Fprintf(&b, "FAIL\n")
			fmt.Fprintf(&b, "FAIL\t%s/%s\t%.3fs\n", goModule, pkg, g.F(0.1, 4))
		} else {
			fmt.Fprintf(&b, "PASS\nok  \t%s/%s\t%.3fs\n", goModule, pkg, g.F(0.01, 4))
		}
	}
	b.WriteString("FAIL\n")
	o := Output{Text: b.String(), Marker: "=== RUN   Test", IsError: true, Command: "go test -v -count=1 ./..."}
	if panicking {
		o.Variant = "panic"
	}
	return o
}

func cargo(g *R, size int) Output {
	var b strings.Builder
	for _, c := range crates {
		fmt.Fprintf(&b, "   Compiling %s\n", c)
	}
	fmt.Fprintf(&b, "   Compiling %s v0.9.3 (/home/dev/src/ledger/parser)\n", rustCrate)
	for k := 0; k < g.N(2, 5); k++ {
		v := Pick(g, []string{"rec", "idx", "tmp", "scale", "raw"})
		mod := Pick(g, rustMods)
		ln := g.N(20, 400)
		fmt.Fprintf(&b, "warning: unused variable: `%s`\n  --> src/%s.rs:%d:13\n   |\n%d |         let %s = cursor.next_token()?;\n   |             ^^^ help: if this is intentional, prefix it with an underscore: `_%s`\n   |\n   = note: `#[warn(unused_variables)]` on by default\n\n", v, mod, ln, ln, v, v)
	}
	fmt.Fprintf(&b, "warning: `%s` (lib test) generated %d warnings (run `cargo fix --lib -p %s --tests` to apply 2 suggestions)\n", rustCrate, g.N(2, 5), rustCrate)
	fmt.Fprintf(&b, "    Finished `test` profile [unoptimized + debuginfo] target(s) in %.2fs\n", g.F(8, 90))
	fmt.Fprintf(&b, "     Running unittests src/lib.rs (target/debug/deps/ledger_parser-%s)\n\n", g.Hex(16))
	var names []string
	for b.Len()+len(names)*40 < size {
		names = append(names, fmt.Sprintf("%s::tests::%s", Pick(g, rustMods), Pick(g, []string{"parses_empty_input", "rejects_bad_checksum", "handles_crlf", "decimal_comma", "negative_amount", "multi_currency", "trailing_whitespace", "bom_prefix", "long_reference", "unicode_payee", "zero_amount", "date_rollover"})+fmt.Sprintf("_%d", len(names))))
	}
	if len(names) < 4 {
		names = append(names, "amount::tests::rounding_0", "ofx::tests::header_1", "lexer::tests::eof_2", "csv_reader::tests::quoted_3")
	}
	sort.Strings(names)
	fmt.Fprintf(&b, "running %d tests\n", len(names))
	var failed []string
	for i, n := range names {
		if i == len(names)/3 || (g.P(0.02) && len(failed) < 3) {
			failed = append(failed, n)
			fmt.Fprintf(&b, "test %s ... FAILED\n", n)
			continue
		}
		if g.P(0.03) {
			fmt.Fprintf(&b, "test %s ... ignored\n", n)
			continue
		}
		fmt.Fprintf(&b, "test %s ... ok\n", n)
	}
	b.WriteString("\nfailures:\n\n")
	for _, n := range failed {
		mod := strings.SplitN(n, "::", 2)[0]
		fmt.Fprintf(&b, "---- %s stdout ----\n\nthread '%s' panicked at src/%s.rs:%d:9:\nassertion `left == right` failed\n  left: Amount { minor: 123450, scale: 2, currency: \"EUR\" }\n right: Amount { minor: 12345, scale: 1, currency: \"EUR\" }\nnote: run with `RUST_BACKTRACE=1` environment variable to display a backtrace\n\n", n, n, mod, g.N(100, 600))
	}
	b.WriteString("\nfailures:\n")
	for _, n := range failed {
		fmt.Fprintf(&b, "    %s\n", n)
	}
	fmt.Fprintf(&b, "\ntest result: FAILED. %d passed; %d failed; 0 ignored; 0 measured; 0 filtered out; finished in %.2fs\n\nerror: test failed, to rerun pass `--lib`\n", len(names)-len(failed), len(failed), g.F(0.01, 3))
	return Output{Text: b.String(), Marker: "test result: FAILED.", IsError: true, Command: "cd parser && cargo test"}
}

func maven(g *R, size int) Output {
	var b strings.Builder
	b.WriteString("[INFO] Scanning for projects...\n[INFO] \n[INFO] ------------------< com.example.acme.ledger:ledger-reports >------------------\n[INFO] Building ledger-reports 2.14.0-SNAPSHOT\n[INFO]   from pom.xml\n[INFO] --------------------------------[ jar ]---------------------------------\n")
	for b.Len() < size*2/3 {
		a := Pick(g, mavenArtifacts)
		for _, ext := range []string{".pom", ".jar"} {
			url := "https://repo.maven.apache.org/maven2/" + a + ext
			fmt.Fprintf(&b, "[INFO] Downloading from central: %s\n", url)
			kb := g.F(1, 2400)
			fmt.Fprintf(&b, "[INFO] Downloaded from central: %s (%.1f kB at %.0f kB/s)\n", url, kb, g.F(200, 9000))
		}
	}
	b.WriteString("[INFO] \n[INFO] --- resources:3.3.1:resources (default-resources) @ ledger-reports ---\n[INFO] Copying 4 resources from src/main/resources to target/classes\n[INFO] \n[INFO] --- compiler:3.13.0:compile (default-compile) @ ledger-reports ---\n[INFO] Recompiling the module because of changed source code.\n")
	fmt.Fprintf(&b, "[INFO] Compiling %d source files with javac [debug release 21] to target/classes\n", g.N(40, 160))
	for k := 0; k < g.N(2, 6); k++ {
		fmt.Fprintf(&b, "[WARNING] /home/dev/src/ledger/reports/src/main/java/com/example/acme/ledger/%s.java:[%d,%d] %s\n", Pick(g, javaClasses), g.N(10, 300), g.N(5, 60), Pick(g, []string{"unchecked conversion", "[deprecation] getAmount() in Entry has been deprecated and marked for removal", "redundant cast to java.math.BigDecimal"}))
	}
	b.WriteString("[INFO] \n[INFO] --- surefire:3.2.5:test (default-test) @ ledger-reports ---\n[INFO] Using auto detected provider org.apache.maven.surefire.junitplatform.JUnitPlatformProvider\n[INFO] \n[INFO] -------------------------------------------------------\n[INFO]  T E S T S\n[INFO] -------------------------------------------------------\n")
	failCls := Pick(g, javaClasses)
	total, failures := 0, 0
	for _, c := range javaClasses {
		n := g.N(3, 25)
		total += n
		fmt.Fprintf(&b, "[INFO] Running %s.%sTest\n", javaGroup, c)
		if c == failCls {
			failures++
			fmt.Fprintf(&b, "[ERROR] Tests run: %d, Failures: 1, Errors: 0, Skipped: 0, Time elapsed: %.3f s <<< FAILURE! -- in %s.%sTest\n", n, g.F(0.1, 3), javaGroup, c)
			fmt.Fprintf(&b, "[ERROR] %s.%sTest.closesPeriodWithPendingEntries -- Time elapsed: 0.041 s <<< FAILURE!\norg.opentest4j.AssertionFailedError: expected: <CLOSED> but was: <PENDING>\n", javaGroup, c)
			for _, fr := range []string{"org.junit.jupiter.api.AssertionFailureBuilder.build(AssertionFailureBuilder.java:151)", "org.junit.jupiter.api.AssertionFailureBuilder.buildAndThrow(AssertionFailureBuilder.java:132)", "org.junit.jupiter.api.AssertEquals.failNotEqual(AssertEquals.java:197)", "org.junit.jupiter.api.AssertEquals.assertEquals(AssertEquals.java:182)", "org.junit.jupiter.api.Assertions.assertEquals(Assertions.java:1156)"} {
				fmt.Fprintf(&b, "\tat %s\n", fr)
			}
			fmt.Fprintf(&b, "\tat %s.%sTest.closesPeriodWithPendingEntries(%sTest.java:%d)\n\tat java.base/java.lang.reflect.Method.invoke(Method.java:580)\n\tat java.base/java.util.ArrayList.forEach(ArrayList.java:1596)\n\n", javaGroup, c, c, g.N(30, 200))
		} else {
			fmt.Fprintf(&b, "[INFO] Tests run: %d, Failures: 0, Errors: 0, Skipped: %d, Time elapsed: %.3f s -- in %s.%sTest\n", n, g.N(0, 1), g.F(0.05, 4), javaGroup, c)
		}
	}
	fmt.Fprintf(&b, "[INFO] \n[INFO] Results:\n[INFO] \n[ERROR] Failures: \n[ERROR]   %sTest.closesPeriodWithPendingEntries:%d expected: <CLOSED> but was: <PENDING>\n[INFO] \n[ERROR] Tests run: %d, Failures: %d, Errors: 0, Skipped: 0\n[INFO] \n", failCls, g.N(30, 200), total, failures)
	fmt.Fprintf(&b, "[INFO] ------------------------------------------------------------------------\n[INFO] BUILD FAILURE\n[INFO] ------------------------------------------------------------------------\n[INFO] Total time:  %02d:%02d min\n[INFO] Finished at: 2026-09-14T09:%02d:%02d+02:00\n[INFO] ------------------------------------------------------------------------\n", g.N(0, 3), g.N(0, 59), g.N(0, 59), g.N(0, 59))
	b.WriteString("[ERROR] Failed to execute goal org.apache.maven.plugins:maven-surefire-plugin:3.2.5:test (default-test) on project ledger-reports: There are test failures.\n[ERROR] \n[ERROR] Please refer to /home/dev/src/ledger/reports/target/surefire-reports for the individual test results.\n[ERROR] -> [Help 1]\n")
	return Output{Text: b.String(), Marker: "[INFO] BUILD FAILURE", IsError: true, Command: "cd reports && mvn -B test"}
}

func gradle(g *R, size int) Output {
	var b strings.Builder
	b.WriteString("Starting a Gradle Daemon (subsequent builds will be faster)\n")
	modules := []string{"core", "reports", "export", "api", "app"}
	tasks := []string{"compileJava", "processResources", "classes", "jar", "compileTestJava", "processTestResources", "testClasses"}
	for pass := 0; b.Len() < size/2; pass++ {
		for _, m := range modules {
			if pass > 0 {
				// A multi-project build: later passes are the plugin and
				// library subprojects.
				m = fmt.Sprintf("%s-%s", Pick(g, []string{"plugins", "libs", "integrations"}), m+fmt.Sprint(pass))
			}
			for _, t := range tasks {
				state := Pick(g, []string{"UP-TO-DATE", "UP-TO-DATE", "UP-TO-DATE", "NO-SOURCE", "FROM-CACHE", ""})
				if state == "" {
					fmt.Fprintf(&b, "> Task :%s:%s\n", m, t)
				} else {
					fmt.Fprintf(&b, "> Task :%s:%s %s\n", m, t, state)
				}
			}
			if b.Len() > size/2 {
				break
			}
		}
		if g.P(0.5) {
			fmt.Fprintf(&b, "Note: Some input files use or override a deprecated API.\nNote: Recompile with -Xlint:deprecation for details.\n")
		}
	}
	b.WriteString("\n> Task :app:test\n\n")
	cls := Pick(g, javaClasses)
	for k := 0; k < g.N(1, 3); k++ {
		m := Pick(g, []string{"rejectsNegativeAmount()", "closesPeriod()", "exportsUtf8Bom()", "convertsAtSpotRate()"})
		fmt.Fprintf(&b, "%sTest > %s FAILED\n    org.opentest4j.AssertionFailedError at %sTest.java:%d\n\n", cls, m, cls, g.N(20, 200))
	}
	for b.Len() < size {
		fmt.Fprintf(&b, "%sTest > %s PASSED\n", Pick(g, javaClasses), Pick(g, []string{"roundsHalfEven()", "skipsVoided()", "paginates()", "mergesDuplicates()", "parsesIso()", "respectsTenant()"}))
	}
	n := g.N(200, 900)
	f := g.N(1, 3)
	fmt.Fprintf(&b, "\n%d tests completed, %d failed, %d skipped\n\n> Task :app:test FAILED\n\n", n, f, g.N(0, 4))
	b.WriteString("FAILURE: Build failed with an exception.\n\n* What went wrong:\nExecution failed for task ':app:test'.\n> There were failing tests. See the report at: file:///home/dev/src/ledger/app/build/reports/tests/test/index.html\n\n* Try:\n> Run with --scan to get full insights.\n\nDeprecated Gradle features were used in this build, making it incompatible with Gradle 9.0.\n\nYou can use '--warning-mode all' to show the individual deprecation warnings and determine if they come from your own scripts or plugins.\n\nFor more on this, please refer to https://docs.gradle.org/8.10.1/userguide/command_line_interface.html#sec:command_line_warnings in the Gradle documentation.\n\n")
	fmt.Fprintf(&b, "BUILD FAILED in %ds\n%d actionable tasks: %d executed, %d up-to-date\n", g.N(20, 300), g.N(30, 60), g.N(5, 20), g.N(20, 40))
	return Output{Text: b.String(), Marker: "> Task :", IsError: true, Command: "./gradlew build"}
}

func dockerBuild(g *R, size int) Output {
	var b strings.Builder
	step := 0
	next := func() int { step++; return step }
	b.WriteString("#0 building with \"default\" instance using docker driver\n\n")
	s := next()
	fmt.Fprintf(&b, "#%d [internal] load build definition from Dockerfile\n#%d transferring dockerfile: %dB done\n#%d DONE 0.0s\n\n", s, s, g.N(900, 3000), s)
	for _, img := range []string{"docker.io/library/node:22-bookworm-slim", "docker.io/library/debian:bookworm-slim"} {
		s = next()
		fmt.Fprintf(&b, "#%d [internal] load metadata for %s\n#%d DONE %.1fs\n\n", s, img, s, g.F(0.4, 2.5))
	}
	s = next()
	fmt.Fprintf(&b, "#%d [internal] load .dockerignore\n#%d transferring context: 2B done\n#%d DONE 0.0s\n\n", s, s, s)
	s = next()
	digest := g.Hex(64)
	fmt.Fprintf(&b, "#%d [builder 1/7] FROM docker.io/library/node:22-bookworm-slim@sha256:%s\n", s, digest)
	for k := 0; k < g.N(4, 8); k++ {
		layer := g.Hex(64)
		total := g.F(1, 50)
		el := g.F(0.1, 1)
		for p := 0.0; p < total; p += total / float64(g.N(2, 5)) {
			fmt.Fprintf(&b, "#%d sha256:%s %.2fMB / %.2fMB %.1fs\n", s, layer, p, total, el)
			el += g.F(0.2, 1.5)
		}
		fmt.Fprintf(&b, "#%d sha256:%s %.2fMB / %.2fMB %.1fs done\n", s, layer, total, total, el)
	}
	fmt.Fprintf(&b, "#%d DONE %.1fs\n\n", s, g.F(5, 20))
	s = next()
	fmt.Fprintf(&b, "#%d [builder 2/7] RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git\n", s)
	t := 0.2
	for i, rel := range []string{"bookworm InRelease", "bookworm-updates InRelease", "bookworm-security InRelease", "bookworm/main amd64 Packages [8792 kB]", "bookworm-updates/main amd64 Packages [13.8 kB]", "bookworm-security/main amd64 Packages [218 kB]"} {
		t += g.F(0.1, 0.6)
		fmt.Fprintf(&b, "#%d %.3f Get:%d http://deb.debian.org/debian %s\n", s, t, i+1, rel)
	}
	fmt.Fprintf(&b, "#%d %.3f Fetched 9282 kB in 2s (4620 kB/s)\n#%d %.3f Reading package lists...\n", s, t+1, s, t+1.4)
	for b.Len() < size/2 {
		p := Pick(g, debPackages)
		t += g.F(0.01, 0.3)
		fmt.Fprintf(&b, "#%d %.3f Get:%d http://deb.debian.org/debian bookworm/main amd64 %s amd64 %d.%d.%d-%d [%d kB]\n", s, t, g.N(7, 60), p, g.N(1, 3), g.N(0, 20), g.N(0, 9), g.N(1, 9), g.N(20, 3000))
	}
	for _, p := range debPackages[:g.N(6, len(debPackages))] {
		t += g.F(0.01, 0.2)
		fmt.Fprintf(&b, "#%d %.3f Setting up %s (%d.%d.%d) ...\n", s, t, p, g.N(1, 3), g.N(0, 20), g.N(0, 9))
	}
	fmt.Fprintf(&b, "#%d DONE %.1fs\n\n", s, t+0.3)
	s = next()
	fmt.Fprintf(&b, "#%d [builder 3/7] COPY package.json package-lock.json ./\n#%d DONE 0.1s\n\n", s, s)
	s = next()
	fmt.Fprintf(&b, "#%d [builder 4/7] RUN npm ci\n", s)
	t = 1
	for b.Len() < size*5/6 {
		t += g.F(0.1, 2)
		fmt.Fprintf(&b, "#%d %.2f npm warn deprecated %s\n", s, t, Pick(g, npmDeprecated))
	}
	fmt.Fprintf(&b, "#%d %.2f \n#%d %.2f added %d packages, and audited %d packages in %ds\n#%d %.2f \n#%d %.2f %d packages are looking for funding\n#%d %.2f   run `npm fund` for details\n#%d %.2f \n#%d %.2f %d vulnerabilities (%d moderate, %d high)\n", s, t, s, t, g.N(900, 1600), g.N(900, 1600), int(t), s, t, s, t, g.N(100, 250), s, t, s, t, s, t, 5, 3, 2)
	fmt.Fprintf(&b, "#%d DONE %.1fs\n\n", s, t+0.5)
	s = next()
	fmt.Fprintf(&b, "#%d [builder 5/7] COPY . .\n#%d DONE 0.4s\n\n", s, s)
	s = next()
	fmt.Fprintf(&b, "#%d [builder 6/7] RUN npm run build\n#%d 0.412 \n#%d 0.412 > ledger-web@0.42.0 build\n#%d 0.412 > tsc -b && vite build\n#%d 0.412 \n", s, s, s, s, s)
	fmt.Fprintf(&b, "#%d 9.871 src/pages/Statements.tsx(%d,%d): error TS2345: Argument of type 'string | undefined' is not assignable to parameter of type 'string'.\n", s, g.N(10, 300), g.N(5, 40))
	fmt.Fprintf(&b, "#%d 9.871   Type 'undefined' is not assignable to type 'string'.\n#%d ERROR: process \"/bin/sh -c npm run build\" did not complete successfully: exit code: 2\n------\n > [builder 6/7] RUN npm run build:\n9.871 src/pages/Statements.tsx: error TS2345\n------\n", s, s)
	b.WriteString("Dockerfile:14\n--------------------\n  12 |     RUN npm ci\n  13 |     COPY . .\n  14 | >>> RUN npm run build\n  15 |     \n  16 |     FROM debian:bookworm-slim\n--------------------\nERROR: failed to solve: process \"/bin/sh -c npm run build\" did not complete successfully: exit code: 2\n")
	return Output{Text: b.String(), Marker: "load build definition from Dockerfile", IsError: true, Command: "docker build --progress=plain -t " + registry + "/ledger-web:dev ."}
}

func kubectl(g *R, variant string, size int) Output {
	switch variant {
	case "", "get_pods":
		var b strings.Builder
		b.WriteString("NAMESPACE        NAME                                        READY   STATUS             RESTARTS        AGE     IP             NODE                                          NOMINATED NODE   READINESS GATES\n")
		for b.Len() < size {
			ns := Pick(g, k8sNamespaces)
			app := Pick(g, k8sApps)
			status, ready, restarts := "Running", "1/1", "0"
			switch {
			case g.P(0.06):
				status, ready, restarts = "CrashLoopBackOff", "0/1", fmt.Sprintf("%d (%dm ago)", g.N(5, 80), g.N(1, 9))
			case g.P(0.04):
				status, ready = "Pending", "0/1"
			case g.P(0.1):
				restarts = fmt.Sprintf("%d (%dh ago)", g.N(1, 4), g.N(1, 30))
			}
			name := fmt.Sprintf("%s-%s-%s", app, g.Lower(10), g.Lower(5))
			fmt.Fprintf(&b, "%-16s %-43s %-7s %-18s %-15s %-7s %-14s %-45s <none>           <none>\n", ns, name, ready, status, restarts, fmt.Sprintf("%dd", g.N(1, 60)), fmt.Sprintf("10.42.%d.%d", g.N(0, 9), g.N(2, 250)), fmt.Sprintf("ip-10-0-%d-%d.eu-west-1.compute.internal", g.N(1, 3), g.N(2, 250)))
		}
		return Output{Variant: "get_pods", Text: b.String(), Marker: "NOMINATED NODE", Command: "kubectl get pods -A -o wide"}
	case "describe":
		var b strings.Builder
		app := Pick(g, k8sApps)
		name := fmt.Sprintf("%s-%s-%s", app, g.Lower(10), g.Lower(5))
		fmt.Fprintf(&b, "Name:             %s\nNamespace:        ledger-prod\nPriority:         0\nService Account:  %s\nNode:             ip-10-0-2-17.eu-west-1.compute.internal/10.0.2.17\nStart Time:       Sun, 13 Sep 2026 22:41:07 +0000\nLabels:           app.kubernetes.io/name=%s\n                  app.kubernetes.io/part-of=ledger\n                  pod-template-hash=%s\nAnnotations:      kubectl.kubernetes.io/restartedAt: 2026-09-13T22:40:51Z\nStatus:           Running\nIP:               10.42.3.118\nControlled By:  ReplicaSet/%s-%s\n", name, app, app, g.Lower(10), app, g.Lower(10))
		b.WriteString("Containers:\n")
		fmt.Fprintf(&b, "  %s:\n    Container ID:   containerd://%s\n    Image:          %s/%s:2.14.%d\n    Image ID:       %s/%s@sha256:%s\n    Port:           8080/TCP\n    Host Port:      0/TCP\n    State:          Waiting\n      Reason:       CrashLoopBackOff\n    Last State:     Terminated\n      Reason:       Error\n      Exit Code:    1\n      Started:      Mon, 14 Sep 2026 09:12:44 +0000\n      Finished:     Mon, 14 Sep 2026 09:12:49 +0000\n    Ready:          False\n    Restart Count:  %d\n    Limits:\n      cpu:     1\n      memory:  512Mi\n    Requests:\n      cpu:      250m\n      memory:   256Mi\n    Liveness:   http-get http://:8080/healthz delay=10s timeout=1s period=10s #success=1 #failure=3\n    Readiness:  http-get http://:8080/readyz delay=5s timeout=1s period=5s #success=1 #failure=3\n    Environment:\n      DATABASE_URL:   <set to the key 'url' in secret 'ledger-db'>  Optional: false\n      LOG_LEVEL:      info\n      OTEL_EXPORTER_OTLP_ENDPOINT:  http://otel-collector.monitoring:4317\n    Mounts:\n      /var/run/secrets/kubernetes.io/serviceaccount from kube-api-access-%s (ro)\n", app, g.Hex(64), registry, app, g.N(0, 9), registry, app, g.Hex(64), g.N(10, 90), g.Lower(5))
		b.WriteString("Conditions:\n  Type                        Status\n  PodReadyToStartContainers   True \n  Initialized                 True \n  Ready                       False \n  ContainersReady             False \n  PodScheduled                True \nVolumes:\n  kube-api-access-x2k9p:\n    Type:                    Projected (a volume that contains injected data from multiple sources)\n    TokenExpirationSeconds:  3607\n    ConfigMapName:           kube-root-ca.crt\n    DownwardAPI:             true\nQoS Class:                   Burstable\nNode-Selectors:              <none>\nTolerations:                 node.kubernetes.io/not-ready:NoExecute op=Exists for 300s\n                             node.kubernetes.io/unreachable:NoExecute op=Exists for 300s\nEvents:\n  Type     Reason     Age                    From     Message\n  ----     ------     ----                   ----     -------\n")
		for b.Len() < size {
			r := Pick(g, []struct{ typ, reason, msg string }{
				{"Normal", "Pulled", "Container image \"" + registry + "/" + app + ":2.14.3\" already present on machine"},
				{"Normal", "Created", "Created container " + app},
				{"Normal", "Started", "Started container " + app},
				{"Warning", "BackOff", "Back-off restarting failed container " + app + " in pod " + name + "_ledger-prod(" + g.UUID() + ")"},
				{"Warning", "Unhealthy", fmt.Sprintf("Readiness probe failed: Get \"http://10.42.3.118:8080/readyz\": dial tcp 10.42.3.118:8080: connect: connection refused")},
			})
			fmt.Fprintf(&b, "  %-8s %-10s %-22s %-8s %s\n", r.typ, r.reason, fmt.Sprintf("%dm (x%d over %dh)", g.N(1, 59), g.N(2, 300), g.N(1, 12)), "kubelet", r.msg)
		}
		return Output{Variant: "describe", Text: b.String(), Marker: "Events:\n", IsError: false, Command: "kubectl describe pod -n ledger-prod " + name}
	case "json":
		var items []any
		text := ""
		for len(text) < size {
			app := Pick(g, k8sApps)
			ns := Pick(g, k8sNamespaces)
			rs := g.N(1, 6)
			items = append(items, O(
				"apiVersion", "apps/v1", "kind", "Deployment",
				"metadata", O("annotations", O("deployment.kubernetes.io/revision", fmt.Sprint(g.N(1, 90)), "meta.helm.sh/release-name", "ledger", "meta.helm.sh/release-namespace", ns),
					"creationTimestamp", g.Time(2000*time.Hour).Format(time.RFC3339), "generation", g.N(1, 90),
					"labels", O("app.kubernetes.io/instance", "ledger", "app.kubernetes.io/managed-by", "Helm", "app.kubernetes.io/name", app, "app.kubernetes.io/version", "2.14.3", "helm.sh/chart", "ledger-2.14.3"),
					"name", app, "namespace", ns, "resourceVersion", fmt.Sprint(g.N(1000000, 99999999)), "uid", g.UUID()),
				"spec", O("progressDeadlineSeconds", 600, "replicas", rs, "revisionHistoryLimit", 10,
					"selector", O("matchLabels", O("app.kubernetes.io/instance", "ledger", "app.kubernetes.io/name", app)),
					"strategy", O("rollingUpdate", O("maxSurge", "25%", "maxUnavailable", "25%"), "type", "RollingUpdate"),
					"template", O("metadata", O("creationTimestamp", nil, "labels", O("app.kubernetes.io/instance", "ledger", "app.kubernetes.io/name", app)),
						"spec", O("containers", []any{O("image", registry+"/"+app+":2.14.3", "imagePullPolicy", "IfNotPresent", "name", app,
							"ports", []any{O("containerPort", 8080, "name", "http", "protocol", "TCP")},
							"resources", O("limits", O("cpu", "1", "memory", "512Mi"), "requests", O("cpu", "250m", "memory", "256Mi")),
							"terminationMessagePath", "/dev/termination-log", "terminationMessagePolicy", "File")},
							"dnsPolicy", "ClusterFirst", "restartPolicy", "Always", "schedulerName", "default-scheduler", "securityContext", O(), "serviceAccountName", app, "terminationGracePeriodSeconds", 30))),
				"status", O("availableReplicas", rs, "conditions", []any{
					O("lastTransitionTime", g.Time(500*time.Hour).Format(time.RFC3339), "lastUpdateTime", g.Time(500*time.Hour).Format(time.RFC3339), "message", "Deployment has minimum availability.", "reason", "MinimumReplicasAvailable", "status", "True", "type", "Available"),
					O("lastTransitionTime", g.Time(500*time.Hour).Format(time.RFC3339), "lastUpdateTime", g.Time(500*time.Hour).Format(time.RFC3339), "message", fmt.Sprintf("ReplicaSet \"%s-%s\" has successfully progressed.", app, g.Lower(10)), "reason", "NewReplicaSetAvailable", "status", "True", "type", "Progressing")},
					"observedGeneration", g.N(1, 90), "readyReplicas", rs, "replicas", rs, "updatedReplicas", rs),
			))
			doc := O("apiVersion", "v1", "items", items, "kind", "List", "metadata", O("resourceVersion", ""))
			text = indentJSON(doc, 4)
		}
		doc := O("apiVersion", "v1", "items", items, "kind", "List", "metadata", O("resourceVersion", ""))
		return Output{Variant: "json", Text: text, Marker: "resourceVersion", Command: "kubectl get deployments -A -o json", JSON: doc}
	case "logs":
		var b strings.Builder
		for b.Len() < size {
			lvl := Pick(g, []string{"info", "info", "info", "debug", "warn", "error"})
			msg := Pick(g, []string{"request completed", "request completed", "db pool stats", "retrying webhook delivery", "slow query", "context deadline exceeded"})
			fmt.Fprintf(&b, "{\"time\":\"%s\",\"level\":\"%s\",\"msg\":\"%s\",\"service\":\"ledger-api\",\"trace_id\":\"%s\",\"route\":\"%s\",\"status\":%d,\"duration_ms\":%.1f}\n", g.Time(2*time.Hour).Format(time.RFC3339Nano), lvl, msg, g.Hex(32), Pick(g, []string{"/v1/invoices", "/v1/accounts/{id}", "/v1/exports", "/v1/batches"}), Pick(g, []int{200, 200, 200, 201, 404, 409, 500, 503}), g.F(1, 2500))
		}
		return Output{Variant: "logs", Text: b.String(), Marker: "\"service\":\"ledger-api\"", Command: "kubectl logs -n ledger-prod deploy/ledger-api --since=1h"}
	}
	panic("gen.kubectl: unknown variant " + variant)
}

func terraformPlan(g *R, size int) Output {
	var b strings.Builder
	for _, r := range tfResources {
		for k := 0; k < g.N(1, 4); k++ {
			kind := strings.Split(r.typ, "_")[1]
			id := fmt.Sprintf("%s-%s", kind[:min(3, len(kind))], g.Hex(17))
			if k > 0 {
				fmt.Fprintf(&b, "module.%s.%s.%s[%d]: Refreshing state... [id=%s]\n", Pick(g, []string{"network", "compute", "data"}), r.typ, r.name, k, id)
			} else {
				fmt.Fprintf(&b, "%s.%s: Refreshing state... [id=%s]\n", r.typ, r.name, id)
			}
		}
	}
	b.WriteString("\nTerraform used the selected providers to generate the following execution\nplan. Resource actions are indicated with the following symbols:\n  + create\n  ~ update in-place\n-/+ destroy and then create replacement\n\nTerraform will perform the following actions:\n\n")
	add, change, destroy := 0, 0, 0
	for i := 0; b.Len() < size || i < 3; i++ {
		r := tfResources[i%len(tfResources)]
		addr := fmt.Sprintf("%s.%s", r.typ, r.name)
		if i >= len(tfResources) {
			addr = fmt.Sprintf("module.%s.%s.%s[%d]", Pick(g, []string{"network", "compute", "data"}), r.typ, r.name, i/len(tfResources))
		}
		switch i % 3 {
		case 0:
			change++
			fmt.Fprintf(&b, "  # %s will be updated in-place\n  ~ resource \"%s\" \"%s\" {\n        id                     = \"%s\"\n", addr, r.typ, r.name, g.Hex(17))
			fmt.Fprintf(&b, "      ~ tags                   = {\n          ~ \"Version\"     = \"2.13.%d\" -> \"2.14.%d\"\n            # (4 unchanged elements hidden)\n        }\n", g.N(0, 9), g.N(0, 9))
			fmt.Fprintf(&b, "      ~ tags_all               = {\n          ~ \"Version\"     = \"2.13.%d\" -> \"2.14.%d\"\n            # (6 unchanged elements hidden)\n        }\n        # (%d unchanged attributes hidden)\n\n        # (%d unchanged blocks hidden)\n    }\n\n", g.N(0, 9), g.N(0, 9), g.N(8, 40), g.N(1, 6))
		case 1:
			add++
			fmt.Fprintf(&b, "  # %s will be created\n  + resource \"%s\" \"%s\" {\n", addr, r.typ, r.name)
			for _, a := range []string{"arn", "id", "name_prefix", "owner_id", "tags_all", "create_date", "unique_id"} {
				fmt.Fprintf(&b, "      + %-22s = (known after apply)\n", a)
			}
			fmt.Fprintf(&b, "      + name                   = \"%s-%s\"\n      + tags                   = {\n          + \"Environment\" = \"prod\"\n          + \"Project\"     = \"%s\"\n          + \"Owner\"       = \"platform\"\n        }\n    }\n\n", tfProject, r.name, tfProject)
		default:
			destroy++
			add++
			fmt.Fprintf(&b, "  # %s must be replaced\n-/+ resource \"%s\" \"%s\" {\n      ~ arn                    = \"arn:aws:%s:eu-west-1:000000000000:%s/%s\" -> (known after apply)\n      ~ id                     = \"%s\" -> (known after apply)\n      ~ engine_version         = \"15.7\" -> \"16.4\" # forces replacement\n        # (%d unchanged attributes hidden)\n    }\n\n", addr, r.typ, r.name, strings.Split(r.typ, "_")[1], r.name, g.Hex(12), g.Hex(17), g.N(10, 30))
		}
	}
	fmt.Fprintf(&b, "Plan: %d to add, %d to change, %d to destroy.\n\nChanges to Outputs:\n  ~ api_version = \"2.13.4\" -> \"2.14.3\"\n\n─────────────────────────────────────────────────────────────────────────────\n\nNote: You didn't use the -out option to save this plan, so Terraform can't\nguarantee to take exactly these actions if you run \"terraform apply\" now.\n", add, change, destroy)
	return Output{Text: b.String(), Marker: "Terraform will perform the following actions:", Command: "cd deploy/terraform/prod && terraform plan -no-color"}
}

func gitDiff(g *R, size int) Output {
	var b strings.Builder
	files := []string{}
	for i := 0; i < 12; i++ {
		files = append(files, Pick(g, goPackages)+"/"+strings.ToLower(Pick(g, goTypes))+Pick(g, []string{".go", "_test.go"}))
	}
	files = append(files, "web/package-lock.json")
	for i := 0; b.Len() < size || i < 2; i++ {
		f := files[i%len(files)]
		fmt.Fprintf(&b, "diff --git a/%s b/%s\nindex %s..%s 100644\n--- a/%s\n+++ b/%s\n", f, f, g.Hex(7), g.Hex(7), f, f)
		if strings.HasSuffix(f, ".json") {
			start := g.N(1000, 9000)
			fmt.Fprintf(&b, "@@ -%d,40 +%d,40 @@\n", start, start)
			for k := 0; k < 10; k++ {
				pkg := strings.Fields(Pick(g, crates))[0]
				fmt.Fprintf(&b, "     \"node_modules/%s\": {\n-      \"version\": \"1.%d.%d\",\n-      \"resolved\": \"https://registry.npmjs.org/%s/-/%s-1.%d.%d.tgz\",\n-      \"integrity\": \"sha512-%s\",\n+      \"version\": \"1.%d.%d\",\n+      \"resolved\": \"https://registry.npmjs.org/%s/-/%s-1.%d.%d.tgz\",\n+      \"integrity\": \"sha512-%s\",\n       \"dev\": true,\n", pkg, k, 1, pkg, pkg, k, 1, g.B64(48), k, 2, pkg, pkg, k, 2, g.B64(48))
			}
			continue
		}
		pkg := strings.TrimSuffix(f[:strings.LastIndex(f, "/")], "/")
		pkg = pkg[strings.LastIndex(pkg, "/")+1:]
		for h := 0; h < g.N(1, 4); h++ {
			start := g.N(10, 400)
			typ := Pick(g, goTypes)
			fn := Pick(g, goFuncs)
			fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@ func (x *%s) %s(ctx context.Context) error {\n", start, 9, start, 12, typ, fn)
			fmt.Fprintf(&b, " \tif x == nil {\n \t\treturn ErrNil%s\n \t}\n", typ)
			fmt.Fprintf(&b, "-\ttotal := x.Subtotal()\n+\ttotal, err := x.Subtotal(ctx)\n+\tif err != nil {\n+\t\treturn fmt.Errorf(\"%s: %s %%s: %%w\", x.ID, err)\n+\t}\n", pkg, strings.ToLower(fn))
			fmt.Fprintf(&b, " \tfor _, line := range x.Lines {\n-\t\ttotal = total.Add(line.Tax())\n+\t\ttotal = total.Add(line.Tax().Round(%d))\n \t}\n", g.N(2, 4))
		}
	}
	return Output{Text: b.String(), Marker: "diff --git a/", Command: "git diff HEAD~3"}
}

func ripgrep(g *R, size int, heading bool) Output {
	var b strings.Builder
	sym := Pick(g, []string{"Subtotal(", "RoundHalfEven", "ErrNilInvoice", "tenantID", "ctx.Done()"})
	last := ""
	for b.Len() < size {
		f := Pick(g, goPackages) + "/" + strings.ToLower(Pick(g, goTypes)) + Pick(g, []string{".go", "_test.go"})
		if g.P(0.15) {
			f = "vendor/git.example.com/acme/money/" + strings.ToLower(Pick(g, goTypes)) + ".go"
		}
		ln := g.N(5, 900)
		line := Pick(g, []string{
			"\ttotal := x." + sym + "ctx)",
			"\t// " + sym + " must be called with the tenant scope set",
			"\tif err := " + strings.ToLower(Pick(g, goTypes)) + "." + sym + "); err != nil {",
			"func (s *Service) " + strings.Trim(sym, "(") + "Total(ctx context.Context, id string) (Money, error) {",
			"\t\tassert.Equal(t, want, got." + sym + "ctx))",
		})
		if heading {
			if f != last {
				if last != "" {
					b.WriteString("\n")
				}
				b.WriteString(f + "\n")
				last = f
			}
			fmt.Fprintf(&b, "%d:%s\n", ln, line)
		} else {
			fmt.Fprintf(&b, "%s:%d:%s\n", f, ln, line)
		}
	}
	o := Output{Text: b.String(), Command: "rg -n '" + sym + "'"}
	if heading {
		o.Variant = "heading"
		o.Command = "rg -n --heading '" + sym + "'"
	}
	o.Marker = sym
	return o
}

func dirListing(g *R, variant string, size int) Output {
	switch variant {
	case "", "ls":
		var b strings.Builder
		fmt.Fprintf(&b, "total %d\n", g.N(100, 900))
		b.WriteString("drwxr-xr-x  14 dev  staff    448 Sep 14 09:12 .\ndrwxr-xr-x   9 dev  staff    288 Sep  2 17:40 ..\n")
		for b.Len() < size {
			name := Pick(g, dirNames)
			isDir := g.P(0.3)
			perm, sz := "-rw-r--r--", g.N(120, 90000)
			if isDir {
				perm, sz = "drwxr-xr-x", g.N(2, 40)*32
			} else {
				name = strings.ToLower(Pick(g, goTypes)) + "_" + g.Lower(4) + Pick(g, fileExts)
			}
			t := g.Time(4000 * time.Hour)
			fmt.Fprintf(&b, "%s  %2d dev  staff  %6d %s %s\n", perm, g.N(1, 20), sz, t.Format("Jan _2 15:04"), name)
		}
		return Output{Variant: "ls", Text: b.String(), Marker: "drwxr-xr-x", Command: "ls -la"}
	case "find":
		var b strings.Builder
		for b.Len() < size {
			depth := g.N(1, 5)
			parts := []string{"."}
			if g.P(0.25) {
				parts = append(parts, "web", "node_modules", Pick(g, []string{"@babel", "react-dom", "lodash", "date-fns", "@testing-library"}))
			}
			for d := 0; d < depth; d++ {
				parts = append(parts, Pick(g, dirNames))
			}
			parts = append(parts, strings.ToLower(Pick(g, goTypes))+Pick(g, fileExts))
			b.WriteString(strings.Join(parts, "/") + "\n")
		}
		return Output{Variant: "find", Text: b.String(), Marker: "./", Command: "find . -type f"}
	case "tree":
		var b strings.Builder
		b.WriteString(".\n")
		dirs, files := 0, 0
		var walk func(prefix string, depth int)
		walk = func(prefix string, depth int) {
			n := g.N(2, 7)
			for i := 0; i < n && b.Len() < size; i++ {
				lastEntry := i == n-1
				branch, nextPrefix := "├── ", prefix+"│   "
				if lastEntry {
					branch, nextPrefix = "└── ", prefix+"    "
				}
				if depth < 3 && g.P(0.4) {
					dirs++
					b.WriteString(prefix + branch + Pick(g, dirNames) + "\n")
					walk(nextPrefix, depth+1)
				} else {
					files++
					b.WriteString(prefix + branch + strings.ToLower(Pick(g, goTypes)) + Pick(g, fileExts) + "\n")
				}
			}
		}
		for b.Len() < size {
			walk("", 0)
		}
		fmt.Fprintf(&b, "\n%d directories, %d files\n", dirs, files)
		return Output{Variant: "tree", Text: b.String(), Marker: " directories, ", Command: "tree -L 4"}
	}
	panic("gen.dirListing: unknown variant " + variant)
}

func jsonAPI(g *R, size int, compact bool) Output {
	var items []any
	text := ""
	api := "https://" + repoHost + "/api/v3/repos/" + repoPath
	for len(text) < size {
		n := g.N(100, 2999)
		login := Pick(g, firstNames) + "-" + g.Lower(3)
		var ls []any
		for k := 0; k < g.N(0, 3); k++ {
			l := Pick(g, labels)
			ls = append(ls, O("id", g.N(100000, 999999), "node_id", "LA_"+g.Alnum(16), "url", api+"/labels/"+l, "name", l, "color", g.Hex(6), "default", false, "description", nil))
		}
		created := g.Time(3000 * time.Hour)
		items = append(items, O(
			"url", fmt.Sprintf("%s/issues/%d", api, n),
			"repository_url", api,
			"labels_url", fmt.Sprintf("%s/issues/%d/labels{/name}", api, n),
			"comments_url", fmt.Sprintf("%s/issues/%d/comments", api, n),
			"events_url", fmt.Sprintf("%s/issues/%d/events", api, n),
			"html_url", fmt.Sprintf("https://%s/%s/issues/%d", repoHost, repoPath, n),
			"id", g.N(1000000000, 2000000000),
			"node_id", "I_kwDO"+g.Alnum(14),
			"number", n,
			"title", title(g.Words(g.N(4, 10))),
			"user", O("login", login, "id", g.N(100000, 9999999), "node_id", "U_"+g.Alnum(14),
				"avatar_url", fmt.Sprintf("https://avatars.example.com/u/%d?v=4", g.N(100000, 9999999)),
				"gravatar_id", "", "url", "https://"+repoHost+"/api/v3/users/"+login,
				"html_url", "https://"+repoHost+"/"+login, "type", "User", "site_admin", false),
			"labels", nilIfEmpty(ls),
			"state", Pick(g, []string{"open", "open", "closed"}),
			"locked", false,
			"assignee", nil,
			"assignees", []any{},
			"milestone", nil,
			"comments", g.N(0, 40),
			"created_at", created.Format(time.RFC3339),
			"updated_at", created.Add(time.Duration(g.N(1, 500))*time.Hour).Format(time.RFC3339),
			"closed_at", nil,
			"author_association", Pick(g, []string{"MEMBER", "CONTRIBUTOR", "NONE"}),
			"active_lock_reason", nil,
			"body", g.Sentence(g.N(10, 60))+"\n\n### Steps to reproduce\n\n1. "+g.Sentence(g.N(5, 12))+"\n2. "+g.Sentence(g.N(5, 12))+"\n\n### Expected\n\n"+g.Sentence(g.N(6, 20)),
			"reactions", O("url", fmt.Sprintf("%s/issues/%d/reactions", api, n), "total_count", 0, "+1", 0, "-1", 0, "laugh", 0, "hooray", 0, "confused", 0, "heart", 0, "rocket", 0, "eyes", 0),
			"timeline_url", fmt.Sprintf("%s/issues/%d/timeline", api, n),
			"performed_via_github_app", nil,
			"state_reason", nil,
		))
		if compact {
			text = string(Encode(items, Compact))
		} else {
			text = indentJSON(items, 2)
		}
	}
	o := Output{Text: text, Marker: "repository_url", Command: "curl -s " + api + "/issues?state=all&per_page=100", JSON: items}
	if compact {
		o.Variant = "compact"
	}
	return o
}

func nilIfEmpty(xs []any) []any {
	if xs == nil {
		return []any{}
	}
	return xs
}

func csvExport(g *R, size int) Output {
	var b strings.Builder
	b.WriteString("order_id,tenant_id,region,sku,quantity,unit_price,currency,discount_pct,created_at,status,notes\n")
	for b.Len() < size {
		note := ""
		if g.P(0.1) {
			note = "\"" + g.Sentence(g.N(3, 9)) + "\""
		}
		fmt.Fprintf(&b, "ord_%s,t_%s,%s,%s,%d,%.2f,%s,%d,%s,%s,%s\n", g.Lower(12), g.Lower(8), Pick(g, regions), Pick(g, skus), g.N(1, 250), g.F(4, 2000), Pick(g, currencies), Pick(g, []int{0, 0, 0, 5, 10, 15, 20}), g.Time(2000*time.Hour).Format("2006-01-02T15:04:05Z"), Pick(g, []string{"paid", "paid", "paid", "refunded", "pending", "void"}), note)
	}
	return Output{Text: b.String(), Marker: "order_id,tenant_id,region", Command: "ledgerctl export orders --since 2026-06-01 --format csv"}
}

func htmlPage(g *R, size int) Output {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\" data-theme=\"light\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>Rounding rules | Ledger Developer Docs</title>\n<meta name=\"description\" content=\"How Ledger rounds monetary amounts across currencies.\">\n<link rel=\"canonical\" href=\"https://docs.example.com/ledger/rounding\">\n<link rel=\"preload\" href=\"/assets/inter-var.woff2\" as=\"font\" type=\"font/woff2\" crossorigin>\n")
	fmt.Fprintf(&b, "<link rel=\"stylesheet\" href=\"/assets/app.%s.css\">\n<script>window.__CONFIG__={\"env\":\"prod\",\"release\":\"%s\",\"analytics\":false};</script>\n", g.Hex(8), g.Hex(12))
	b.WriteString("<style>\n:root{--fg:#1d1d1f;--bg:#fff;--accent:#3355ff}\n.sidebar{position:sticky;top:0;max-height:100vh;overflow:auto}\n.toc a{color:var(--fg);text-decoration:none}\n.callout{border-left:4px solid var(--accent);padding:.5rem 1rem}\n</style>\n</head>\n<body>\n<header class=\"site-header\"><a class=\"logo\" href=\"/\">Ledger Docs</a>\n<nav aria-label=\"Main\"><ul>")
	for _, s := range []string{"Guides", "API reference", "SDKs", "Changelog", "Status", "Support"} {
		fmt.Fprintf(&b, "<li><a href=\"/ledger/%s\">%s</a></li>", strings.ToLower(strings.ReplaceAll(s, " ", "-")), s)
	}
	b.WriteString("</ul></nav></header>\n<div class=\"layout\"><aside class=\"sidebar\"><ul class=\"toc\">\n")
	for k := 0; k < 30; k++ {
		t := title(g.Words(g.N(2, 4)))
		fmt.Fprintf(&b, "<li><a href=\"/ledger/%s\">%s</a></li>\n", strings.ToLower(strings.ReplaceAll(t, " ", "-")), t)
	}
	b.WriteString("</ul></aside>\n<main id=\"content\"><article>\n<h1>Rounding rules</h1>\n<p class=\"lead\">Ledger stores every amount in minor units and rounds half to even at the boundaries listed below.</p>\n")
	for b.Len() < size*3/4 {
		fmt.Fprintf(&b, "<h2 id=\"%s\">%s</h2>\n<p>%s %s</p>\n", g.Lower(6), title(g.Words(g.N(2, 5))), g.Sentence(g.N(12, 30)), g.Sentence(g.N(8, 25)))
		if g.P(0.4) {
			fmt.Fprintf(&b, "<div class=\"callout\"><strong>Note</strong><p>%s</p></div>\n", g.Sentence(g.N(8, 20)))
		}
		if g.P(0.4) {
			fmt.Fprintf(&b, "<pre><code class=\"language-go\">amount := money.New(%d, \"EUR\")\nrounded := amount.Round(money.HalfEven)\n</code></pre>\n", g.N(1, 99999))
		}
		if g.P(0.3) {
			b.WriteString("<table><thead><tr><th>Currency</th><th>Minor units</th><th>Rounding</th></tr></thead><tbody>\n")
			for _, c := range currencies {
				fmt.Fprintf(&b, "<tr><td>%s</td><td>%d</td><td>half even</td></tr>\n", c, 2)
			}
			b.WriteString("</tbody></table>\n")
		}
	}
	b.WriteString("</article></main></div>\n<footer class=\"site-footer\"><p>&copy; 2026 Acme Example Ltd.</p><ul>")
	for _, s := range []string{"Privacy", "Terms", "Security", "Contact"} {
		fmt.Fprintf(&b, "<li><a href=\"/%s\">%s</a></li>", strings.ToLower(s), s)
	}
	b.WriteString("</ul></footer>\n")
	for b.Len() < size {
		fmt.Fprintf(&b, "<script type=\"module\" src=\"/assets/chunk-%s.js\" integrity=\"sha384-%s\" crossorigin=\"anonymous\"></script>\n", g.Hex(8), g.B64(48))
	}
	b.WriteString("<script>document.querySelectorAll('pre code').forEach(function(el){el.classList.add('hl')});</script>\n</body>\n</html>\n")
	return Output{Text: b.String(), Marker: "<!DOCTYPE html>", Command: "web_fetch https://docs.example.com/ledger/rounding"}
}

// mcpResult is an MCP tools/call result (the "result" member of the
// JSON-RPC response): a text content block holding the rows as JSON, plus
// the same data as structuredContent, as MCP 2025-06-18 servers return it.
func mcpResult(g *R, size int) Output {
	var rows []any
	text := ""
	for len(text)*2 < size { // the rows go out twice: as text and structured
		rows = append(rows, O(
			"id", fmt.Sprintf("LED-%d", g.N(100, 4999)),
			"title", title(g.Words(g.N(4, 9))),
			"state", Pick(g, []string{"Backlog", "Todo", "In Progress", "In Review", "Done"}),
			"priority", g.N(0, 4),
			"assignee", Pick(g, firstNames)+"@example.com",
			"team", "Ledger",
			"labels", []any{Pick(g, labels)},
			"createdAt", g.Time(3000*time.Hour).Format(time.RFC3339Nano),
			"updatedAt", g.Time(300*time.Hour).Format(time.RFC3339Nano),
			"url", fmt.Sprintf("https://tracker.example.com/acme/issue/LED-%d", g.N(100, 4999)),
			"description", g.Sentence(g.N(10, 40)),
		))
		text = string(Encode(O("issues", rows, "pageInfo", O("hasNextPage", true, "endCursor", g.UUID())), Compact))
	}
	structured := O("issues", rows, "pageInfo", O("hasNextPage", true, "endCursor", g.UUID()))
	inner := string(Encode(structured, Compact))
	result := O("content", []any{O("type", "text", "text", inner)}, "structuredContent", structured, "isError", false)
	return Output{Text: string(Encode(result, Compact)), Marker: "structuredContent", Command: "mcp__tracker__search_issues", JSON: result}
}

// indentJSON serialises v the way json.MarshalIndent with this indent does,
// keeping key order.
func indentJSON(v any, indent int) string {
	var b strings.Builder
	writeIndented(&b, v, 0, strings.Repeat(" ", indent))
	return b.String()
}

func writeIndented(b *strings.Builder, v any, depth int, ind string) {
	pad := strings.Repeat(ind, depth)
	switch t := v.(type) {
	case *Obj:
		if len(t.kv) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, e := range t.kv {
			b.WriteString(pad + ind)
			encodeString(b, e.k, false)
			b.WriteString(": ")
			writeIndented(b, e.v, depth+1, ind)
			if i < len(t.kv)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(pad + "}")
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range t {
			b.WriteString(pad + ind)
			writeIndented(b, e, depth+1, ind)
			if i < len(t)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(pad + "]")
	default:
		encode(b, v, Compact)
	}
}
