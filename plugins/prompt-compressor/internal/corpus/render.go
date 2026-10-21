package corpus

import (
	"encoding/base64"
	"strings"
)

// Renderers turn a Convo into a request body in one format. They draw no
// randomness: everything variable is fixed when the Convo is built, so the
// body after n steps is a prefix-extension of the body after n-1 steps
// (cache_control placement aside, which the options control).

// OpenAIOpts tunes RenderOpenAI.
type OpenAIOpts struct {
	// Developer sends the system prompt with the developer role.
	Developer bool
	// Stream asks for a streamed response with usage.
	Stream bool
}

// RenderOpenAI renders Chat Completions after n steps.
func RenderOpenAI(c *Convo, n int, o OpenAIOpts) *Obj {
	role := "system"
	if o.Developer {
		role = "developer"
	}
	msgs := []any{O("role", role, "content", c.System), O("role", "user", "content", c.Task)}
	for _, s := range c.Steps[:n] {
		var calls []any
		for _, cl := range s.Calls {
			calls = append(calls, O("id", cl.ID, "type", "function", "function", O("name", cl.Name, "arguments", argsJSON(cl.Args))))
		}
		var content any
		if s.Text != "" {
			content = s.Text
		}
		m := O("role", "assistant", "content", content)
		if len(calls) > 0 {
			m.Set("tool_calls", calls)
		}
		msgs = append(msgs, m)
		for _, cl := range s.Calls {
			var out any = cl.Out.Text
			if cl.Blocks {
				out = []any{O("type", "text", "text", cl.Out.Text)}
			}
			msgs = append(msgs, O("role", "tool", "tool_call_id", cl.ID, "content", out))
		}
		if s.After != "" {
			msgs = append(msgs, O("role", "user", "content", s.After))
		}
	}
	var tools []any
	for _, t := range c.Tools {
		tools = append(tools, O("type", "function", "function", O("name", t.Name, "description", t.Description, "parameters", t.Schema)))
	}
	body := O("model", c.Model, "messages", msgs, "tools", tools, "tool_choice", "auto", "parallel_tool_calls", true, "temperature", 0.2)
	if o.Stream {
		body.Set("stream", true).Set("stream_options", O("include_usage", true))
	}
	return body
}

// AnthropicOpts tunes RenderAnthropic.
type AnthropicOpts struct {
	// SystemCache sends the system prompt as a block with cache_control.
	SystemCache bool
	// MovingCache puts cache_control on the last block of the last user
	// message, as coding agents do every turn, so the breakpoint moves.
	MovingCache bool
	// ToolsCache marks the last tool definition with cache_control.
	ToolsCache bool
	// Thinking enables extended thinking.
	Thinking bool
	// Bedrock renders an InvokeModel body (anthropic_version, no model).
	Bedrock bool
	// TaskBlocks are extra blocks sent with the task (images, documents).
	TaskBlocks []any
	// Extra top-level members (context_management and the like).
	Extra *Obj
	// Stream asks for a streamed response.
	Stream bool
}

func ephemeral() *Obj { return O("type", "ephemeral") }

// RenderAnthropic renders a Messages body after n steps.
func RenderAnthropic(c *Convo, n int, o AnthropicOpts) *Obj {
	var system any = c.System
	if o.SystemCache {
		system = []any{O("type", "text", "text", c.System, "cache_control", ephemeral())}
	}
	var task any = c.Task
	if o.MovingCache {
		// Agents that move the breakpoint send every user turn as blocks.
		task = []any{O("type", "text", "text", c.Task)}
	}
	if len(o.TaskBlocks) > 0 {
		task = append(append([]any{}, o.TaskBlocks...), O("type", "text", "text", c.Task))
	}
	msgs := []any{O("role", "user", "content", task)}
	for _, s := range c.Steps[:n] {
		var blocks []any
		if s.Thinking != "" {
			blocks = append(blocks, O("type", "thinking", "thinking", s.Thinking, "signature", s.Signature))
		}
		if s.Redacted != "" {
			blocks = append(blocks, O("type", "redacted_thinking", "data", s.Redacted))
		}
		if s.Text != "" {
			blocks = append(blocks, O("type", "text", "text", s.Text))
		}
		for _, cl := range s.Calls {
			blocks = append(blocks, O("type", "tool_use", "id", cl.ID, "name", cl.Name, "input", cl.Args))
		}
		msgs = append(msgs, O("role", "assistant", "content", blocks))
		if len(s.Calls) == 0 && s.After == "" {
			continue
		}
		var results []any
		for _, cl := range s.Calls {
			r := O("type", "tool_result", "tool_use_id", cl.ID)
			if cl.Blocks {
				content := []any{O("type", "text", "text", cl.Out.Text)}
				if cl.Image {
					content = append(content, O("type", "image", "source", O("type", "base64", "media_type", "image/png", "data", base64.StdEncoding.EncodeToString(cl.ImageData))))
				}
				r.Set("content", content)
			} else {
				r.Set("content", cl.Out.Text)
			}
			if cl.Out.IsError {
				r.Set("is_error", true)
			}
			if cl.CacheControl {
				r.Set("cache_control", ephemeral())
			}
			results = append(results, r)
		}
		if s.After != "" {
			results = append(results, O("type", "text", "text", s.After))
		}
		msgs = append(msgs, O("role", "user", "content", results))
	}
	if o.MovingCache {
		msgs = withMovingCache(msgs)
	}
	var tools []any
	for i, t := range c.Tools {
		td := O("name", t.Name, "description", t.Description, "input_schema", t.Schema)
		if o.ToolsCache && i == len(c.Tools)-1 {
			td.Set("cache_control", ephemeral())
		}
		tools = append(tools, td)
	}
	var body *Obj
	if o.Bedrock {
		body = O("anthropic_version", "bedrock-2023-05-31", "max_tokens", 8192)
	} else {
		body = O("model", c.Model, "max_tokens", 16000)
	}
	body.Set("system", system).Set("messages", msgs).Set("tools", tools)
	if o.Thinking {
		body.Set("thinking", O("type", "enabled", "budget_tokens", 6000))
	} else {
		body.Set("temperature", 0.2)
	}
	if o.Extra != nil {
		for _, e := range o.Extra.kv {
			body.Set(e.k, e.v)
		}
	}
	if o.Stream {
		body.Set("stream", true)
	}
	return body
}

// withMovingCache copies the last user message with cache_control on its
// last block; earlier messages are shared untouched.
func withMovingCache(msgs []any) []any {
	out := append([]any(nil), msgs...)
	for i := len(out) - 1; i >= 0; i-- {
		m := out[i].(*Obj)
		if m.Get("role") != "user" {
			continue
		}
		m = m.Clone()
		switch content := m.Get("content").(type) {
		case string:
			m.Set("content", []any{O("type", "text", "text", content, "cache_control", ephemeral())})
		case []any:
			blocks := append([]any(nil), content...)
			last := blocks[len(blocks)-1].(*Obj).Clone()
			last.Set("cache_control", ephemeral())
			blocks[len(blocks)-1] = last
			m.Set("content", blocks)
		}
		out[i] = m
		return out
	}
	return out
}

// BedrockOpts tunes RenderBedrock.
type BedrockOpts struct {
	// CachePoints adds cachePoint blocks after the system prompt and the
	// tool definitions.
	CachePoints bool
	// Reasoning sends each step's thinking as reasoningContent.
	Reasoning bool
}

// RenderBedrock renders a Converse body after n steps.
func RenderBedrock(c *Convo, n int, o BedrockOpts) *Obj {
	system := []any{O("text", c.System)}
	if o.CachePoints {
		system = append(system, O("cachePoint", O("type", "default")))
	}
	msgs := []any{O("role", "user", "content", []any{O("text", c.Task)})}
	for _, s := range c.Steps[:n] {
		var blocks []any
		if o.Reasoning && s.Thinking != "" {
			blocks = append(blocks, O("reasoningContent", O("reasoningText", O("text", s.Thinking, "signature", s.Signature))))
		}
		if s.Text != "" {
			blocks = append(blocks, O("text", s.Text))
		}
		for _, cl := range s.Calls {
			blocks = append(blocks, O("toolUse", O("toolUseId", cl.ID, "name", cl.Name, "input", cl.Args)))
		}
		msgs = append(msgs, O("role", "assistant", "content", blocks))
		if len(s.Calls) == 0 && s.After == "" {
			continue
		}
		var results []any
		for _, cl := range s.Calls {
			var content []any
			if cl.Structured && cl.Out.JSON != nil {
				content = []any{O("json", cl.Out.JSON)}
			} else {
				content = []any{O("text", cl.Out.Text)}
			}
			status := "success"
			if cl.Out.IsError {
				status = "error"
			}
			results = append(results, O("toolResult", O("toolUseId", cl.ID, "content", content, "status", status)))
		}
		if s.After != "" {
			results = append(results, O("text", s.After))
		}
		msgs = append(msgs, O("role", "user", "content", results))
	}
	var tools []any
	for _, t := range c.Tools {
		tools = append(tools, O("toolSpec", O("name", t.Name, "description", t.Description, "inputSchema", O("json", t.Schema))))
	}
	if o.CachePoints {
		tools = append(tools, O("cachePoint", O("type", "default")))
	}
	return O("messages", msgs, "system", system, "inferenceConfig", O("maxTokens", 8192, "temperature", 0.2), "toolConfig", O("tools", tools))
}

// GoogleOpts tunes RenderGoogle.
type GoogleOpts struct {
	// FunctionRole sends function responses with role "function" (older
	// Vertex clients) instead of "user".
	FunctionRole bool
}

// RenderGoogle renders a generateContent body (Google AI or Vertex) after
// n steps.
func RenderGoogle(c *Convo, n int, o GoogleOpts) *Obj {
	contents := []any{O("role", "user", "parts", []any{O("text", c.Task)})}
	for _, s := range c.Steps[:n] {
		var parts []any
		if s.Text != "" {
			parts = append(parts, O("text", s.Text))
		}
		for i, cl := range s.Calls {
			p := O("functionCall", O("name", cl.Name, "args", cl.Args))
			if i == 0 && s.ThoughtSig != "" {
				p.Set("thoughtSignature", s.ThoughtSig)
			}
			parts = append(parts, p)
		}
		contents = append(contents, O("role", "model", "parts", parts))
		if len(s.Calls) == 0 && s.After == "" {
			continue
		}
		var results []any
		for _, cl := range s.Calls {
			var resp *Obj
			if cl.Structured && cl.Out.JSON != nil {
				resp = O("result", cl.Out.JSON)
			} else {
				resp = O("output", cl.Out.Text)
			}
			results = append(results, O("functionResponse", O("name", cl.Name, "response", resp)))
		}
		role := "user"
		if o.FunctionRole {
			role = "function"
		}
		contents = append(contents, O("role", role, "parts", results))
		if s.After != "" {
			contents = append(contents, O("role", "user", "parts", []any{O("text", s.After)}))
		}
	}
	var decls []any
	for _, t := range c.Tools {
		decls = append(decls, O("name", t.Name, "description", t.Description, "parameters", googleSchema(t.Schema)))
	}
	return O("contents", contents, "systemInstruction", O("parts", []any{O("text", c.System)}),
		"tools", []any{O("functionDeclarations", decls)},
		"generationConfig", O("temperature", 0.2, "maxOutputTokens", 8192, "thinkingConfig", O("thinkingBudget", 2048)))
}

// googleSchema drops JSON Schema members Gemini's OpenAPI subset rejects.
func googleSchema(s *Obj) *Obj {
	out := &Obj{}
	for _, e := range s.kv {
		switch e.k {
		case "additionalProperties", "format", "default":
			continue
		case "properties":
			props := &Obj{}
			for _, p := range e.v.(*Obj).kv {
				props.Set(p.k, googleSchema(p.v.(*Obj)))
			}
			out.Set("properties", props)
			continue
		}
		out.Set(e.k, e.v)
	}
	return out
}

// RenderOllama renders an /api/chat body after n steps.
func RenderOllama(c *Convo, n int) *Obj {
	msgs := []any{O("role", "system", "content", c.System), O("role", "user", "content", c.Task)}
	for _, s := range c.Steps[:n] {
		var calls []any
		for _, cl := range s.Calls {
			calls = append(calls, O("function", O("name", cl.Name, "arguments", cl.Args)))
		}
		m := O("role", "assistant", "content", s.Text)
		if len(calls) > 0 {
			m.Set("tool_calls", calls)
		}
		msgs = append(msgs, m)
		for _, cl := range s.Calls {
			msgs = append(msgs, O("role", "tool", "content", cl.Out.Text, "tool_name", cl.Name))
		}
		if s.After != "" {
			msgs = append(msgs, O("role", "user", "content", s.After))
		}
	}
	var tools []any
	for _, t := range c.Tools {
		tools = append(tools, O("type", "function", "function", O("name", t.Name, "description", t.Description, "parameters", t.Schema)))
	}
	return O("model", c.Model, "messages", msgs, "tools", tools, "stream", false, "options", O("num_ctx", 32768, "temperature", 0.2))
}

// ResponsesOpts tunes RenderResponses.
type ResponsesOpts struct {
	// Codex renders a Codex CLI style body: shell and apply_patch tools,
	// store false, encrypted reasoning carried in the history.
	Codex bool
	// CacheKey is the prompt_cache_key (Codex sends the session id).
	CacheKey string
	// Extra top-level members.
	Extra *Obj
	// Prefix items before the conversation (a compaction item).
	Prefix []any
}

// RenderResponses renders a Responses body carrying the full history after
// n steps.
func RenderResponses(c *Convo, n int, o ResponsesOpts) *Obj {
	input := append([]any{}, o.Prefix...)
	input = append(input, O("type", "message", "role", "user", "content", []any{O("type", "input_text", "text", c.Task)}))
	for _, s := range c.Steps[:n] {
		input = append(input, responsesStepItems(s)...)
	}
	return responsesBody(c, input, o)
}

func responsesStepItems(s Step) []any {
	var items []any
	if s.Encrypted != "" {
		var summary []any
		if s.Summary != "" {
			summary = []any{O("type", "summary_text", "text", s.Summary)}
		} else {
			summary = []any{}
		}
		items = append(items, O("type", "reasoning", "summary", summary, "content", nil, "encrypted_content", s.Encrypted))
	}
	if s.Text != "" {
		items = append(items, O("type", "message", "role", "assistant", "content", []any{O("type", "output_text", "text", s.Text, "annotations", []any{})}))
	}
	for _, cl := range s.Calls {
		if cl.Name == "apply_patch" {
			items = append(items, O("type", "custom_tool_call", "name", cl.Name, "input", cl.Args.Get("input"), "call_id", cl.ID))
		} else {
			items = append(items, O("type", "function_call", "name", cl.Name, "arguments", argsJSON(cl.Args), "call_id", cl.ID))
		}
	}
	items = append(items, responsesOutputs(s)...)
	if s.After != "" {
		items = append(items, O("type", "message", "role", "user", "content", []any{O("type", "input_text", "text", s.After)}))
	}
	return items
}

func responsesOutputs(s Step) []any {
	var items []any
	for _, cl := range s.Calls {
		out := cl.Out.Text
		if cl.Wrapped {
			exit := 0
			if cl.Out.IsError {
				exit = 1
			}
			out = string(Encode(O("output", cl.Out.Text, "metadata", O("exit_code", exit, "duration_seconds", 2.4)), Compact))
		}
		typ := "function_call_output"
		if cl.Name == "apply_patch" {
			typ = "custom_tool_call_output"
		}
		items = append(items, O("type", typ, "call_id", cl.ID, "output", out))
	}
	return items
}

func responsesBody(c *Convo, input []any, o ResponsesOpts) *Obj {
	var tools []any
	for _, t := range c.Tools {
		if t.Name == "apply_patch" {
			tools = append(tools, O("type", "custom", "name", t.Name, "description", t.Description, "format", O("type", "text")))
			continue
		}
		tools = append(tools, O("type", "function", "name", t.Name, "description", t.Description, "strict", false, "parameters", t.Schema))
	}
	body := O("model", c.Model, "instructions", c.System, "input", input, "tools", tools, "tool_choice", "auto", "parallel_tool_calls", false)
	if o.Codex {
		body.Set("reasoning", O("effort", "medium", "summary", "auto")).
			Set("store", false).Set("stream", true).
			Set("include", []string{"reasoning.encrypted_content"})
	}
	if o.CacheKey != "" {
		body.Set("prompt_cache_key", o.CacheKey)
	}
	if o.Extra != nil {
		for _, e := range o.Extra.kv {
			body.Set(e.k, e.v)
		}
	}
	return body
}

// RenderResponsesChained renders turn n of a session that chains with
// previous_response_id: turn 0 sends the task, later turns only the
// previous step's tool outputs.
func RenderResponsesChained(c *Convo, n int, prevID string) *Obj {
	var input []any
	if n == 0 {
		input = []any{O("type", "message", "role", "user", "content", []any{O("type", "input_text", "text", c.Task)})}
	} else {
		s := c.Steps[n-1]
		input = responsesOutputs(s)
		if s.After != "" {
			input = append(input, O("type", "message", "role", "user", "content", []any{O("type", "input_text", "text", s.After)}))
		}
	}
	body := responsesBody(c, input, ResponsesOpts{})
	body.Set("store", true)
	if prevID != "" {
		body.Set("previous_response_id", prevID)
	}
	return body
}

func codexTools() []ToolDef {
	return []ToolDef{
		{"shell", "Runs a shell command and returns its output.",
			O("type", "object", "properties", O("command", O("type", "array", "items", O("type", "string"), "description", "The command to execute"), "workdir", O("type", "string", "description", "The working directory to execute the command in"), "timeout_ms", O("type", "number", "description", "The timeout for the command in milliseconds")), "required", []string{"command"}, "additionalProperties", false)},
		{"apply_patch", "Use the `apply_patch` tool to edit files. The input is a patch in the *** Begin Patch / *** End Patch envelope.", nil},
		{"update_plan", "Updates the task plan.",
			O("type", "object", "properties", O("explanation", O("type", "string"), "plan", O("type", "array", "items", O("type", "object", "properties", O("step", O("type", "string"), "status", O("type", "string")), "required", []string{"step", "status"}, "additionalProperties", false))), "required", []string{"plan"}, "additionalProperties", false)},
		codingTools()[5],
	}
}

func codexArgs(cmd string) *Obj {
	return O("command", []string{"bash", "-lc", cmd}, "workdir", workDir, "timeout_ms", 120000)
}

// codexShell turns a call into what Codex sends: MCP tools stay function
// calls (Codex names them server__tool), everything else runs through its
// shell tool with the output wrapped in a JSON envelope.
func codexShell(cl *Call) {
	switch cl.Out.Kind {
	case MCP:
		return
	case HTML:
		cl.Name, cl.Args, cl.Wrapped = "shell", codexArgs("curl -sL "+strings.TrimPrefix(cl.Out.Command, "web_fetch ")), true
	default:
		cl.Name, cl.Args, cl.Wrapped = "shell", codexArgs(cl.Out.Command), true
	}
}

func ollamaGenerate(model, system, prompt string) *Obj {
	return O("model", model, "system", system, "prompt", prompt, "stream", false, "options", O("num_ctx", 16384))
}
