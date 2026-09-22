package domain

// LLMModelOption is one entry in a provider's model picker: the value that goes
// on the agent record, and the words a human picks it by. Label exists because
// the value is not self-explanatory for every provider — a Claude Code alias is
// a routing instruction ("opus[1m]" = latest Opus with the 1M-token beta), not
// a name anyone would recognise, and an empty id means "send no --model and let
// the CLI decide".
type LLMModelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ClaudeCodeModels is the single source of truth for what an agent on the
// claude_code provider may be set to; the executor's buildArgs consumes the
// chosen id verbatim.
//
// The list is static and curated rather than discovered because the CLI does
// not enumerate models AND does not validate the flag: `--model bogus-xyz`
// passes through to the API, producing a failed run that has already cost a
// task its turn. Hence a picker, not a free-text box. Every entry was probed
// against the installed CLI, reading the model the session's own init event
// reported:
//
//	<none>       -> whatever the CLI is set to (claude-opus-5[1m] here)
//	fable        -> claude-fable-5
//	opus         -> claude-opus-5
//	sonnet       -> claude-sonnet-5
//	haiku        -> claude-haiku-4-5-20251001
//	opus[1m]     -> claude-opus-5[1m]
//	sonnet[1m]   -> claude-sonnet-5[1m]
//
// Three aliases the CLI also accepts are deliberately NOT offered: haiku[1m]
// resolves but the API refuses it (400 "long context beta not available");
// fable[1m] is accepted then silently downgraded to plain fable; opusplan is an
// interactive plan-mode alias that just reports sonnet headlessly. The bare
// `default` alias behaves exactly like sending no flag, so it is represented by
// the empty id below rather than twice. Refresh this list when the CLI's model
// line-up moves; it is a snapshot of a probe, and it says so.
func ClaudeCodeModels() []LLMModelOption {
	return []LLMModelOption{
		// Empty id = omit --model entirely, leaving the choice to whatever the
		// operator configured the CLI with; first and default because this
		// provider exists to run on the operator's own subscription.
		{ID: "", Label: "CLI default (no --model)"},
		{ID: "fable", Label: "Fable (latest)"},
		{ID: "opus", Label: "Opus (latest)"},
		{ID: "sonnet", Label: "Sonnet (latest)"},
		{ID: "haiku", Label: "Haiku (latest)"},
		{ID: "opus[1m]", Label: "Opus — 1M context"},
		{ID: "sonnet[1m]", Label: "Sonnet — 1M context"},
	}
}

// ModelsForHostExecutedProvider returns the picker list for a provider that runs
// as a local process, and false for every HTTP-reachable provider that can be
// asked for its own list. A host-executed provider with no declared ModelOptions
// gets an empty list rather than a 502: adding a curated picker for a new CLI
// is a data change in its definition, not a new arm here.
func ModelsForHostExecutedProvider(t LLMProviderType) ([]LLMModelOption, bool) {
	if !RequiresHostExecutor(t) {
		return nil, false
	}
	def, ok := LLMProviderDefinitionFor(t)
	if !ok || len(def.ModelOptions) == 0 {
		return []LLMModelOption{}, true
	}
	return def.ModelOptions, true
}
