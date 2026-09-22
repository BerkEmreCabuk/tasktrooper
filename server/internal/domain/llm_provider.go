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
	// LLMProviderClaudeCode is not an HTTP endpoint at all: a task on this
	// provider is handed to a headless Claude Code CLI session on the machine
	// that run belongs to, which does its own tool use inside the task
	// workspace. Everything the board does around a run — clone + tt-<key>
	// branch checkout, grounding, verify, commit/PR — is unchanged; only the
	// LLM tool-use loop is delegated (internal/adapter/cli/claudecode).
	LLMProviderClaudeCode LLMProviderType = "claude_code"
	// LLMProviderCursorAgent is a process on the runner host like Claude Code:
	// the Cursor CLI (`cursor-agent`) with its own subscription auth, no base
	// URL and no API key. See internal/adapter/cli/cursor and
	// application/agentfs.FlavorCursor for the catalog it reads
	// (.cursor/rules/*.mdc).
	LLMProviderCursorAgent LLMProviderType = "cursor_agent"
	// LLMProviderAntigravity is Google's Antigravity agentic CLI (`agy`) as a
	// process on the runner host, with its own account auth. See
	// internal/adapter/cli/antigravity and application/agentfs.FlavorAntigravity
	// (which reads the same .claude/ layout claudecode uses).
	LLMProviderAntigravity LLMProviderType = "antigravity"
	// LLMProviderOpencode is the OpenCode CLI (`opencode`) as a process on the
	// runner host. Unlike cursor_agent and antigravity it has no dedicated
	// agentfs flavor: OpenCode discovers skills from the same .claude/skills
	// layout (confirmed compatible), and there is no confirmed way to make a
	// custom role the ACTIVE session persona from headless `run`, so the role
	// and rules are folded into the prompt instead (opencode.flattenHistory).
	LLMProviderOpencode LLMProviderType = "opencode"
)

// PinnedLocalEmbeddingModel is the model EMBEDDINGS_BASE_URL bootstraps a local
// embedder with — a pin because a vector from another model is not comparable
// to the ones already indexed. In config there is nothing to read: nothing
// about it is configurable.
const PinnedLocalEmbeddingModel = "nomic-embed-text-v1.5"

// PinnedLocalEmbeddingDimensions is nomic-embed-text-v1.5's vector length, so a
// caller can tell a mismatched index apart from a merely-empty one without a
// round trip to LM Studio — see EmbeddingProvenanceStale.
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
	// activate / embed" path keys off: those all mean "reach this base URL with
	// this key", and there is no base URL and no key — the CLI carries its own
	// subscription auth. An agent may still select the provider; what it may
	// not be is the default CHAT provider, because a chat turn is an HTTP call
	// and this one cannot serve it.
	HostExecuted bool `json:"host_executed"`
	// ModelOptions is the curated model picker for a HOST-EXECUTED provider —
	// the list /v1/models serves for it, because such a provider has no
	// endpoint to ask (see ModelsForHostExecutedProvider). Empty for HTTP
	// providers and for host-executed ones whose CLI adapter discovers models
	// live instead (application/agentcli.DefaultModels).
	ModelOptions []LLMModelOption `json:"model_options,omitempty"`
	// Available reports that this provider can actually run work today; false
	// means it is DECLARED but not yet built. The flag exists because "not
	// built yet" and "not listed" are different products: leaving a planned CLI
	// out entirely means the UI cannot say it is coming, while listing it
	// without a flag shows a provider whose selection queues runs no executor
	// will ever pick up — a failure surfaced task by task, far from the choice
	// that caused it. So the provider is listed AND refused: every connect /
	// activate / select path checks this field, on the SERVER, and a "coming
	// soon" badge is a courtesy on top, never a substitute. It is set
	// explicitly on every definition rather than defaulted, so a provider added
	// without a considered value fails closed (unavailable).
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
			// Same tier as the previous default, newer, cheaper, adaptive
			// thinking by default.
			DefaultModel: "claude-sonnet-5",
			// What a subtask the planner rated "hard" escalates to — declared
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
			// No base URL, no key, and no model required: an empty model lets
			// the CLI pick the account default, which is what a subscription
			// user expects.
			DefaultBaseURL:  "",
			DefaultModel:    "",
			RequiresAPIKey:  false,
			BaseURLRequired: false,
			ModelRequired:   false,
			// A CLI session is a whole task, not one HTTP round-trip; the number
			// exists so a caller does not read a zero as "no timeout". The
			// executor's own budget is what actually bounds a run
			// (claude_code.run_timeout).
			DefaultTimeoutSeconds: 3600,
			HostExecuted:          true,
			// The curated picker lives here as data, not in a switch anywhere —
			// see ModelsForHostExecutedProvider for who reads it.
			ModelOptions: ClaudeCodeModels(),
			Available:    true,
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
// an agent, connected, activated. Every guard asks it first, because a provider
// with no executor cannot be fixed by configuration. An unknown provider type
// is not available: the same answer ValidLLMProviderType gives it, and it keeps
// a typo from being treated as ready.
func ProviderAvailable(t LLMProviderType) bool {
	def, ok := LLMProviderDefinitionFor(t)
	return ok && def.Available
}

// ErrProviderUnavailable marks every refusal caused by naming a provider that
// is declared but not yet runnable. It is a sentinel so callers inside retry
// loops and behind transports can tell a PERMANENT refusal from a transient
// failure: nothing here becomes true on the second attempt, so the answer is
// 400 and no retry, not 500 and a backoff.
var ErrProviderUnavailable = errors.New("provider is declared but not yet available")

// LLMProviderLabel is the human name of a provider type: the catalog's own
// label, or the raw type string for one the catalog does not recognise. Empty
// only for an empty type.
func LLMProviderLabel(t LLMProviderType) string {
	if def, ok := LLMProviderDefinitionFor(t); ok && def.Label != "" {
		return def.Label
	}
	return string(t)
}

// ErrUnavailableProvider is what a caller gets for naming one. The sentence
// says the missing half is the executor, not anything the user can configure —
// the alternative message ("no client for provider cursor_agent") sends people
// to look for a key they were never going to find.
func ErrUnavailableProvider(t LLMProviderType) error {
	label := LLMProviderLabel(t)
	return fmt.Errorf("%s is not available yet: it is listed so you can see it is coming, but this server has no executor "+
		"for it, so anything selected on it would never run. Pick a provider that is available: %w", label, ErrProviderUnavailable)
}

// RequiresHostExecutor reports whether runs on this provider are executed by a
// local process rather than by the HTTP agent loop; the board runner asks this
// before it picks an executor, because the answer decides whether a missing
// executor is a clear "that binary is not on this host" failure or simply not
// this provider's concern.
func RequiresHostExecutor(t LLMProviderType) bool {
	def, ok := LLMProviderDefinitionFor(t)
	return ok && def.HostExecuted
}

// ErrHostExecutedProvider is what every HTTP path must return when handed a
// provider that is a local process. The alternative was demonstrated in
// production: a claude_code agent's CHAT turn went to the OpenAI-compatible
// adapter, which built the empty string as its endpoint and reported
// `Post "/chat/completions": unsupported protocol scheme ""` three times for a
// configuration that was never wrong. So the check is on the PROVIDER and fails
// fast, before any client is resolved and before the retry loop can multiply
// it, and it is raised from more than one place — the agent loop's entry and
// the multi-provider client's dispatch — because "a claude_code request must
// never become an HTTP request" is an invariant guarded in exactly one spot is
// one refactor away from not being guarded.
func ErrHostExecutedProvider(t LLMProviderType) error {
	label := LLMProviderLabel(t)
	return fmt.Errorf("this agent runs on %s, which is only available on a local runner host: "+
		"its CLI is not installed where this server runs, so there is no engine here to answer with. "+
		"Run this agent on a local runner, or move it to an API-backed provider: %w", label, ErrHostExecutedUnservable)
}

// ErrHostExecutedUnservable marks every refusal caused by a request naming a
// host-executed provider. It exists so callers can tell a PERMANENT refusal
// from a transient failure, and there are exactly two things they do with it:
// do not retry (the call sits in a retry loop; a local binary this host does
// not have will not appear on the third attempt) and do not degrade quietly
// (an optional step may skip but must say which step and why; a load-bearing
// one must fail). It replaced a silent fallback that rerouted these calls to
// the default HTTP provider, turning a specific, fixable error into whatever
// the unrelated default happened to be failing with.
var ErrHostExecutedUnservable = errors.New("host-executed provider cannot serve this call")

// EmbeddingProvenanceStale reports whether an index built with (indexModel,
// indexDimensions) can still be trusted against an install now configured for
// (configuredModel, configuredDimensions). This is the guard a dimension change
// needs and pgvector/JSONB storage does not provide: comparing rows a different
// model wrote does not error — it returns a confident, meaningless ranking. Both
// name and dimension count are checked independently (a provider could reuse a
// model name across sizes). A blank indexModel is stale whenever a model IS
// configured (the index predates the embedding columns, or "unknown" must
// never read as "matches"); a blank configuredModel means there is nothing to
// compare against yet, which is not this function's failure.
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
