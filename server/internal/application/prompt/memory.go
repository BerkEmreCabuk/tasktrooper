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

var memoryDuplicateSaveKey = Define[struct{}]("guard.memory_duplicate_save", struct{}{})
var memoryPromotedToSkillKey = Define[struct{}]("tool_results.memory_promoted_to_skill", struct{}{})
var memoryScopeProjectRequiredSaveKey = Define[struct{}]("guard.memory_scope_project_required_save", struct{}{})
var memoryScopeProjectRequiredSearchKey = Define[struct{}]("guard.memory_scope_project_required_search", struct{}{})

// MemoryDuplicateSaveNote is what save_memory answers instead of writing a
// new row when the content is already remembered in the same bucket.
func MemoryDuplicateSaveNote() string { return Text(memoryDuplicateSaveKey) }

// MemoryPromotedToSkillNote is what save_memory answers when the content was
// reusable know-how and got filed in the skill catalog instead.
func MemoryPromotedToSkillNote() string { return Text(memoryPromotedToSkillKey) }

// MemoryScopeProjectRequiredSaveText is save_memory's refusal for
// scope=project with no repository in context; it names scope=global as the
// way out because a save always has one.
func MemoryScopeProjectRequiredSaveText() string { return Text(memoryScopeProjectRequiredSaveKey) }

// MemoryScopeProjectRequiredSearchText is search_memory's version of the same
// refusal; a search has no equally good fallback, so it does not suggest one.
func MemoryScopeProjectRequiredSearchText() string {
	return Text(memoryScopeProjectRequiredSearchKey)
}
