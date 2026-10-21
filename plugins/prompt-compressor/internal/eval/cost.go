package eval

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/TykTechnologies/midsommar/v2/models"
)

// Price is one model's list price per token, the fields of a Studio model
// price row (models.ModelPrice): CPIT input, CPT output, cache write and
// cache read.
type Price struct {
	Vendor       string  `json:"vendor"`
	Model        string  `json:"model"`
	CPIT         float64 `json:"cpit"`
	CPT          float64 `json:"cpt"`
	CacheWritePT float64 `json:"cache_write_pt"`
	CacheReadPT  float64 `json:"cache_read_pt"`
}

// Prices maps a request format to the price the replay bills it at.
type Prices map[string]Price

// DefaultPrices are illustrative public list prices (USD per token, October
// 2026) for one model per format. They exist so a replay prints a number;
// pass your own with -prices for decisions.
func DefaultPrices() Prices {
	m := func(v float64) float64 { return v / 1e6 }
	claude := Price{Vendor: "anthropic", Model: "claude-sonnet-4-5", CPIT: m(3), CPT: m(15), CacheWritePT: m(3.75), CacheReadPT: m(0.30)}
	bedrock := claude
	bedrock.Vendor = "bedrock"
	return Prices{
		"anthropic":        claude,
		"bedrock":          bedrock,
		"openai":           {Vendor: "openai", Model: "gpt-4.1", CPIT: m(2), CPT: m(8), CacheReadPT: m(0.5)},
		"openai_responses": {Vendor: "openai", Model: "gpt-5-codex", CPIT: m(1.25), CPT: m(10), CacheReadPT: m(0.125)},
		"google_ai":        {Vendor: "google_ai", Model: "gemini-2.5-pro", CPIT: m(1.25), CPT: m(10), CacheReadPT: m(0.31)},
		"vertex":           {Vendor: "vertex", Model: "gemini-2.5-pro", CPIT: m(1.25), CPT: m(10), CacheReadPT: m(0.31)},
		"ollama":           {Vendor: "ollama", Model: "local"},
	}
}

// LoadPrices reads a Prices JSON file ({"anthropic": {...}, ...}) over the
// defaults.
func LoadPrices(path string) (Prices, error) {
	p := DefaultPrices()
	if path == "" {
		return p, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var over Prices
	if err := json.Unmarshal(raw, &over); err != nil {
		return nil, fmt.Errorf("prices %s: %w", path, err)
	}
	for k, v := range over {
		p[k] = v
	}
	return p, nil
}

// Usage is one request's billed token split, as the vendor reports it with
// cache tokens apart from the uncached input.
type Usage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheWrite int `json:"cache_write"`
	CacheRead  int `json:"cache_read"`
}

// Add returns u + o.
func (u Usage) Add(o Usage) Usage {
	return Usage{u.Input + o.Input, u.Output + o.Output, u.CacheWrite + o.CacheWrite, u.CacheRead + o.CacheRead}
}

// PromptTotal is every input-side token: uncached, written and read.
func (u Usage) PromptTotal() int { return u.Input + u.CacheWrite + u.CacheRead }

// CacheReadShare is the share of input-side tokens served from cache.
func (u Usage) CacheReadShare() float64 {
	if u.PromptTotal() == 0 {
		return 0
	}
	return float64(u.CacheRead) / float64(u.PromptTotal())
}

// Cost bills u at p with the gateway's own formula (models.ChatRecordCost,
// which also applies the rule that an unset cache price on OpenAI and Google
// bills at the input price), converted back to currency units.
func (p Price) Cost(u Usage) float64 {
	return models.ChatRecordCost(p.Vendor, p.CPT, p.CPIT, p.CacheWritePT, p.CacheReadPT,
		u.Input, u.Output, u.CacheWrite, u.CacheRead) / 10000
}

// CacheRule is how a vendor's prompt cache treats a request, for the
// offline model of which input tokens are cache reads and writes.
type CacheRule struct {
	// MinTokens is the shortest prefix the vendor caches.
	MinTokens int
	// Step rounds a cached prefix down (OpenAI caches in 128-token steps
	// after the first 1024).
	Step int
	// Explicit vendors cache only up to client breakpoints (cache_control,
	// cachePoint) and bill the newly written part as cache writes; implicit
	// ones cache every long prefix automatically and charge no write.
	Explicit bool
}

// CacheRules by request format.
var CacheRules = map[string]CacheRule{
	"anthropic":        {MinTokens: 1024, Explicit: true},
	"bedrock":          {MinTokens: 1024, Explicit: true},
	"openai":           {MinTokens: 1024, Step: 128},
	"openai_responses": {MinTokens: 1024, Step: 128},
	"google_ai":        {MinTokens: 1024},
	"vertex":           {MinTokens: 1024},
}

// TurnUsage models one turn of a session offline from the prefix-stability
// property: the vendor serves from cache the longest prefix this turn shares
// byte for byte with the previous turn's request (so a compressor that is
// a pure function of unit text keeps it, and one that is not loses it).
//
// tokens is the request's input token count; shared the token count of the
// prefix it shares with the previous turn (0 on the first turn). explicit
// says the body carries cache breakpoints (an explicit vendor caches nothing
// without them). For an explicit vendor the rest of the request is written to
// cache at the breakpoint; this slightly overstates writes when the last
// breakpoint sits before the end of the body.
func TurnUsage(rule CacheRule, tokens, shared int, explicit bool, output int) Usage {
	u := Usage{Output: output}
	if rule.MinTokens == 0 || (rule.Explicit && !explicit) {
		u.Input = tokens
		return u
	}
	read := 0
	if shared >= rule.MinTokens {
		read = shared
		if rule.Step > 0 {
			read = rule.MinTokens + (shared-rule.MinTokens)/rule.Step*rule.Step
		}
	}
	read = min(read, tokens)
	u.CacheRead = read
	if rule.Explicit && tokens-read >= 1 && tokens >= rule.MinTokens {
		u.CacheWrite = tokens - read
	} else {
		u.Input = tokens - read
	}
	return u
}
