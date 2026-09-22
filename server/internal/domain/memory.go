package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	MemorySourceAgent      = "agent"
	MemorySourceReflection = "reflection"
	MemorySourceUser       = "user"
)

// Memory scopes are derived from two nullable columns rather than stored:
// agent_id NULL means the memory belongs to the whole team, repository_id NULL
// means it holds everywhere instead of only inside one repository.
const (
	MemoryScopeAgentGlobal  = "agent_global"
	MemoryScopeAgentProject = "agent_project"
	MemoryScopeTeamGlobal   = "team_global"
	MemoryScopeTeamProject  = "team_project"
)

// MemoryOwner selects which owner buckets a query returns.
type MemoryOwner string

const (
	// MemoryOwnerAll returns the agent's own memories plus the team's.
	MemoryOwnerAll MemoryOwner = ""
	// MemoryOwnerAgent returns only the agent's own memories.
	MemoryOwnerAgent MemoryOwner = "agent"
	// MemoryOwnerTeam returns only team memories (agent_id IS NULL).
	MemoryOwnerTeam MemoryOwner = "team"
)

// MemoryRepoScope selects which repository buckets a query returns.
type MemoryRepoScope string

const (
	// MemoryRepoScopeVisible (default) is what an agent sees inside a
	// repository: global memories plus that repository's own; with no repo in
	// play it degrades to global-only, so lessons from an unrelated repo never
	// leak into a run.
	MemoryRepoScopeVisible MemoryRepoScope = ""
	// MemoryRepoScopeProject returns only memories bound to the repository.
	MemoryRepoScopeProject MemoryRepoScope = "project"
	// MemoryRepoScopeGlobal returns only repository-independent memories.
	MemoryRepoScopeGlobal MemoryRepoScope = "global"
	// MemoryRepoScopeAny ignores the repository dimension; for management views
	// that list everything an agent knows.
	MemoryRepoScopeAny MemoryRepoScope = "any"
)

// MemoryQuery describes one read across the memory buckets.
type MemoryQuery struct {
	AgentID      uuid.UUID
	Owner        MemoryOwner
	RepositoryID *uuid.UUID
	Repo         MemoryRepoScope
	Limit        int
}

type AgentMemory struct {
	ID           uuid.UUID  `json:"id"`
	AgentID      uuid.UUID  `json:"agent_id"`
	RepositoryID *uuid.UUID `json:"repository_id,omitempty"`
	Content      string     `json:"content"`
	Category     string     `json:"category"`
	Embedding    []float32  `json:"embedding,omitempty"`
	Source       string     `json:"source"`
	Scope        string     `json:"scope"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// MemoryScopeOf names the bucket a memory belongs to.
func MemoryScopeOf(agentID uuid.UUID, repositoryID *uuid.UUID) string {
	switch {
	case agentID != uuid.Nil && repositoryID != nil:
		return MemoryScopeAgentProject
	case agentID != uuid.Nil:
		return MemoryScopeAgentGlobal
	case repositoryID != nil:
		return MemoryScopeTeamProject
	default:
		return MemoryScopeTeamGlobal
	}
}

// IsTeam reports whether every agent can read (and write) this memory.
func (m AgentMemory) IsTeam() bool { return m.AgentID == uuid.Nil }

// IsProjectScoped reports whether the memory only applies inside one repository.
func (m AgentMemory) IsProjectScoped() bool { return m.RepositoryID != nil }
