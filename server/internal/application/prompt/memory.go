package prompt

// domain.MemoryRunLogCode returns only a stable code (domain must not carry
// LLM-facing prose); this is where it becomes the sentence two different
// audiences see: adapter/tools/memory's save_memory tool result (a model-
// facing refusal) and application/evolution's reflection-outcome detail (a
// human audit trail) — kept in one place so both read the identical wording
// instead of duplicating it, or showing the bare code, in Go.

type memoryRunLogReasonInput struct {
	Code string
}

var memoryRunLogReasonKey = Define("guard.memory_run_log_reason", memoryRunLogReasonInput{Code: "pinned_pr"})
var memoryRunLogHintKey = Define[struct{}]("guard.memory_run_log_hint", struct{}{})

// MemoryRunLogReasonText renders the human-readable clause for a
// domain.MemoryRunLogCode result (e.g. "it is pinned to one pull request").
func MemoryRunLogReasonText(code string) string {
	return memoryRunLogReasonKey.Render(memoryRunLogReasonInput{Code: code})
}

// MemoryRunLogHintText renders the paragraph steering a rejected save
// towards where the note actually belongs.
func MemoryRunLogHintText() string {
	return memoryRunLogHintKey.Render(struct{}{})
}
