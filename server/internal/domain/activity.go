package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrRunCancelled is a chat turn a human stopped from the UI.
//
// It travels back up through SendMessage/SendMessageStream so the transport can
// end cleanly instead of reporting a failure: nothing went wrong, someone
// changed their mind. The run's row and whatever the agent had already said are
// persisted by the service before this is returned, so a caller that sees it has
// nothing left to write.
//
// The status written on the row is TaskAgentRunStatusCancelled — one cancelled
// vocabulary for board runs and chat turns alike.
var ErrRunCancelled = errors.New("agent run cancelled by user")

// The statuses a session_runs row can hold. They were string literals
// scattered across the store and the service until a cross-replica stop needed
// to COMPARE one — a read of a status is a different thing from a write of it,
// and a typo in a comparison is silent where a typo in a write is not.
const (
	SessionRunStatusRunning   = "running"
	SessionRunStatusCompleted = "completed"
	SessionRunStatusFailed    = "failed"
	SessionRunStatusCancelled = "cancelled"
)

type SessionRun struct {
	ID          uuid.UUID  `json:"id"`
	SessionID   *uuid.UUID `json:"session_id,omitempty"`
	RequestID   string     `json:"request_id"`
	Status      string     `json:"status"`
	Model       string     `json:"model,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type SessionStep struct {
	ID        uuid.UUID       `json:"id"`
	RunID     uuid.UUID       `json:"run_id"`
	StepType  string          `json:"step_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type APIKeyRecord struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	ToolPolicy ToolPolicy `json:"tool_policy"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreateAPIKeyRequest struct {
	Name       string     `json:"name"`
	ToolPolicy ToolPolicy `json:"tool_policy"`
}

type CreateAPIKeyResponse struct {
	APIKeyRecord
	Key string `json:"key"`
}
