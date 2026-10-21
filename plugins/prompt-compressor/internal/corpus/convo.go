package corpus

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// ToolDef is a tool the agent offers the model.
type ToolDef struct {
	Name, Description string
	Schema            *Obj
}

// Call is one tool call and the output the client sends back.
type Call struct {
	ID   string
	Name string
	Args *Obj
	Out  Output
	// Structured delivers Out.JSON as a structured result where the format
	// has one (Bedrock json block, Google functionResponse object).
	Structured bool
	// Blocks delivers the result as a block array (Anthropic, OpenAI text
	// parts) instead of a string.
	Blocks bool
	// Image adds a screenshot block to a block-array result.
	Image bool
	// CacheControl marks the result block (Anthropic) with cache_control.
	CacheControl bool
	// ImageData is the PNG for Image, fixed when the call is built.
	ImageData []byte
	// Wrapped delivers the output the way Codex's shell tool does: a JSON
	// string {"output": ..., "metadata": {...}} inside the output string.
	Wrapped bool
}

// Step is one assistant turn: optional reasoning and text, then tool calls.
type Step struct {
	Thinking string
	// Signature signs Thinking (Anthropic, Bedrock reasoningContent).
	Signature string
	// Redacted is a redacted_thinking block's data (Anthropic); empty for
	// none.
	Redacted string
	// Encrypted is an OpenAI Responses reasoning item's encrypted_content;
	// Summary its summary text.
	Encrypted, Summary string
	// ThoughtSig is a Gemini thoughtSignature on the first function call.
	ThoughtSig string
	Text       string
	Calls      []Call
	// After is user text sent with the tool results (a follow-up
	// instruction typed while the agent worked).
	After string
}

// Convo is an agent conversation every format renders.
type Convo struct {
	Model  string
	System string
	Tools  []ToolDef
	Task   string
	Steps  []Step
}

// codingTools are the tools of a generic coding agent.
func codingTools() []ToolDef {
	str := func(desc string) *Obj { return O("type", "string", "description", desc) }
	return []ToolDef{
		{"bash", "Run a shell command in the repository and return its combined stdout and stderr. Commands time out after the given number of milliseconds (default 120000). Prefer non-interactive flags.",
			O("type", "object", "properties", O("command", str("The command to run."), "description", str("A five to ten word summary of what the command does."), "timeout", O("type", "integer", "description", "Timeout in milliseconds.", "maximum", 600000)), "required", []string{"command"}, "additionalProperties", false)},
		{"read_file", "Read a file from the repository. Returns at most 2000 lines, numbered; use offset and limit for longer files.",
			O("type", "object", "properties", O("path", str("Path relative to the repository root."), "offset", O("type", "integer", "minimum", 0), "limit", O("type", "integer", "minimum", 1)), "required", []string{"path"}, "additionalProperties", false)},
		{"edit_file", "Replace an exact string in a file. The old string must occur exactly once unless replace_all is set.",
			O("type", "object", "properties", O("path", str("Path relative to the repository root."), "old_string", str("Text to replace."), "new_string", str("Replacement text."), "replace_all", O("type", "boolean", "default", false)), "required", []string{"path", "old_string", "new_string"}, "additionalProperties", false)},
		{"grep", "Search file contents with ripgrep. Returns matching lines as path:line:text.",
			O("type", "object", "properties", O("pattern", str("A ripgrep regular expression."), "path", str("Directory or file to search."), "glob", str("Optional glob filter, e.g. *.go.")), "required", []string{"pattern"}, "additionalProperties", false)},
		{"web_fetch", "Fetch a URL and return the page body.",
			O("type", "object", "properties", O("url", O("type", "string", "format", "uri")), "required", []string{"url"}, "additionalProperties", false)},
		{"mcp__tracker__search_issues", "Search issues in the team's tracker. Returns matching issues with state, assignee and labels.",
			O("type", "object", "properties", O("query", str("Free-text query."), "state", O("type", "string", "enum", []string{"open", "closed", "all"}), "limit", O("type", "integer", "minimum", 1, "maximum", 250)), "required", []string{"query"})},
	}
}

const codingSystem = `You are a coding agent working in the Acme Ledger monorepo at /home/dev/src/ledger. You help engineers fix bugs, write tests and ship changes safely.

# Working rules
- Read before you edit. Never guess a file's contents.
- Run the narrowest test that proves a change, then the package's full suite.
- Keep diffs minimal and match the surrounding style.
- Never print, log or commit secrets. Credentials come from the environment.
- Ask before running anything destructive (migrations, deletes, force pushes).

# Repository layout
- cmd/ and internal/: the Go API (module git.example.com/acme/ledger)
- worker/: the Python reconciliation worker (pytest)
- web/: the TypeScript web app (jest, vite)
- parser/: the Rust statement parser crate (cargo)
- reports/: the Java reporting service (maven; gradle for the app wrapper)
- deploy/: Kubernetes manifests and Terraform for eu-west-1

# Tools
Use bash for builds, tests and git. Use grep before reading many files. Tool output can be long; summarise what matters instead of repeating it.

# Style
Answer in short paragraphs. Quote file paths as path:line. When a test fails, say which assertion and why before proposing a fix.`

// toolFor maps an output kind to the tool call that produces it.
func toolFor(o Output) (string, *Obj) {
	switch o.Kind {
	case HTML:
		return "web_fetch", O("url", strings.TrimPrefix(o.Command, "web_fetch "))
	case MCP:
		return "mcp__tracker__search_issues", O("query", "rounding export", "state", "all", "limit", 100)
	case Ripgrep:
		return "grep", O("pattern", strings.Trim(strings.TrimPrefix(strings.TrimPrefix(o.Command, "rg -n --heading "), "rg -n "), "'"), "path", ".")
	}
	return "bash", O("command", o.Command, "description", describe(o))
}

func describe(o Output) string {
	switch o.Kind {
	case Pytest, Jest, GoTest, Cargo:
		return "Run the test suite"
	case Maven, Gradle:
		return "Build and test the reporting service"
	case DockerBuild:
		return "Build the web image"
	case Kubectl:
		return "Inspect the cluster"
	case TerraformPlan:
		return "Plan the infrastructure change"
	case GitDiff:
		return "Show recent changes"
	case DirListing:
		return "List files"
	case JSONAPI:
		return "Fetch issues from the API"
	case CSV:
		return "Export recent orders"
	}
	return "Run a command"
}

// NewCall builds the call that produced o.
func NewCall(g *R, o Output, idStyle string) Call {
	name, args := toolFor(o)
	return Call{ID: CallID(g, idStyle), Name: name, Args: args, Out: o}
}

// CallID returns a tool call id in a vendor's style.
func CallID(g *R, style string) string {
	switch style {
	case "anthropic":
		return "toolu_01" + g.Alnum(22)
	case "openai":
		return "call_" + g.Alnum(24)
	case "bedrock":
		return "tooluse_" + g.Alnum(22)
	case "google":
		return "" // Gemini calls carry no id on the wire by default
	}
	panic("gen.CallID: unknown style " + style)
}

// pngImage returns a small deterministic PNG (a screenshot stand-in).
func pngImage(g *R) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	base := g.Bytes(3)
	for y := 0; y < 16; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{base[0] + uint8(x*8), base[1] + uint8(y*12), base[2], 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func argsJSON(a *Obj) string { return string(Encode(a, Compact)) }

func fmtThinking(g *R, topic string) string {
	return fmt.Sprintf("The %s output needs a closer look. %s %s", topic, g.Sentence(g.N(10, 25)), g.Sentence(g.N(8, 20)))
}
