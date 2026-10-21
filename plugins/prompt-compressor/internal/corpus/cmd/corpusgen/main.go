// Command corpusgen writes the Prompt Compressor test corpus.
//
//	go run ./internal/corpus/cmd/corpusgen -out testdata/corpus
//	go run ./internal/corpus/cmd/corpusgen -explode /tmp/turns   # one file per session turn, to read
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
)

func main() {
	out := flag.String("out", "testdata/corpus", "corpus directory to write")
	explode := flag.String("explode", "", "instead, write every session turn of the corpus in -out as <dir>/<session>/turn-NN.json")
	flag.Parse()
	if err := run(*out, *explode); err != nil {
		fmt.Fprintln(os.Stderr, "corpusgen:", err)
		os.Exit(1)
	}
}

func run(out, explode string) error {
	if explode != "" {
		m, err := corpus.Load(out)
		if err != nil {
			return err
		}
		for _, s := range m.Sessions {
			for i, b := range s.Bodies() {
				path := filepath.Join(explode, s.Name, fmt.Sprintf("turn-%02d.json", i+1))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(path, b, 0o644); err != nil {
					return err
				}
			}
		}
		fmt.Printf("corpusgen: wrote %d sessions to %s\n", len(m.Sessions), explode)
		return nil
	}
	m := corpus.Build()
	if err := corpus.Write(out, m); err != nil {
		return err
	}
	fmt.Printf("corpusgen: wrote %d entries and %d sessions to %s\n", len(m.Entries), len(m.Sessions), out)
	return nil
}
