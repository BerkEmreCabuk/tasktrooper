package domain

import (
	"time"

	"github.com/google/uuid"
)

type AuditEntry struct {
	ID            uuid.UUID `json:"id"`
	RequestID     string    `json:"request_id"`
	APIKeyName    string    `json:"api_key_name,omitempty"`
	ToolName      string    `json:"tool_name"`
	Arguments     string    `json:"arguments,omitempty"`
	ResultPreview string    `json:"result_preview,omitempty"`
	DurationMs    int64     `json:"duration_ms"`
	IsError       bool      `json:"is_error"`
	CreatedAt     time.Time `json:"created_at"`
}
