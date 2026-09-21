package domain

import (
	"errors"
	"fmt"
	"time"
)

type LLMProviderType string

const (
	LLMProviderLocal     LLMProviderType = "local"
	LLMProviderOpenAI    LLMProviderType = "openai"
	LLMProviderGroq      LLMProviderType = "groq"
	LLMProviderGemini    LLMProviderType = "gemini"
	LLMProviderAnthropic LLMProviderType = "anthropic"
	// LLMProviderClaudeCode is not an HTTP endpoint at all: a task assigned to
	// an agent on this provider is handed to a headless Claude Code CLI session
	// on the machine that run belongs to — in cloud the assignee's own Mac,
	// reached through the control plane's reverse tunnel; self-hosted, the host
	// this server runs on — which does its own tool use inside the task
	// workspace. Everything the board does around a run — the clone + tt-<key>
	// branch checkout before it, the grounding checks, the verify gate, the
	// commit/PR and the column advance after it — is unchanged; only the middle
	// (the LLM tool-use loop) is delegated. See internal/adapter/cli/claudecode.
	LLMProviderClaudeCode LLMProviderType = "claude_code"
	// LLMProviderCursorAgent is the same kind of thing as LLMProviderClaudeCode
	// — the Cursor CLI (`cursor-agent`) as a process on the runner host, with
	// its own subscription auth, no base URL and no API key. See
	// internal/adapter/cli/cursor for the executor and
	// application/agentfs.FlavorCursor for the catalog it reads
	// (.cursor/rules/*.mdc, the same layout the Cursor IDE uses).
	LLMProviderCursorAgent LLMProviderType = "cursor_agent"
	// LLMProviderAntigravity is the same kind of thing as LLMProviderCursorAgent
	// one line up — Google's Antigravity agentic CLI (`agy`) as a process on the
	// runner host, with its own account auth, no base URL and no API key. See
	// internal/adapter/cli/antigravity for the executor and
	// application/agentfs.FlavorAntigravity for the catalog it reads (the same
	// .claude/ layout claudecode uses).
	LLMProviderAntigravity LLMProviderType = "antigravity"
	// LLMProviderOpencode is the same kind of thing one line up — the OpenCode
	// CLI (`opencode`) as a process on the runner host, with its own provider
	// auth, no base URL and no API key. See internal/adapter/cli/opencode
	// for the executor. Unlike cursor_agent and antigravity it has no dedicated
	// agentfs flavor: OpenCode discovers skills from the same .claude/skills
	// layout claudecode and antigravity already write (confirmed compatible per
	// opencode's own skills docs), and there is no confirmed way to make a
	// custom role definition the ACTIVE session persona from a headless `run`
	// invocation, so the role and rules are folded into the run's prompt
	// instead — see opencode.flattenHistory.
	LLMProviderOpencode LLMProviderType = "opencode"
)

// PinnedLocalEmbeddingModel is the model EMBEDDINGS_BASE_URL bootstraps a local
// embedder with. It is a pin because a vector from another model is not
// comparable to the ones already indexed. Kept here, not read from config:
// nothing about it is configurable, so there is nothing to load.
const PinnedLocalEmbeddingModel = "nomic-embed-text-v1.5"

// PinnedLocalEmbeddingDimensions is nomic-embed-text-v1.5's vector length. It
// exists so a caller can tell a mismatched index apart from a merely-empty one
// without a round trip to LM Studio — see EmbeddingProvenanceStale.
const PinnedLocalEmbeddingDimensions = 768

type LLMProviderDefinition struct {
	Type                  LLMProviderType `json:"type"`
	Label                 string          `json:"label"`
	Description           string          `json:"description"`
	DefaultBaseURL        string          `json:"default_base_url"`
	DefaultModel          string          `json:"default_model"`
	DefaultHeavyModel     string          `json:"default_heavy_model"`
	DefaultTimeoutSeconds int             `json:"default_timeout_seconds"`
	RequiresAPIKey        bool            `json:"requires_api_key"`
	BaseURLRequired       bool            `json:"base_url_required"`
	ModelRequired         bool            `json:"model_required"`
	// HostExecuted marks a provider that is a PROCESS on this host rather than
	// an endpoint on the network. It is the flag every "connect / test /
	// activate / embed with" path keys off: those all mean "reach this base URL
	// with this key", and there is no base URL and no key to reach — the CLI
	// carries its own subscription auth. An agent may still select the provider
	// (that is the whole point); what it may not be is the default
	// chat provider, because a chat turn is an HTTP call and this one cannot
	// serve it.
	HostExecuted bool `json:"host_executed"`
	// Available reports that this provider can actually run work today. It is
	// true for every provider whose engine exists, and false for one that is
	// DECLARED but not yet built.
	//
	// The flag exists because "not built yet" and "not listed" are different
	// products. Leaving a planned CLI out of the definitions entirely means the
	// UI cannot say it is coming; listing it WITHOUT a flag means the UI shows
	// it exactly like a working provider, and the first person who selects it
	// on an agent gets a queue of runs no executor will ever pick up — a
	// failure that surfaces task by task, far from the choice that caused it.
	//
	// So the provider is listed AND refused. Every path that would connect,
	// activate or select it checks this field, on the SERVER, and answers with
	// one sentence naming the reason. The UI's "coming soon" badge is a
	// courtesy on top of that check and never a substitute for it: a client
	// that predates the flag, or a caller hitting the API directly, has to get
	// the same refusal.
	//
	// It is set explicitly on every definition rather than defaulted, so a
	// provider added without a considered value fails closed (unavailable)
	// rather than quietly presenting itself as ready.
	Available bool `json:"available"`
}

type LLMProviderConfig struct {
	ProviderType   LLMProviderType `json:"provider_type"`
	BaseURL        string          `json:"base_url"`
	DefaultModel   string          `json:"default_model"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	Configured     bool            `json:"configured"`
	HasAPIKey      bool            `json:"has_api_key"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type LLMProviderView struct {
	Definition LLMProviderDefinition `json:"definition"`
	Config     LLMProviderConfig     `json:"config"`
	Active     bool                  `json:"active"`
}

type LLMProvidersResponse struct {
	ActiveProvider    LLMProviderType   `json:"active_provider"`
	EmbeddingProvider LLMProviderType   `json:"embedding_provider,omitempty"`
	EmbeddingModel    string            `json:"embedding_model,omitempty"`
	Providers         []LLMProviderView `json:"providers"`
	Endpoints         []LLMEndpoint     `json:"endpoints"`
}

// ConnectLLMProviderRequest — model alanı yoktur: sağlayıcı bağlanırken model
// sorulmaz, kayıtlı değer korunur ya da tanımın varsayılanı kullanılır.
type ConnectLLMProviderRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	// DefaultModel is the provider's fallback model — what an agent that names
	// none is run with. The connect FORM does not ask for it (model choice is
	// per agent), so the field is normally absent and the stored value is kept;
	// it is accepted because a "Custom (OpenAI-compatible)" provider has an
	// empty definition default, which left an API caller no way to set one at
	// all and no sign that the value it sent had been dropped.
	DefaultModel   string `json:"default_model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type TestLLMProviderRequest struct {
	BaseURL        string `json:"base_url"`
	DefaultModel   string `json:"default_model"`
	APIKey         string `json:"api_key"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func AllLLMProviderDefinitions() []LLMProviderDefinition {
	defs := []LLMProviderDefinition{
		{
			Type:                  LLMProviderLocal,
			Label:                 "Custom (OpenAI-compatible)",
			Description:           "Any OpenAI-compatible endpoint (LM Studio, Ollama, vLLM, or a custom IP)",
			DefaultBaseURL:        "http://127.0.0.1:1234/v1",
			DefaultModel:          "",
			DefaultTimeoutSeconds: 300,
			RequiresAPIKey:        false,
			BaseURLRequired:       true,
			ModelRequired:         false,
			Available:             true,
		},
		{
			Type:                  LLMProviderOpenAI,
			Label:                 "OpenAI (GPT)",
			Description:           "GPT models via the OpenAI API",
			DefaultBaseURL:        "https://api.openai.com/v1",
			DefaultModel:          "gpt-4o",
			DefaultTimeoutSeconds: 120,
			RequiresAPIKey:        true,
			BaseURLRequired:       true,
			ModelRequired:         true,
			Available:             true,
		},
		{
			Type:                  LLMProviderGroq,
			Label:                 "Groq",
			Description:           "Groq API (OpenAI-compatible) — free tier, very fast. Models with tool-calling support (e.g. llama-3.3-70b-versatile).",
			DefaultBaseURL:        "https://api.groq.com/openai/v1",
			DefaultModel:          "llama-3.3-70b-versatile",
			DefaultTimeoutSeconds: 120,
			RequiresAPIKey:        true,
			BaseURLRequired:       true,
			ModelRequired:         true,
			Available:             true,
		},
		{
			Type:                  LLMProviderGemini,
			Label:                 "Google Gemini",
			Description:           "Google Gemini API (AI Studio) — free tier, only an API key is required. With no key it falls back to GCP Vertex AI (ADC).",
			DefaultBaseURL:        "https://generativelanguage.googleapis.com",
			DefaultModel:          "gemini-2.5-flash",
			DefaultTimeoutSeconds: 120,
			RequiresAPIKey:        true,
			BaseURLRequired:       false,
			ModelRequired:         true,
			Available:             true,
		},
		{
			Type:           LLMProviderAnthropic,
			Label:          "Anthropic (Claude)",
			Description:    "Anthropic Claude models (native API)",
			DefaultBaseURL: "https://api.anthropic.com/v1",
			// Same tier as the previous default, newer, cheaper per token, and
			// thinking is adaptive by default (no explicit config needed).
			DefaultModel: "claude-sonnet-5",
			// What a subtask the planner rated "hard" escalates to. Declared
			// here rather than in code so every provider carries its own pair
			// and adding a provider is a data change.
			DefaultHeavyModel:     "claude-opus-5",
			DefaultTimeoutSeconds: 120,
			RequiresAPIKey:        true,
			BaseURLRequired:       true,
			ModelRequired:         true,
			Available:             true,
		},
		{
			Type:  LLMProviderClaudeCode,
			Label: "Claude Code",
			Description: "Runs the task in a headless Claude Code session (`claude -p`) on the machine this server runs on, " +
				"using the Claude subscription that CLI is already signed in with. No API key and no endpoint: the binary is " +
				"the connection. Available only where the `claude` binary is on PATH (CLAUDE_CODE_BIN).",
			// No base URL, no key, and no model is required: an empty model lets
			// the CLI pick the one the account defaults to, which is what a
			// subscription user expects.
			DefaultBaseURL:  "",
			DefaultModel:    "",
			RequiresAPIKey:  false,
			BaseURLRequired: false,
			ModelRequired:   false,
			// A CLI session is a whole task, not one HTTP round-trip; the number
			// is here only so a caller reading the definition does not see a zero
			// and infer "no timeout". The executor's own budget is what actually
			// bounds a run (claude_code.run_timeout).
			DefaultTimeoutSeconds: 3600,
			HostExecuted:          true,
			Available:             true,
		},
		{
			Type:  LLMProviderCursorAgent,
			Label: "Cursor",
			Description: "Runs the task in a headless Cursor CLI session (`cursor-agent`) on the runner host, using the " +
				"Cursor subscription that CLI is signed in with. No API key and no endpoint. " +
				"Available only where the `cursor-agent` binary is on PATH (CURSOR_AGENT_BIN).",
			DefaultBaseURL:  "",
			DefaultModel:    "",
			RequiresAPIKey:  false,
			BaseURLRequired: false,
			ModelRequired:   false,
			// Mirrors claude_code so the number reads as "a whole session".
			DefaultTimeoutSeconds: 3600,
			HostExecuted:          true,
			Available:             true,
		},
		{
			Type:  LLMProviderAntigravity,
			Label: "Antigravity",
			Description: "Runs the task in a headless Antigravity (AGY) session on the runner host, using the Google account " +
				"that CLI is signed in with. No API key and no endpoint: the `agy` binary is the connection. " +
				"Available only where the `agy` binary is on PATH (ANTIGRAVITY_BIN).",
			DefaultBaseURL:        "",
			DefaultModel:          "",
			RequiresAPIKey:        false,
			BaseURLRequired:       false,
			ModelRequired:         false,
			DefaultTimeoutSeconds: 3600,
			HostExecuted:          true,
			Available:             true,
		},
		{
			Type:  LLMProviderOpencode,
			Label: "OpenCode",
			Description: "Runs the task in a headless OpenCode session (`opencode run`) on the runner host, using whichever " +
				"model provider that CLI is configured with. No API key and no endpoint of TaskTrooper's own: the `opencode` " +
				"binary is the connection. Available only where the `opencode` binary is on PATH (OPENCODE_BIN).",
			DefaultBaseURL:        "",
			DefaultModel:          "",
			RequiresAPIKey:        false,
			BaseURLRequired:       false,
			ModelRequired:         false,
			DefaultTimeoutSeconds: 3600,
			HostExecuted:          true,
			Available:             true,
		},
	}
	return defs
}

// ProviderAvailable reports whether this provider can be used at all — put on
// an agent, connected, activated. It is the question every guard asks before it
// asks anything else, because a provider with no executor cannot be fixed by
// configuration: no key, no runner and no retry make it work.
//
// An unknown provider type is not available. That is the same answer
// ValidLLMProviderType gives it and keeps a typo from being treated as ready.
func ProviderAvailable(t LLMProviderType) bool {
	def, ok := LLMProviderDefinitionFor(t)
	return ok && def.Available
}

// ErrProviderUnavailable marks every refusal caused by naming a provider that
// is declared but not yet runnable.
//
// It is a sentinel for the same reason ErrHostExecutedUnservable is one: the
// callers sit inside retry loops and behind transports that must choose a
// status code, and both need to tell a PERMANENT refusal from a transient
// failure. Nothing here becomes true on the second attempt — the executor is
// not written — so the answer is 400 and no retry, not 500 and a backoff.
var ErrProviderUnavailable = errors.New("provider is declared but not yet available")

// ErrUnavailableProvider is what a caller gets for naming one. The sentence
// says what the provider is, that the missing half is the executor rather than
// anything the user can configure, and what to do instead — because the
// alternative message ("no client for provider cursor_agent") sends people to
// look for a key they were never going to find.
// LLMProviderLabel is the human name of a provider type: the catalog's own
// label, or the raw type string for one the catalog does not recognise. Empty
// only for an empty type.
func LLMProviderLabel(t LLMProviderType) string {
	if def, ok := LLMProviderDefinitionFor(t); ok && def.Label != "" {
		return def.Label
	}
	return string(t)
}

func ErrUnavailableProvider(t LLMProviderType) error {
	label := LLMProviderLabel(t)
	return fmt.Errorf("%s is not available yet: it is listed so you can see it is coming, but this server has no executor "+
		"for it, so anything selected on it would never run. Pick a provider that is available: %w", label, ErrProviderUnavailable)
}

// RequiresHostExecutor reports whether runs on this provider are executed by a
// local process rather than by the HTTP agent loop. The board runner asks this
// before it picks an executor: the answer decides whether a missing executor is
// a clear "that binary is not on this host" failure or simply not this
// provider's concern.
func RequiresHostExecutor(t LLMProviderType) bool {
	def, ok := LLMProviderDefinitionFor(t)
	return ok && def.HostExecuted
}

// ErrHostExecutedProvider is what every HTTP path must return when it is handed
// a provider that is a local process.
//
// It exists because the alternative was demonstrated in production: a
// claude_code agent's CHAT turn went to the OpenAI-compatible adapter, which
// has no base URL and no key for this provider, built the empty string as its
// endpoint and reported
//
//	llm chat failed: llm failed after 3 attempts: http request:
//	Post "/chat/completions": unsupported protocol scheme ""
//
// — three times, for a configuration that was never wrong. Nothing in that
// sentence names the actual problem, which is that this agent's engine is a
// binary and this host either has no executor for it or the caller never
// consulted one.
//
// So the check is on the PROVIDER and it fails fast, before any client is
// resolved and before the retry loop can multiply it. It is deliberately raised
// from more than one place — the agent loop's entry (which covers chat, board
// and every other loop caller) and the multi-provider client's dispatch (which
// covers anything that reaches a client directly) — because "a claude_code
// request must never become an HTTP request" is an invariant, and an invariant
// guarded in exactly one spot is one refactor away from not being guarded.
func ErrHostExecutedProvider(t LLMProviderType) error {
	label := LLMProviderLabel(t)
	return fmt.Errorf("this agent runs on %s, which is only available on a local runner host: "+
		"its CLI is not installed where this server runs, so there is no engine here to answer with. "+
		"Run this agent on a local runner, or move it to an API-backed provider: %w", label, ErrHostExecutedUnservable)
}

// ErrHostExecutedUnservable marks every refusal caused by a request naming a
// host-executed provider — the agentic refusal above, and the utility refusal
// the multi-provider client raises for a toolless call it will not reroute.
//
// It exists so callers can tell a PERMANENT refusal from a transient failure,
// and there are exactly two things they do with that:
//
//	do not retry. Every one of these call sites sits inside a retry loop built
//	  for a flaky endpoint. A provider that is a local binary this host does not
//	  have will not become one on the third attempt; retrying only multiplies
//	  the same sentence and delays the report.
//	do not degrade quietly. An optional step (a commit-message rewrite, a
//	  summary) may still skip, but it must say which step it skipped and why —
//	  and a load-bearing one (intake, the planner, the golden judge) must fail
//	  its run rather than report a result it never computed.
//
// It replaces a silent fallback that rerouted these calls to whatever HTTP
// provider was set as default. That fallback turned a specific,
// fixable error into whatever the unrelated default provider happened to be
// failing with — for the install it was written for, a dead `gemini-2.0-flash`
// and, before it, an unpaid Mistral.
var ErrHostExecutedUnservable = errors.New("host-executed provider cannot serve this call")

// EmbeddingProvenanceStale reports whether an index built with (indexModel,
// indexDimensions) can still be trusted for similarity search against an install
// now configured for (configuredModel, configuredDimensions).
//
// This is the guard a dimension change needs and pgvector/JSONB storage does
// not provide on its own: nothing at the storage layer stops a query embedded
// with model B from being compared, cosine-similarity-wise, against rows a
// different model A wrote, and the arithmetic does not error — it returns a
// confident, meaningless ranking. Vectors from two models are not comparable
// and need not even share a dimension count (see
// PinnedLocalEmbeddingDimensions), so both the model name and the dimension
// count are checked, independently: a provider that reuses a model name across
// versions with different output sizes would otherwise pass on the name alone.
//
// A blank indexModel is treated as stale whenever a model IS configured: it
// means the index predates the embedding_model/embedding_dims columns
// (migration 116) or was built before an embedding provider was ever chosen,
// and "unknown" must never be read as "matches". A blank configuredModel means
// the caller has nothing to compare against yet, which is not this function's
// failure to report.
func EmbeddingProvenanceStale(indexModel string, indexDimensions int, configuredModel string, configuredDimensions int) bool {
	if configuredModel == "" {
		return false
	}
	if indexModel == "" || indexModel != configuredModel {
		return true
	}
	if indexDimensions > 0 && configuredDimensions > 0 && indexDimensions != configuredDimensions {
		return true
	}
	return false
}

// ProviderDefaultModels is the model pair an agent inherits when it is put on a
// provider that names none of its own: the provider's declared default and,
// when it has one, the stronger model a hard subtask escalates to. Both are
// empty for a host-executed provider that names none — the CLI then runs on
// whatever the operator configured it with, which is the subscription default.
func ProviderDefaultModels(t LLMProviderType) (model, heavy string) {
	def, ok := LLMProviderDefinitionFor(t)
	if !ok {
		return "", ""
	}
	return def.DefaultModel, def.DefaultHeavyModel
}

func LLMProviderDefinitionFor(t LLMProviderType) (LLMProviderDefinition, bool) {
	for _, def := range AllLLMProviderDefinitions() {
		if def.Type == t {
			return def, true
		}
	}
	return LLMProviderDefinition{}, false
}

func ValidLLMProviderType(t string) bool {
	_, ok := LLMProviderDefinitionFor(LLMProviderType(t))
	return ok
}
