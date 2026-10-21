package corpus

import (
	"fmt"
	"sort"
)

// SessionTurns is the number of requests in each agent session.
const SessionTurns = 30

// sessionKinds is the tool mix of a coding-agent session, weighted towards
// what agents run most (tests, searches, diffs).
var sessionKinds = []struct {
	kind    Kind
	variant string
	weight  int
}{
	{Ripgrep, "", 5}, {GoTest, "", 4}, {Pytest, "", 3}, {Jest, "", 2}, {Cargo, "", 2},
	{GitDiff, "", 4}, {DirListing, "tree", 1}, {DirListing, "ls", 2}, {Kubectl, "get_pods", 1},
	{Kubectl, "logs", 1}, {Kubectl, "describe", 1}, {Maven, "", 1}, {Gradle, "", 1}, {DockerBuild, "", 1},
	{TerraformPlan, "", 1}, {JSONAPI, "", 1}, {CSV, "", 1}, {HTML, "", 1}, {MCP, "", 1}, {GoTest, "panic", 1},
}

// sessionSize draws a tool output size: mostly small, some medium, a few
// large, like real agent traffic.
func sessionSize(g *R) int {
	switch p := g.F(0, 1); {
	case p < 0.55:
		return g.N(400, 2000)
	case p < 0.88:
		return g.N(2000, 8000)
	default:
		return g.N(10000, 24000)
	}
}

func pickKind(g *R) (Kind, string) {
	total := 0
	for _, k := range sessionKinds {
		total += k.weight
	}
	n := g.N(0, total-1)
	for _, k := range sessionKinds {
		if n < k.weight {
			return k.kind, k.variant
		}
		n -= k.weight
	}
	panic("unreachable")
}

var followUps = map[int]string{
	7:  "Also make sure the CSV export keeps the UTF-8 BOM, finance opens it in Excel.",
	15: "Don't touch the migrations directory, that's owned by another team.",
	22: "Can you run the full suite before you finish?",
}

// sessionConvo builds the 29 steps behind a 30-turn session.
func sessionConvo(name, style, model string, tools []ToolDef) (*Convo, []Kind, []ToolRef) {
	b := newBuilder(name, style)
	seen := map[Kind]bool{}
	var steps []Step
	for i := 0; i < SessionTurns-1; i++ {
		var outs []Output
		calls := 1
		if b.g.P(0.2) {
			calls = 2
		}
		for k := 0; k < calls; k++ {
			kind, variant := pickKind(b.g)
			outs = append(outs, b.out(kind, variant, sessionSize(b.g)))
			seen[kind] = true
		}
		text := ""
		if b.g.P(0.6) {
			text = b.g.Sentence(b.g.N(6, 18))
		}
		s := b.step(text, outs...)
		s.After = followUps[i]
		steps = append(steps, s)
	}
	var kinds []Kind
	for k := range seen {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return &Convo{Model: model, System: codingSystem, Tools: tools, Task: "The nightly reconciliation job reports mismatches for EUR invoices since the money package refactor. Find the cause across the Go API, the Python worker and the web app, fix it, and add regression tests.", Steps: steps}, kinds, b.refs
}

// Sessions builds the multi-turn agent sessions.
func Sessions() []Session {
	var ss []Session
	{
		c, kinds, refs := sessionConvo("session_anthropic", "anthropic", modelClaude, codingTools())
		g := NewR("session_anthropic/opaque")
		for i := range c.Steps {
			if i%3 == 0 {
				c.Steps[i].Thinking = fmtThinking(g, "latest")
				c.Steps[i].Signature = g.B64(g.N(180, 420))
			}
		}
		c.Steps[4].Calls[0].Blocks, c.Steps[4].Calls[0].Image, c.Steps[4].Calls[0].ImageData = true, true, pngImage(g)
		s := Session{Name: "anthropic_agent_30", Format: "anthropic", Path: pathAnthropic, History: "full", MovingCacheControl: true, Kinds: kinds, Tools: refs,
			Description: "A Claude Code style session: system and tools cached, cache_control moved to the newest user message every turn, thinking with signatures on every third step, one image result.",
			Shapes:      []string{ShapeCacheControlSystem, ShapeCacheControlTools, ShapeCacheControlMoving, ShapeThinking, ShapeImage, ShapeParallelToolCalls, ShapeUserFollowUp}}
		for n := 0; n < SessionTurns; n++ {
			s.bodies = append(s.bodies, Encode(RenderAnthropic(c, n, AnthropicOpts{SystemCache: true, ToolsCache: true, MovingCache: true, Thinking: true, Stream: true}), Compact))
		}
		ss = append(ss, s)
	}
	{
		c, kinds, refs := sessionConvo("session_openai", "openai", modelGPT, codingTools())
		s := Session{Name: "openai_agent_30", Format: "openai", Path: pathOpenAIChat, History: "full", Kinds: kinds, Tools: refs,
			Description: "An OpenAI Chat Completions agent session with the developer role and occasional parallel tool calls.",
			Shapes:      []string{ShapeDeveloperRole, ShapeParallelToolCalls, ShapeUserFollowUp}}
		for n := 0; n < SessionTurns; n++ {
			s.bodies = append(s.bodies, Encode(RenderOpenAI(c, n, OpenAIOpts{Developer: true, Stream: true}), Compact))
		}
		ss = append(ss, s)
	}
	{
		c, kinds, refs := sessionConvo("session_bedrock", "bedrock", modelClaude, codingTools())
		g := NewR("session_bedrock/opaque")
		for i := range c.Steps {
			if i%4 == 0 {
				c.Steps[i].Thinking = fmtThinking(g, "latest")
				c.Steps[i].Signature = g.B64(g.N(180, 420))
			}
			for k := range c.Steps[i].Calls {
				if c.Steps[i].Calls[k].Out.JSON != nil && i%2 == 0 {
					c.Steps[i].Calls[k].Structured = true
				}
			}
		}
		s := Session{Name: "bedrock_converse_agent_30", Format: "bedrock", Path: pathBedrockConverse, History: "full", Kinds: kinds, Tools: refs,
			Description: "A Bedrock Converse agent session with cachePoint blocks, reasoningContent on every fourth step and some structured json tool results.",
			Shapes:      []string{ShapeBedrockCachePoint, ShapeReasoningContent, ShapeParallelToolCalls, ShapeUserFollowUp}}
		for n := 0; n < SessionTurns; n++ {
			s.bodies = append(s.bodies, Encode(RenderBedrock(c, n, BedrockOpts{CachePoints: true, Reasoning: true}), Compact))
		}
		ss = append(ss, s)
	}
	{
		c, kinds, refs := sessionConvo("session_codex", "openai", modelCodex, codexTools())
		g := NewR("session_codex/opaque")
		for i := range c.Steps {
			st := &c.Steps[i]
			st.Encrypted = "gAAAAAB" + g.B64(g.N(400, 1600))
			if g.P(0.5) {
				st.Summary = "**" + title(g.Words(g.N(3, 6))) + "**"
			}
			for k := range st.Calls {
				codexShell(&st.Calls[k])
			}
			if i%5 == 2 {
				st.Calls = append(st.Calls, Call{ID: CallID(g, "openai"), Name: "apply_patch",
					Args: O("input", fmt.Sprintf("*** Begin Patch\n*** Update File: %s/%s.go\n@@\n-\treturn x.Subtotal()\n+\treturn x.Subtotal(ctx)\n*** End Patch\n", Pick(g, goPackages), Pick(g, []string{"invoice", "ledger", "money"}))),
					Out:  Output{Kind: "patch_result", Text: "Success. Updated the following files:\nM internal/billing/invoice.go\n", Marker: "Success."}})
			}
		}
		s := Session{Name: "responses_codex_30", Format: "openai_responses", Path: pathResponses, History: "full", Kinds: kinds, Tools: refs,
			Description: "A Codex CLI style Responses session: full history with store false, encrypted reasoning items, wrapped shell outputs and apply_patch calls.",
			Shapes:      []string{ShapeEncryptedReasoning, ShapeWrappedOutput, ShapeCustomToolOutput, ShapeUserFollowUp}}
		key := g.UUID()
		for n := 0; n < SessionTurns; n++ {
			s.bodies = append(s.bodies, Encode(RenderResponses(c, n, ResponsesOpts{Codex: true, CacheKey: key}), Compact))
		}
		ss = append(ss, s)
	}
	{
		c, kinds, refs := sessionConvo("session_chained", "openai", modelGPT, codingTools())
		g := NewR("session_chained/opaque")
		s := Session{Name: "responses_chained_30", Format: "openai_responses", Path: pathResponses, History: "previous_response_id", Kinds: kinds, Tools: refs,
			Description: "A Responses session chained with previous_response_id: each turn sends only the new tool outputs.",
			Shapes:      []string{ShapePreviousResponseID, ShapeParallelToolCalls, ShapeUserFollowUp}}
		prev := ""
		for n := 0; n < SessionTurns; n++ {
			s.bodies = append(s.bodies, Encode(RenderResponsesChained(c, n, prev), Compact))
			prev = "resp_" + g.Hex(48)
			s.ResponseIDs = append(s.ResponseIDs, prev)
		}
		ss = append(ss, s)
	}
	for i := range ss {
		ss[i].Turns = len(ss[i].bodies)
		ss[i].File = "sessions/" + ss[i].Name + ".turns.gz"
	}
	return ss
}
