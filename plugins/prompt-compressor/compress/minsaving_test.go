package compress

import (
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
)

// A level-2 cut that removes less than minReduceSaving of a leaf is not
// used: the leaf keeps its level-1 form on every request.
func TestSmallReductionsAreNotUsed(t *testing.T) {
	text := caseOutput(corpus.TerraformPlan, "", 6000).Text
	lossless, _ := pass{level: 1}.run(text, 0)
	reduced, name := reduce(lossless, nil)
	if name == "" {
		t.Fatal("not reduced at all")
	}
	saving := float64(len(lossless)-len(reduced)) / float64(len(lossless))
	out, steps := pass{level: 2}.run(text, 0)
	used := hasStep(steps, reducerStrategy(name))
	if used != (saving >= minReduceSaving) {
		t.Errorf("saving %.2f, reducer used %v", saving, used)
	}
	if !used && out != lossless {
		t.Error("an unused reduction changed the text")
	}
	gradle := caseOutput(corpus.Gradle, "", 6000).Text
	if _, steps := (pass{level: 2}).run(gradle, 0); !hasStep(steps, reducerStrategy("gradle")) {
		t.Error("a large reduction was not used")
	}
}
