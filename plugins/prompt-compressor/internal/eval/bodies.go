package eval

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
)

// LoadBodies reads captured request bodies for a replay of your own traffic:
// dir/<format>/<name>.json, one request body per file, where <format> is
// the request filter format (openai, anthropic, bedrock, google_ai, vertex,
// ollama, openai_responses). Bodies are used as they are; nothing leaves
// the machine.
func LoadBodies(dir string) (corpus.Manifest, error) {
	m := corpus.Manifest{Version: corpus.ManifestVersion, Note: "captured bodies from " + dir}
	formats, err := os.ReadDir(dir)
	if err != nil {
		return m, err
	}
	for _, f := range formats {
		if !f.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(dir, f.Name(), "*.json"))
		if err != nil {
			return m, err
		}
		sort.Strings(files)
		for _, p := range files {
			body, err := os.ReadFile(p)
			if err != nil {
				return m, err
			}
			name := f.Name() + "/" + strings.TrimSuffix(filepath.Base(p), ".json")
			m.Entries = append(m.Entries, corpus.NewEntry(name, f.Name(), "", body))
		}
	}
	return m, nil
}
