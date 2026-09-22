package domain

import (
	"time"

	"github.com/google/uuid"
)

// TechStack is one technology an agent's skills can be filed under; a skill
// with none is general, with one only applies inside it. At most one, never
// several: a Django skill is not also a Flutter skill.
type TechStack struct {
	ID          uuid.UUID `json:"id"`
	AgentID     uuid.UUID `json:"agent_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateTechStackRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}

// UpdateTechStackRequest is a patch: reordering sends nothing but positions, so
// a full replace would make the caller resend a description it never touched. A
// nil field is left as it is.
type UpdateTechStackRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Position    *int    `json:"position"`
}
