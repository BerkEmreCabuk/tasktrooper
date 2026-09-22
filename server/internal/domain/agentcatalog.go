package domain

import (
	"time"

	"github.com/google/uuid"
)

// UpstreamAgent is one agent definition as the external catalog stores it, in
// agents/<slug>/.
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
	// Seeded into agent_column_instructions, never into subscriptions, so a
	// column that dispatches the agent without being watched still gets its
	// instruction.
	ColumnInstructions []UpstreamColumnInstruction
	TechStacks         []CreateTechStackRequest
	KPIs               []CreateKPIRequest
	Etag               string
}

type UpstreamSkill struct {
	Name        string
	Description string
	Category    string
	TechStack   string
	Content     string
	// Mirrors the skill's `enabled:` front-matter; the sync must carry a
	// deferred capability's disabled state onto the live skill.
	Enabled bool
	// The revision marker stored back on the live skill as CatalogSha.
	Sha string
}

type UpstreamRule struct {
	Name     string
	Content  string
	Priority int
	Enabled  bool
}

type UpstreamColumnInstruction struct {
	Column      TaskColumn
	Instruction string
}

// CatalogSyncState is the one-row status of the last external-catalog sync.
type CatalogSyncState struct {
	// The git commit sha, or the local directory used in place.
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
	// Upstream changes parked for the user instead of applied.
	Pending int `json:"pending"`
}

const (
	CatalogPendingKindAgent    = "agent"
	CatalogPendingKindSkill    = "skill"
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
