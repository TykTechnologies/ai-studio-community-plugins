package compress

// EngineJob is one leaf for the external engine, as levels 1–3 left it.
type EngineJob struct {
	Role string
	Text string
	// Protect are the protect-pattern matches in Text, which the answer
	// must keep (LLMLingua-2 force_tokens).
	Protect []string
}

// EngineResult is the engine's answer for one job. Changed is false when
// the job's text is to be sent as it is (a passthrough, memoised or not);
// Failed marks a failure under failure_mode open.
type EngineResult struct {
	Text    string
	Changed bool
	Failed  bool
}

// EngineFunc compresses jobs; result i answers job i. A non-nil error fails
// the body (failure_mode closed).
type EngineFunc func(model string, jobs []EngineJob) ([]EngineResult, error)
