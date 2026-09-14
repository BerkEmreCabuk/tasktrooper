package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

type Job struct {
	ID          uuid.UUID       `json:"id"`
	Status      JobStatus       `json:"status"`
	Request     json.RawMessage `json:"request"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CallbackURL string          `json:"callback_url,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

type JobRequest struct {
	Model       string     `json:"model,omitempty"`
	Messages    []Message  `json:"messages"`
	ToolPolicy  ToolPolicy `json:"tool_policy,omitempty"`
	CallbackURL string     `json:"callback_url,omitempty"`
	FileIDs     []string   `json:"file_ids,omitempty"`
}

type JobResult struct {
	Message Message `json:"message"`
	Usage   Usage   `json:"usage"`
}
