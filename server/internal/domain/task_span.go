package domain

import (
	"time"

	"github.com/google/uuid"
)

// TaskColumnSpan is one uninterrupted stay of a task in one board column.
// A task that bounces back through need_revision produces a second span for
// the same column with a higher VisitNo rather than overwriting the first, so
// the history of a rework loop stays readable.
type TaskColumnSpan struct {
	ID              uuid.UUID  `json:"id"`
	TaskID          uuid.UUID  `json:"task_id"`
	RepositoryID    uuid.UUID  `json:"repository_id"`
	BoardColumn     string     `json:"board_column"`
	AgentID         *uuid.UUID `json:"agent_id,omitempty"`
	EnteredAt       time.Time  `json:"entered_at"`
	LeftAt          *time.Time `json:"left_at,omitempty"`
	DurationSeconds *int       `json:"duration_seconds,omitempty"`
	VisitNo         int        `json:"visit_no"`
	ReviewVerdict   string     `json:"review_verdict,omitempty"`
}

const (
	ReviewVerdictApprove = "approve"
	ReviewVerdictReject  = "reject"
)

// SpanCountsForSpeed reports whether time spent in a column may be charged to
// an agent's speed KPI.
//
// Excluded: blocked (waiting on a human answer), human_uat and analiz_review
// (human approval gates with no agent subscriber), backlog/todo (nobody's
// work), need_revision (a queue, not work), and the terminal columns.
func SpanCountsForSpeed(col TaskColumn) bool {
	switch col {
	case TaskColumnInProgress, TaskColumnCodeReview, TaskColumnReadyForQA,
		TaskColumnInQA, TaskColumnPMUAT:
		return true
	default:
		return false
	}
}
