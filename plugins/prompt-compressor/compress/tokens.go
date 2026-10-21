package compress

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

// Token counts are estimates: the vendors' own tokenizers are not public for
// every model. OpenAI-family models are counted with tiktoken (o200k_base for
// the gpt-4o, gpt-4.1, gpt-5 and o-series generation, cl100k_base for older
// ones); other vendors with a characters-per-token ratio.
//
// The BPE files are embedded (tiktoken-go-loader): tiktoken-go's default
// loader downloads them at runtime, which fails on an air-gapped edge.

func init() {
	tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}

// Estimator names, as reported.
const (
	EstimatorO200k  = "o200k_base"
	EstimatorCL100k = "cl100k_base"
	EstimatorChars  = "chars"
)

// charsPerToken is the ratio used for vendors without a public tokenizer.
var charsPerToken = map[string]float64{
	"anthropic": 3.5,
	"bedrock":   3.5,
	"google_ai": 4.0,
	"vertex":    4.0,
}

const defaultCharsPerToken = 4.0

// Estimator counts tokens for one model.
type Estimator struct {
	name  string
	enc   *tiktoken.Tiktoken
	ratio float64
	// perByte: ratio is bytes per token, calibrated on the request
	// (calibrated); otherwise it is runes per token.
	perByte bool
}

// Name is the tokenizer or ratio the counts come from.
func (e *Estimator) Name() string { return e.name }

// Count returns the estimated tokens of s.
func (e *Estimator) Count(s string) int {
	if s == "" {
		return 0
	}
	switch {
	case e.perByte:
		return max(int(float64(len(s))/e.ratio+0.5), 1)
	case e.enc != nil:
		return len(e.enc.EncodeOrdinary(s))
	}
	return max(int(float64(utf8.RuneCountInString(s))/e.ratio+0.5), 1)
}

// Calibration: a request's counts use bytes per token measured with the
// tokenizer on at most calibrationBytes of its own text, at most
// calibrationLeafBytes from each leaf. tiktoken allocates hundreds of times
// the text it encodes, so encoding every leaf made a multi-megabyte request
// allocate more than a gigabyte (eac.78). Counts are reported, never used
// to decide anything, so an estimate cannot change the output (R1).
const (
	calibrationBytes     = 8 << 10
	calibrationLeafBytes = 2 << 10
)

// calibrated returns an estimator that counts bytes at the ratio e's
// tokenizer gives on texts; e itself for a ratio estimator or no text.
func (e *Estimator) calibrated(count func(string) int, texts []string) *Estimator {
	if e.enc == nil {
		return e
	}
	var b strings.Builder
	for _, t := range texts {
		if b.Len() >= calibrationBytes {
			break
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(runePrefixString(t, min(calibrationLeafBytes, calibrationBytes-b.Len())))
	}
	sample := b.String()
	tokens := count(sample)
	if tokens <= 0 || len(sample) == 0 {
		return e
	}
	return &Estimator{name: e.name, ratio: float64(len(sample)) / float64(tokens), perByte: true}
}

// runePrefixString is at most n bytes of s, ending on a rune boundary.
func runePrefixString(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

var (
	encMu   sync.Mutex
	encMemo = map[string]*tiktoken.Tiktoken{}
)

func encoding(name string) *tiktoken.Tiktoken {
	encMu.Lock()
	defer encMu.Unlock()
	if enc, ok := encMemo[name]; ok {
		return enc
	}
	enc, err := tiktoken.GetEncoding(name)
	if err != nil {
		enc = nil
	}
	encMemo[name] = enc
	return enc
}

// EstimatorFor picks the estimator for a request in format to model.
func EstimatorFor(format, model string) *Estimator {
	m := strings.ToLower(model)
	if i := strings.LastIndexByte(m, '/'); i >= 0 {
		m = m[i+1:]
	}
	if openAIFamily(format, m) {
		name := EstimatorO200k
		if strings.HasPrefix(m, "gpt-3.5") || strings.HasPrefix(m, "gpt-4-") || m == "gpt-4" || strings.HasPrefix(m, "text-embedding") {
			name = EstimatorCL100k
		}
		if enc := encoding(name); enc != nil {
			return &Estimator{name: name, enc: enc}
		}
	}
	ratio, ok := charsPerToken[format]
	if !ok {
		ratio = defaultCharsPerToken
	}
	return &Estimator{name: EstimatorChars, ratio: ratio}
}

func openAIFamily(format, model string) bool {
	if strings.HasPrefix(format, "openai") {
		return true
	}
	for _, p := range []string{"gpt-", "o1", "o3", "o4", "codex", "chatgpt"} {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

// selectionTokens is the size min_unit_tokens is compared with: UTF-8 bytes
// over four, whatever the model. Selection must depend only on the unit text
// and the policy (R1), so it cannot use the model's tokenizer.
func selectionTokens(s string) int {
	return (len(s) + 3) / 4
}
