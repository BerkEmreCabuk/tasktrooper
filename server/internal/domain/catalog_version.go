package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	CatalogVersionKindSkill = "skill"
	CatalogVersionKindRule  = "rule"

	CatalogVersionActionCreate  = "create"
	CatalogVersionActionUpdate  = "update"
	CatalogVersionActionDelete  = "delete"
	CatalogVersionActionRestore = "restore"

	CatalogVersionSourceUser      = "user"
	CatalogVersionSourceEvolution = "evolution"
	CatalogVersionSourceSeed      = "seed"
	CatalogVersionSourceUpstream  = "upstream"
	CatalogVersionSourceMerge     = "merge"
)

// CatalogVersion is one immutable snapshot of a skill or a rule, appended on
// every write. Skill-only fields (description, category, tags) and rule-only
// fields (priority) share the row: the two kinds differ by four columns, and
// one history table keeps the restore path identical for both.
type CatalogVersion struct {
	ID           uuid.UUID  `json:"id"`
	AgentID      uuid.UUID  `json:"agent_id"`
	TargetKind   string     `json:"target_kind"`
	TargetID     uuid.UUID  `json:"target_id"`
	Version      int        `json:"version"`
	Action       string     `json:"action"`
	Name         string     `json:"name"`
	Description  string     `json:"description,omitempty"`
	Category     string     `json:"category,omitempty"`
	Tags         []string   `json:"tags,omitempty"`
	Content      string     `json:"content"`
	Priority     int        `json:"priority,omitempty"`
	Enabled      bool       `json:"enabled"`
	Source       string     `json:"source"`
	Reason       string     `json:"reason,omitempty"`
	ReflectionID *uuid.UUID `json:"reflection_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}
