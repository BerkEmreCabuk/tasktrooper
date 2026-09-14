package domain

// LLMModelOption is one entry in a provider's model picker: the value that goes
// on the agent record, and the words a human picks it by.
//
// Label exists because the value is not self-explanatory for every provider. A
// Claude Code alias is a routing instruction ("opus[1m]" means the latest Opus
// with the 1M-token context beta), not a model name anyone would recognise, and
// an empty id means something that cannot be written down at all — "send no
// --model and let the CLI decide".
type LLMModelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ClaudeCodeModels is the single source of truth for what an agent on the
// claude_code provider may be set to. The /v1/models endpoint serves it and the
// executor's buildArgs consumes the chosen id verbatim.
//
// # Why this list is static and curated rather than discovered
//
// There is no CLI subcommand that enumerates models — `claude --help` documents
// --model only prose-wise ("an alias for the latest model (e.g. 'fable',
// 'opus', or 'sonnet') or a model's full name (e.g. 'claude-fable-5')") and
// there is nothing to query. Worse, the CLI does not VALIDATE the flag either:
// probing v2.1.220 with `--model bogus-xyz` produced a session whose init event
// read `"model":"bogus-xyz"` and whose result was `is_error:true` — "There's an
// issue with the selected model (bogus-xyz)". The alias is passed through to the
// API untouched, so a typo is not a rejected flag, it is a failed run that has
// already cost a task its turn. That is precisely why the web UI must offer a
// picker instead of a free-text box, and why the picker's contents have to be
// asserted here rather than inferred from the CLI at runtime.
//
// # How the list was settled
//
// Every entry was probed against the installed CLI with
// `claude -p --model <alias> --output-format stream-json --verbose "hi"`,
// reading the model the session's own `system/init` event reported:
//
//	<none>       -> claude-opus-5[1m]              (whatever the CLI is set to)
//	fable        -> claude-fable-5
//	opus         -> claude-opus-5
//	sonnet       -> claude-sonnet-5
//	haiku        -> claude-haiku-4-5-20251001
//	opus[1m]     -> claude-opus-5[1m]
//	sonnet[1m]   -> claude-sonnet-5[1m]
//
// Three aliases the CLI also accepts are deliberately NOT offered:
//
//   - haiku[1m] resolves (claude-haiku-4-5-20251001[1m]) but the API refuses it:
//     "400 The long context beta is not yet available for this subscription".
//     An option that always fails is worse than an absent one.
//   - fable[1m] is accepted and then silently downgraded — the init event reports
//     plain claude-fable-5. Offering it would promise a 1M context and quietly
//     deliver the ordinary one.
//   - opusplan is accepted but is an interactive plan-mode routing alias; in
//     headless -p mode it simply reported claude-sonnet-5, so on this path it is
//     an obscure spelling of "sonnet".
//
// The bare `default` alias is accepted too and behaves exactly like sending no
// flag at all, so it is represented by the empty id below rather than twice.
//
// Refresh this list (and the transcript above) when the CLI's model line-up
// moves; it is a snapshot of a probe, and it says so.
func ClaudeCodeModels() []LLMModelOption {
	return []LLMModelOption{
		// Empty id = omit --model entirely, which leaves the choice to whatever
		// the operator configured the CLI with (~/.claude/settings.json, the
		// ANTHROPIC_MODEL env, the /model they last picked). It is first and it
		// is the default for a reason: this provider exists to run on the
		// operator's own subscription, and their own CLI default is the setting
		// most likely to be the one they want.
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
// as a local process, and false for every provider that is reachable over HTTP
// and can therefore be asked for its own model list.
//
// It is the switch the /v1/models handler makes: a host-executed provider has no
// endpoint to query, so listing its models is a lookup here rather than a
// request anywhere.
func ModelsForHostExecutedProvider(t LLMProviderType) ([]LLMModelOption, bool) {
	if !RequiresHostExecutor(t) {
		return nil, false
	}
	switch t {
	case LLMProviderClaudeCode:
		return ClaudeCodeModels(), true
	default:
		// A host-executed provider that has not declared its models yet. An
		// empty list is the honest answer — the picker shows nothing to choose
		// rather than the endpoint 502-ing on a client that cannot be dialled.
		return []LLMModelOption{}, true
	}
}
