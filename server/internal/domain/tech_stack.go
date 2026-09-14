package domain

import (
	"time"

	"github.com/google/uuid"
)

// TechStack is one technology an agent's skills can be filed under. A skill
// with no stack is general — it holds whatever the code is written in — and a
// skill with one only applies inside that technology. At most one, never
// several: a skill written for Django is not also a Flutter skill.
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

// UpdateTechStackRequest is a patch rather than the full-replace shape the rest
// of this catalog uses: reordering a stack list sends nothing but positions,
// and a full replace would make the caller resend a description it never
// touched. A nil field is left as it is.
type UpdateTechStackRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Position    *int    `json:"position"`
}
