package compress

import (
	"reflect"
	"testing"
)

func TestCompactJSON(t *testing.T) {
	for in, want := range map[string]string{
		"{\n  \"b\": 1,\n  \"a\": [1.50, 2e3]\n}": `{"b":1,"a":[1.50,2e3]}`,    // key order and number spelling kept
		`{ "a": 1, "a": 2 }`:                      `{"a":1,"a":2}`,             // duplicate keys kept
		"[\n {\"s\": \"x  y\\n\\u001b[0m\"}\n]":   `[{"s":"x  y\n\u001b[0m"}]`, // strings and escapes untouched
		"  {\"a\" : true}  ":                      `{"a":true}`,
	} {
		got, ok := compactJSON(in)
		if !ok || got != want {
			t.Errorf("compactJSON(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{`"a string"`, `42`, `{"a":`, `not json`, ``} {
		if _, ok := compactJSON(in); ok {
			t.Errorf("compactJSON(%q) compacted a non-document", in)
		}
	}
}

func TestStripANSI(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[31mred\x1b[0m":                       "red",
		"\x1b[1;32m✓\x1b[39;49m ok":                "✓ ok",
		"\x1b]0;title\x07after":                    "after",
		"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\": "link",
		"a\x1bcb":       "ab", // ESC c (reset)
		"plain":         "plain",
		"dangling \x1b": "dangling \x1b",
		"\x1b[12;":      "\x1b[12;",
		"\x1b(Bascii":   "ascii",
	} {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollapseCRFrames(t *testing.T) {
	for in, want := range map[string]string{
		"10%\r50%\r100%\ndone":     "100%\ndone",
		"abcdef\rXY":               "XYcdef",
		"line one\r\nline two\r\n": "line one\nline two\n",
		"no frames":                "no frames",
		"ünï\rab":                  "abï",
	} {
		if got := collapseCRFrames(in); got != want {
			t.Errorf("collapseCRFrames(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollapseWhitespace(t *testing.T) {
	for in, want := range map[string]string{
		"a   \nb\t\n":             "a\nb\n",
		"a\n\n\n\nb":              "a\n\nb",
		"    indented\n\n\n  x  ": "    indented\n\n  x",
		"a\n \n\t\n\nb":           "a\n\nb",
	} {
		if got := collapseWhitespace(in); got != want {
			t.Errorf("collapseWhitespace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLosslessSteps(t *testing.T) {
	out, steps := lossless("\x1b[32mok\x1b[0m   \n\n\n\n10%\r100%")
	if out != "ok\n\n100%" {
		t.Errorf("out = %q", out)
	}
	want := []string{StrategyANSI, StrategyCRFrames, StrategyWhitespace}
	var got []string
	saved := 0
	for _, s := range steps {
		got = append(got, s.Strategy)
		saved += s.Saved
	}
	if !reflect.DeepEqual(got, want) || saved != len("\x1b[32mok\x1b[0m   \n\n\n\n10%\r100%")-len(out) {
		t.Errorf("steps %+v", steps)
	}
	if out, steps := lossless(`{"a": 1}`); out != `{"a":1}` || len(steps) != 1 || steps[0].Strategy != StrategyJSONCompact {
		t.Errorf("JSON: %q %+v", out, steps)
	}
}

// A JSON document's string values get the text strategies too, at any
// nesting of JSON-encoded strings; keys, numbers and other strings keep
// their bytes.
func TestLosslessJSONStringValues(t *testing.T) {
	in := `{"output": "\u001b[32mok\u001b[0m   \n\n\n\ndone", "metadata": {"exit_code": 0, "nameé": "café"}, "nested": "{\"log\": \"a  \\n\\n\\n\\nb\"}"}`
	want := `{"output":"ok\n\ndone","metadata":{"exit_code":0,"nameé":"café"},"nested":"{\"log\":\"a\\n\\nb\"}"}`
	out, steps := lossless(in)
	if out != want {
		t.Fatalf("out =\n%s\nwant\n%s", out, want)
	}
	names := map[string]bool{}
	for _, s := range steps {
		names[s.Strategy] = true
	}
	for _, n := range []string{StrategyJSONCompact, StrategyANSI, StrategyWhitespace} {
		if !names[n] {
			t.Errorf("steps %+v lack %s", steps, n)
		}
	}
}
