package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// o4a.18: the pages ask in their own dialogs. The browser's confirm(),
// alert() and prompt() block the page, look unlike the rest of Studio and
// stop scripted browsers (Playwright, browser tools) until answered.
func TestUIUsesNoBrowserDialogs(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("ui", "webc", "*.js"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no ui/webc pages: %v", err)
	}
	// confirm( not preceded by a word character, a dot or $ (so a method
	// such as this._confirm is fine), optionally as window.confirm(.
	browserDialog := regexp.MustCompile(`(?:^|[^\w.$])((?:window\.)?(?:confirm|alert|prompt)\s*\()`)
	lineComment := regexp.MustCompile(`(?m)^\s*//.*$`)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range browserDialog.FindAllSubmatch(lineComment.ReplaceAll(b, nil), -1) {
			t.Errorf("%s: browser dialog %q", p, m[1])
		}
	}
}
