package domain

import "time"

type Config struct {
	LLM           LLMConfig           `koanf:"llm"`
	Server        ServerConfig        `koanf:"server"`
	Tools         ToolsConfig         `koanf:"tools"`
	Storage       StorageConfig       `koanf:"storage"`
	Jobs          JobsConfig          `koanf:"jobs"`
	RAG           RAGConfig           `koanf:"rag"`
	Orchestration OrchestrationConfig `koanf:"orchestration"`
	Board         BoardConfig         `koanf:"board"`
	Context       ContextConfig       `koanf:"context"`
	Mapping       MappingConfig       `koanf:"mapping"`
	Indexer       IndexerConfig       `koanf:"indexer"`
	Embedding     EmbeddingConfig     `koanf:"embedding"`
	Graph         GraphConfig         `koanf:"graph"`
	Evolution     EvolutionConfig     `koanf:"evolution"`
	ProdOps       ProdOpsConfig       `koanf:"prod_ops"`
	Storeops      StoreopsConfig      `koanf:"storeops"`
	DeployOps     DeployOpsConfig     `koanf:"deploy_ops"`
	ClaudeCode    ClaudeCodeConfig    `koanf:"claude_code"`
	Antigravity   AntigravityConfig   `koanf:"antigravity"`
	CursorAgent   CursorAgentConfig   `koanf:"cursor_agent"`
	Opencode      OpencodeConfig      `koanf:"opencode"`
}

// CursorAgentConfig drives the local Cursor CLI executor: the path a board run
// on the cursor_agent provider is handed to instead of the in-process agent
// loop (internal/adapter/agentcli/cursor).
//
// There is no Enabled flag, for the same reason AntigravityConfig has none:
// the switch is whether the binary exists on this host.
type CursorAgentConfig struct {
	// Binary is the CLI to run, resolved on PATH. Empty means "cursor-agent".
	// Normally set from CURSOR_AGENT_BIN.
	Binary string `koanf:"binary"`
	// MaxConcurrent caps simultaneous CLI sessions. 0 means the executor's
	// default (3).
	MaxConcurrent int `koanf:"max_concurrent"`
	// RunTimeout bounds ONE session end to end. 0 means the executor's default
	// (1h).
	RunTimeout time.Duration `koanf:"run_timeout"`
}

// OpencodeConfig drives the local OpenCode CLI executor: the path a board run
// on the opencode provider is handed to instead of the in-process agent loop
// (internal/adapter/agentcli/opencode).
//
// There is no Enabled flag, for the same reason AntigravityConfig has none:
// the switch is whether the binary exists on this host.
type OpencodeConfig struct {
	// Binary is the CLI to run, resolved on PATH. Empty means "opencode".
	// Normally set from OPENCODE_BIN.
	Binary string `koanf:"binary"`
	// MaxConcurrent caps simultaneous CLI sessions. 0 means the executor's
	// default (3).
	MaxConcurrent int `koanf:"max_concurrent"`
	// RunTimeout bounds ONE session end to end. 0 means the executor's default
	// (1h).
	RunTimeout time.Duration `koanf:"run_timeout"`
}

// AntigravityConfig drives the local Antigravity (AGY) CLI executor: the path a
// board run on the antigravity provider is handed to instead of the in-process
// agent loop (internal/adapter/agentcli/antigravity).
//
// There is no Enabled flag, for the same reason ClaudeCodeConfig has none: the
// switch is whether the binary exists on this host. An installation without the
// CLI registers no executor at all, and an antigravity agent's run then fails
// with one clear sentence naming the missing binary.
type AntigravityConfig struct {
	// Binary is the CLI to run, resolved on PATH. Empty means "agy".
	// Normally set from ANTIGRAVITY_BIN.
	Binary string `koanf:"binary"`
	// MaxTurns bounds one CLI session. 0 means the executor's default (100).
	MaxTurns int `koanf:"max_turns"`
	// MaxConcurrent caps simultaneous CLI sessions. 0 means the executor's
	// default (3).
	MaxConcurrent int `koanf:"max_concurrent"`
	// RunTimeout bounds ONE session end to end. 0 means the executor's default
	// (1h).
	RunTimeout time.Duration `koanf:"run_timeout"`
}

// ClaudeCodeConfig drives the local Claude Code CLI executor: the path a board
// run on the claude_code provider is handed to instead of the in-process agent
// loop (internal/adapter/agentcli/claudecode).
//
// There is no Enabled flag, for the same reason MobileConfig has none: the
// switch is whether the binary exists on this host. An installation without the
// CLI registers no executor at all, and a claude_code agent's run then fails
// with one clear sentence naming the missing binary — which is more useful than
// a flag an operator can set to true on a machine where it cannot work.
type ClaudeCodeConfig struct {
	// Binary is the CLI to run, resolved on PATH. Empty means "claude".
	// Normally set from CLAUDE_CODE_BIN, which is NOT a secret and therefore
	// not touched by the process-secret scrub (platform/runtime/envscrub.go):
	// it names a program, and the child process needs PATH to find it anyway.
	Binary string `koanf:"binary"`
	// MaxTurns bounds one CLI session, the way llm.task_max_iterations bounds a
	// loop run. 0 means the executor's default (100).
	MaxTurns int `koanf:"max_turns"`
	// MaxConcurrent caps simultaneous CLI sessions. It is a separate number
	// from board.max_concurrent_runs because it limits a different thing: the
	// board cap sizes the worker pool for the whole tenant, this one is what
	// one Claude subscription will serve at a time. A run that cannot get a
	// slot waits for one (bounded — see the executor's DefaultSlotWait); it is
	// not parked, because waiting on the run in front is a queue, not a quota.
	// 0 means the executor's default (3).
	MaxConcurrent int `koanf:"max_concurrent"`
	// RunTimeout bounds ONE session end to end. It is the only thing that ever
	// gives up on a wedged CLI: a subprocess has no equivalent of a provider's
	// HTTP timeout, and the run's heartbeat keeps the row fresh, so the
	// stale-run reconciler never sees it either. 0 means the executor's default
	// (1h).
	RunTimeout time.Duration `koanf:"run_timeout"`
	// SettingSources says which of the CLI's settings files a session loads: a
	// comma-separated subset of user, project, local. Empty means the executor's
	// default, "project,local" — the OPERATOR's own ~/.claude settings are left
	// out, because a local runner is somebody's working machine and their hooks,
	// plugins and permission rules would otherwise land inside every board run.
	// Set "user,project,local" to restore the CLI's own default; the one install
	// that needs to is the one whose Claude auth lives in a user-level
	// apiKeyHelper (a normal subscription login is unaffected).
	SettingSources string `koanf:"setting_sources"`
}

// ProdOpsConfig drives production monitoring: the health probe that watches
// every deploy target's health URL and turns a dead environment into an
// incident without waiting for an external alerting stack.
type ProdOpsConfig struct {
	MonitorEnabled bool          `koanf:"monitor_enabled"`
	ProbeInterval  time.Duration `koanf:"probe_interval"`
}

// StoreopsConfig drives the mobile store monitor: the sweep that re-verifies
// onboarding checklists, polls store review status, and renews signing
// assets ahead of expiry.
type StoreopsConfig struct {
	PollInterval time.Duration `koanf:"poll_interval"`
}

// DeployOpsConfig drives the deploy monitor: the sweep that mirrors GitHub
// Actions deploy runs locally, attributes console-triggered runs, and turns
// a failed deploy into an incident. One API call per repository ×
// environment per sweep, which is why the default interval is slower than
// the health probe's.
type DeployOpsConfig struct {
	MonitorEnabled bool          `koanf:"monitor_enabled"`
	PollInterval   time.Duration `koanf:"poll_interval"`
	// HealthWindow is how long after a successful deploy an incident on that
	// environment is attributed to the task that just released — the
	// post-release watch the QA agent is told to keep, and the window inside
	// which auto_rollback fires. 0 means the package default (15m).
	//
	// Short on purpose. It is keyed on a specific commit and it authorises a
	// ROLLBACK of a specific card, so it has to be short enough that a
	// coincidence does not get somebody's release reverted. The generic
	// 45-minute correlation in prodops/remedy.go is unrelated and unchanged —
	// that one only ever writes advisory words.
	HealthWindow time.Duration `koanf:"health_window"`
}

// EvolutionConfig drives agent self-evolution: periodic reflections, KPI
// evaluation, and evolution-impact tracking.
type EvolutionConfig struct {
	Enabled            bool          `koanf:"enabled"`
	AllowWebResearch   bool          `koanf:"allow_web_research"`
	Model              string        `koanf:"model"`
	ProviderType       string        `koanf:"provider_type"`
	TickInterval       time.Duration `koanf:"tick_interval"`
	ReflectInterval    time.Duration `koanf:"reflect_interval"`
	RevisionDebounce   time.Duration `koanf:"revision_debounce"`
	ImpactWindow       time.Duration `koanf:"impact_window"`
	MinEventsForImpact int           `koanf:"min_events_for_impact"`
	MaxSkillChanges    int           `koanf:"max_skill_changes"`
	MaxRuleChanges     int           `koanf:"max_rule_changes"`
	// MaxSkillsPerAgent / MaxRulesPerAgent are the standing budget, not the
	// per-reflection one: once an agent is at budget, a reflection can only
	// update, merge or delete — a create is rejected. Left unbounded, a weekly
	// reflection grows the catalog forever and the agent's own instructions
	// start contradicting each other.
	MaxSkillsPerAgent int `koanf:"max_skills_per_agent"`
	MaxRulesPerAgent  int `koanf:"max_rules_per_agent"`
	// GoldenGate runs the golden suite before AND after the changes and
	// reverts them when the after-run is worse. Costs one extra suite run per
	// reflection that actually changed something.
	GoldenGate        bool   `koanf:"golden_gate"`
	JudgeModel        string `koanf:"judge_model"`
	JudgeProviderType string `koanf:"judge_provider_type"`
	MaxMemoryChanges  int    `koanf:"max_memory_changes"`
	MemoryMaxCount    int    `koanf:"memory_max_count"`
	EvidenceMaxChars  int    `koanf:"evidence_max_chars"`
}

type BoardConfig struct {
	DispatchEnabled         bool              `koanf:"dispatch_enabled"`
	MaxConcurrentRuns       int               `koanf:"max_concurrent_runs"`
	VerificationEnabled     bool              `koanf:"verification_enabled"`
	VerifyMaxFixAttempts    int               `koanf:"verify_max_fix_attempts"`
	RequireCriteriaComplete bool              `koanf:"require_criteria_complete"`
	TaskTypeModels          map[string]string `koanf:"task_type_models"`
	// ReconcileStaleAfter/ReconcileInterval drive the reconciler that recovers
	// task_agent_runs orphaned by a process restart or a dropped in-memory
	// job (dispatch is otherwise purely event-driven, so nothing else ever
	// revisits an idle task). Kept generous by default: a run has no
	// per-iteration timeout, so a legitimately slow multi-turn agent run must
	// not be mistaken for a lost one.
	ReconcileStaleAfter time.Duration `koanf:"reconcile_stale_after"`
	ReconcileInterval   time.Duration `koanf:"reconcile_interval"`
	// PipelineGateTimeout is the longest a task may wait in code_review for a
	// build/test result before its reviewer is dispatched anyway, and
	// PipelineGateInterval is how often unfinished pipelines are re-asked.
	//
	// Both exist because the gate's only key used to be an event nobody
	// guarantees: PipelineRunner.finalize, in the process that started the
	// pipeline. A pod restart, a dropped delivery or a CI account with no
	// minutes left left the card in code_review with a spinner and no agent,
	// forever. See board.PipelineGateWindow for why the default is what it is.
	PipelineGateTimeout  time.Duration `koanf:"pipeline_gate_timeout"`
	PipelineGateInterval time.Duration `koanf:"pipeline_gate_interval"`
}

type LLMConfig struct {
	BaseURL string `koanf:"base_url"`
	Model   string `koanf:"model"`
	APIKey  string `koanf:"api_key"`
	// MaxIterations bounds a chat turn: a question plus a few lookups.
	MaxIterations int `koanf:"max_iterations"`
	// TaskMaxIterations bounds a board task or orchestration subtask, which is
	// search + read + edit + verify and needs several times the turns of a
	// chat. Sharing MaxIterations killed real coding runs mid-edit.
	TaskMaxIterations int `koanf:"task_max_iterations"`
	// RunMaxTotalTokens is a mid-run circuit breaker: the agent loop sums every
	// response's PromptTokens+CompletionTokens across one run (chat or task)
	// and ends it — through the same wrap-up path an exhausted iteration
	// budget takes — once the total crosses this many tokens. It exists
	// alongside, not instead of, MaxIterations/TaskMaxIterations: a run can
	// blow through a token budget in far fewer turns than its iteration cap
	// allows (a huge context, a summarizer that failed to shrink it), and
	// billing.Service.Allow only gates BEFORE a run starts, never during one —
	// see its own comment for why. 0 disables it.
	RunMaxTotalTokens int           `koanf:"run_max_total_tokens"`
	Timeout           time.Duration `koanf:"timeout"`
}

type ServerConfig struct {
	Port    int            `koanf:"port"`
	APIKey  string         `koanf:"api_key"`
	APIKeys []APIKeyConfig `koanf:"api_keys"`
	// PublicBaseURL is the externally reachable origin of this instance
	// (e.g. "https://bridge.example.com"). GitHub push webhooks are pointed at
	// it; empty disables webhook setup.
	PublicBaseURL string `koanf:"public_base_url"`
}

type APIKeyConfig struct {
	Key        string     `koanf:"key"`
	Name       string     `koanf:"name"`
	ToolPolicy ToolPolicy `koanf:"tool_policy"`
}

type ToolsConfig struct {
	DefaultPolicy      ToolPolicy               `koanf:"default_policy"`
	Search             SearchConfig             `koanf:"search"`
	Terminal           TerminalConfig           `koanf:"terminal"`
	Web                WebConfig                `koanf:"web"`
	Browser            BrowserConfig            `koanf:"browser"`
	Mobile             MobileConfig             `koanf:"mobile"`
	BoilerplateCatalog BoilerplateCatalogConfig `koanf:"boilerplate_catalog"`
	MaxToolOutputChars int                      `koanf:"max_tool_output_chars"`
}

type ContextConfig struct {
	MaxTokens          int `koanf:"max_tokens"`
	ReserveOutput      int `koanf:"reserve_output"`
	SummarizeThreshold int `koanf:"summarize_threshold"`
	KeepRecentMessages int `koanf:"keep_recent_messages"`
}

type MappingConfig struct {
	Enabled              bool `koanf:"enabled"`
	TreeMaxDepth         int  `koanf:"tree_max_depth"`
	MaxFiles             int  `koanf:"max_files"`
	SkeletonMaxTokens    int  `koanf:"skeleton_max_tokens"`
	InjectOnSessionStart bool `koanf:"inject_on_session_start"`
}

type IndexerConfig struct {
	Enabled         bool     `koanf:"enabled"`
	TopK            int      `koanf:"top_k"`
	ReindexOnChange bool     `koanf:"reindex_on_change"`
	ChunkMaxLines   int      `koanf:"chunk_max_lines"`
	AllowedRoots    []string `koanf:"allowed_roots"`
	QueryRewrite    bool     `koanf:"query_rewrite"`
	// Concurrency is how many files are chunked, embedded and stored at the
	// same time. Indexing is one HTTP round trip per chunk and the process
	// spends nearly all of its wall clock waiting for the embedding endpoint,
	// so a single worker leaves both sides idle. Raising it multiplies the
	// request rate too — the embedding rate limiter is what keeps that from
	// turning into 429s. 0 falls back to defaultIndexConcurrency.
	Concurrency int `koanf:"concurrency"`
}

// EmbeddingConfig paces embedding requests. Indexing a repository fires one
// embedding call per chunk back to back, which hosted endpoints answer with
// 429 — and a single 429 used to fail the whole index at ~9%. RequestsPerMinute
// spaces the calls out, the retry settings absorb the 429s that still happen,
// and a Retry-After from the provider also delays every following call so one
// rejection does not turn into a burst of them.
type EmbeddingConfig struct {
	// RequestsPerMinute caps embedding calls; 0 leaves them unthrottled.
	RequestsPerMinute int `koanf:"requests_per_minute"`
	// MaxRetries is how many times a rate-limited call is retried. 0 means unset
	// (the loader's default applies); a negative value turns retrying off.
	MaxRetries int `koanf:"max_retries"`
	// RetryBackoff is the first wait after a 429; it doubles per attempt and is
	// used only when the provider sends no Retry-After.
	RetryBackoff time.Duration `koanf:"retry_backoff"`
	// MaxRetryWait caps a single wait, including a provider's Retry-After.
	MaxRetryWait time.Duration `koanf:"max_retry_wait"`
	// RequestTimeout budgets one embedding call. The indexer derives its
	// per-chunk deadline from this plus the retry waits.
	RequestTimeout time.Duration `koanf:"request_timeout"`
	// QueryCacheEntries bounds the in-memory LRU that caches query embedding
	// vectors keyed by (provider, model, exact text), so an identical query
	// (a rewritten codebase_search query, the planner's skill search, memory
	// recall) is served without a re-embed. Index-time chunk embedding shares
	// the same cache, but indexer/incremental.go's SHA-256 content dedup means
	// a chunk is only ever re-embedded when its content changed, so it does
	// not repeatedly thrash the LRU. 0 means unset (the loader's default
	// applies); a negative value disables the cache.
	QueryCacheEntries int `koanf:"query_cache_entries"`
}

type GraphConfig struct {
	Enabled           bool `koanf:"enabled"`
	MaxExpansionDepth int  `koanf:"max_expansion_depth"`
	MaxExpandedChunks int  `koanf:"max_expanded_chunks"`
}

type SearchConfig struct {
	Enabled    bool `koanf:"enabled"`
	MaxResults int  `koanf:"max_results"`
}

type TerminalConfig struct {
	Enabled    bool          `koanf:"enabled"`
	WorkingDir string        `koanf:"working_dir"`
	Timeout    time.Duration `koanf:"timeout"`
	// MaxTimeout caps what a single run_terminal call may ask for with
	// timeout_seconds. Timeout is the default for an unspecified call; this is
	// the ceiling for a slow one (dependency install, cold build, full suite).
	MaxTimeout time.Duration         `koanf:"max_timeout"`
	Sandbox    TerminalSandboxConfig `koanf:"sandbox"`
}

type TerminalSandboxConfig struct {
	Mode               string   `koanf:"mode"`
	AllowedCommands    []string `koanf:"allowed_commands"`
	BlockedPatterns    []string `koanf:"blocked_patterns"`
	RestrictWorkingDir bool     `koanf:"restrict_working_dir"`
}

type WebConfig struct {
	Enabled          bool  `koanf:"enabled"`
	MaxResponseBytes int64 `koanf:"max_response_bytes"`
}

type BoilerplateCatalogConfig struct {
	Enabled bool `koanf:"enabled"`
}

type BrowserConfig struct {
	Enabled bool `koanf:"enabled"`
}

// MobileConfig attaches a real Android device, reached through an Appium
// server, to the mobile_* tools.
//
// There is no Enabled flag on purpose. The browser is a binary in the image and
// can be assumed present; a phone is a physical object an operator plugged in,
// so "configured" is the only honest switch — an installation with no hub URL
// and no device has nothing to enable, and registering the tools anyway would
// give agents a tool set whose every call fails.
type MobileConfig struct {
	// HubURL is the Appium server base, e.g. http://appium.tasktrooper:4723.
	HubURL string `koanf:"hub_url"`
	// DeviceUDID pins which phone. Appium would otherwise take whatever adb
	// lists first, which on a host with an emulator attached is not the device
	// QA was told it is testing.
	DeviceUDID string `koanf:"device_udid"`
	// PlatformVersion is optional, and only makes the capability set explicit.
	PlatformVersion string `koanf:"platform_version"`
	// DevicePIN unlocks the lock screen at the start of every session. It is a
	// credential: it comes from the environment, is never logged, and is never
	// exposed to an agent as a tool argument.
	DevicePIN string `koanf:"device_pin"`
	// AuthToken is sent to the hub as a bearer token. The tunnel in front of a
	// home-hosted Appium is the real access control; this is the hop inside it.
	AuthToken string `koanf:"auth_token"`
	// BridgeURL is the adb sidecar next to Appium (cmd/device-agent). It is
	// what lets the settings UI pair and connect a phone; without it the
	// device still works, but attaching one is again a shell on the Appium
	// host. Cluster-side wiring, so it stays in config rather than in the
	// registration an operator edits.
	BridgeURL   string `koanf:"bridge_url"`
	BridgeToken string `koanf:"bridge_token"`
}

type MCPServerConfig struct {
	ID           string            `koanf:"id"`
	Enabled      bool              `koanf:"enabled"`
	Transport    string            `koanf:"transport"`
	Command      string            `koanf:"command"`
	Args         []string          `koanf:"args"`
	Env          map[string]string `koanf:"env"`
	URL          string            `koanf:"url"`
	Headers      map[string]string `koanf:"headers"`
	AllowedTools []string          `koanf:"allowed_tools"`
}

type StorageConfig struct {
	Postgres PostgresConfig `koanf:"postgres"`
	Sessions SessionsConfig `koanf:"sessions"`
}

type PostgresConfig struct {
	DSN      string `koanf:"dsn"`
	MaxConns int32  `koanf:"max_conns"`
}

type SessionsConfig struct {
	TTL           time.Duration `koanf:"ttl"`
	WorkspaceRoot string        `koanf:"workspace_root"`
}

type JobsConfig struct {
	MaxConcurrent int           `koanf:"max_concurrent"`
	Timeout       time.Duration `koanf:"timeout"`
}

type RAGConfig struct {
	Enabled      bool   `koanf:"enabled"`
	StorageDir   string `koanf:"storage_dir"`
	ChunkSize    int    `koanf:"chunk_size"`
	ChunkOverlap int    `koanf:"chunk_overlap"`
	TopK         int    `koanf:"top_k"`
}

func (c *Config) ExpandEnv() {
	c.LLM.BaseURL = EnvExpandString(c.LLM.BaseURL)
	c.LLM.Model = EnvExpandString(c.LLM.Model)
	c.LLM.APIKey = EnvExpandString(c.LLM.APIKey)
	c.Server.APIKey = EnvExpandString(c.Server.APIKey)
	c.Server.PublicBaseURL = EnvExpandString(c.Server.PublicBaseURL)
	for i := range c.Server.APIKeys {
		c.Server.APIKeys[i].Key = EnvExpandString(c.Server.APIKeys[i].Key)
		c.Server.APIKeys[i].Name = EnvExpandString(c.Server.APIKeys[i].Name)
	}
	c.Tools.Terminal.WorkingDir = EnvExpandString(c.Tools.Terminal.WorkingDir)
	c.Storage.Postgres.DSN = EnvExpandString(c.Storage.Postgres.DSN)
	c.Storage.Sessions.WorkspaceRoot = EnvExpandString(c.Storage.Sessions.WorkspaceRoot)
	c.RAG.StorageDir = EnvExpandString(c.RAG.StorageDir)
	for i := range c.Indexer.AllowedRoots {
		c.Indexer.AllowedRoots[i] = EnvExpandString(c.Indexer.AllowedRoots[i])
	}
}
