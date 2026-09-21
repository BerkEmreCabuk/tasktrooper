package domain

import (
	"time"

	"github.com/google/uuid"
)

// UpstreamAgent is one agent definition as the external catalog stores it in
// agents/<slug>/. The sync reconciles these against live agents by CatalogSlug.
type UpstreamAgent struct {
	Slug          string
	Name          string
	Description   string
	SubagentType  string
	SystemPrompt  string
	ProviderType  LLMProviderType
	Model         string
	ModelHeavy    string
	Effort        string
	MaxTurns      int
	SelfEvolution bool
	Enabled       bool
	ToolPolicy    ToolPolicy
	Roles         []TemplateRoleSuggestion
	Subscriptions []TaskColumn
	Skills        []UpstreamSkill
	Rules         []UpstreamRule
	// ColumnInstructions are the agent's per-column defaults from
	// agents/<slug>/columns/<column_slug>.md: what the agent is told to do when
	// a task arrives in that column, delivered as a prompt append at dispatch.
	// They are seeded into agent_column_instructions, never into
	// subscriptions, so a column that dispatches the agent without being
	// watched still gets its instruction.
	ColumnInstructions []UpstreamColumnInstruction
	// TechStacks are the stacks the agent's skills are filed under, created on
	// the agent at ingest and resolved by name when a skill's front-matter
	// tech_stack names one.
	TechStacks []CreateTechStackRequest
	// KPIs are the agent's default scoreboard, created at ingest the same way
	// the built-in templates used to carry them.
	KPIs []CreateKPIRequest
	// Etag is a stable hash of this agent's whole definition, so a sync can
	// tell an unchanged agent apart from a changed one at a glance.
	Etag string
}

type UpstreamSkill struct {
	Name        string
	Description string
	Category    string
	TechStack   string
	Content     string
	// Enabled mirrors the skill's `enabled:` front-matter; a deferred
	// capability ships disabled so the row exists while the prompt builder
	// skips it, and the sync must carry that flag onto the live skill.
	Enabled bool
	// Sha hashes just this skill's content, the revision marker stored back on
	// the live skill as CatalogSha.
	Sha string
}

type UpstreamRule struct {
	Name     string
	Content  string
	Priority int
	Enabled  bool
}

// UpstreamColumnInstruction is one per-column default prompt from the
// catalog's agents/<slug>/columns/ directory.
type UpstreamColumnInstruction struct {
	Column      TaskColumn
	Instruction string
}

// CatalogSyncState is the one-row status of the last external-catalog sync.
type CatalogSyncState struct {
	// RepoRef names where the last sync read from: the git commit sha, or the
	// local directory used in place.
	RepoRef      string             `json:"repo_ref"`
	LastSyncAt   time.Time          `json:"last_sync_at"`
	LastError    string             `json:"last_error,omitempty"`
	LastSummary  *CatalogSyncResult `json:"last_summary,omitempty"`
	PendingCount int                `json:"pending_count"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type CatalogSyncResult struct {
	RepoRef string `json:"repo_ref"`
	Created int    `json:"created"`
	Updated int    `json:"updated"`
	Merged  int    `json:"merged"`
	Skipped int    `json:"skipped"`
	// Pending is how many upstream changes could not be applied and were
	// parked for the user (toggle off, budget full, LLM merge failed).
	Pending int `json:"pending"`
}

const (
	CatalogPendingKindAgent  = "agent"
	CatalogPendingKindSkill  = "skill"
	CatalogPendingActionCreate = "create"
	CatalogPendingActionUpdate = "update"
	CatalogPendingActionDelete = "delete"
	CatalogPendingActionMerge  = "merge"
)

type CatalogPending struct {
	ID        uuid.UUID `json:"id"`
	AgentSlug string    `json:"agent_slug"`
	AgentName string    `json:"agent_name"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}