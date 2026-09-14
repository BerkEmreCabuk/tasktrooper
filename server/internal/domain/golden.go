package domain

import (
	"time"

	"github.com/google/uuid"
)

// GoldenTask is a fixed eval prompt replayed after self-evolution changes to
// measure whether the agent's new skill/rule set still performs.
type GoldenTask struct {
	ID        uuid.UUID `json:"id"`
	AgentID   uuid.UUID `json:"agent_id"`
	Name      string    `json:"name"`
	Prompt    string    `json:"prompt"`
	Expected  []string  `json:"expected"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GoldenResult struct {
	ID           uuid.UUID  `json:"id"`
	GoldenID     uuid.UUID  `json:"golden_id"`
	AgentID      uuid.UUID  `json:"agent_id"`
	ReflectionID *uuid.UUID `json:"reflection_id,omitempty"`
	Passed       bool       `json:"passed"`
	Detail       string     `json:"detail"`
	CreatedAt    time.Time  `json:"created_at"`
}

type CreateGoldenTaskRequest struct {
	Name     string   `json:"name"`
	Prompt   string   `json:"prompt"`
	Expected []string `json:"expected"`
	Enabled  bool     `json:"enabled"`
}
