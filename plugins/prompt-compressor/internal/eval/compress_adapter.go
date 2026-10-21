package eval

import (
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

// Library runs the plugin's own compress package, the code the gateway runs,
// at each level with the default policy settings.
type Library struct {
	c *compress.Compressor
	// Policy returns the policy for a level; nil uses config.DefaultPolicy
	// with that level.
	Policy func(level int) *config.Policy
	// Label names Policy in reports.
	Label string
	// Outcomes counts the library's outcomes over the replay (the replay
	// calls it from one goroutine).
	Outcomes map[compress.Outcome]int
}

// NewLibrary returns the compress package wrapped for the replay.
func NewLibrary() *Library {
	return &Library{c: compress.New(compress.Options{}), Outcomes: map[compress.Outcome]int{}}
}

func (l *Library) Name() string {
	if l.Label != "" {
		return "compress (plugin library, " + l.Label + ")"
	}
	return "compress (plugin library, default policy)"
}

// Compress compresses body at level. A rewrite rejected by the library's
// post-check returns the original body and an error the report classifies.
func (l *Library) Compress(format, _ string, body []byte, level int) ([]byte, error) {
	if level == 0 {
		return body, nil
	}
	var p *config.Policy
	if l.Policy != nil {
		p = l.Policy(level)
	} else {
		p = config.DefaultPolicy()
		p.Level = level
	}
	out, rep := l.c.Compress(compress.Input{Format: format, Body: body, Policy: p})
	l.Outcomes[rep.Outcome]++
	if rep.Outcome == compress.OutcomeRejected {
		return body, classError(rep.Reason)
	}
	return out, nil
}

// classError is an error that is only a class: it carries no text from the
// body, so reports may print it.
type classError string

func (e classError) Error() string      { return string(e) }
func (e classError) ErrorClass() string { return string(e) }

// LibraryTokens counts tokens with the compress package's estimator for the
// format (tiktoken for OpenAI-family formats, a calibrated ratio otherwise).
func LibraryTokens(format string) Tokenizer { return libTokens{compress.EstimatorFor(format, "")} }

type libTokens struct{ e *compress.Estimator }

func (t libTokens) Name() string       { return t.e.Name() }
func (t libTokens) Count(b []byte) int { return t.e.Count(string(b)) }
