package domain

import (
	"errors"

	"github.com/google/uuid"
)

// ErrColumnHasWorkflowStages is UpdateColumns' refusal to remove a column
// slug a workflow stage with behaviours still references. workflow_stages
// carries no FK to board_columns on purpose (ReplaceColumns deletes every
// board_columns row and reinserts on every save), so this is the only thing
// that stops a rename/delete from silently orphaning a configured stage.
var ErrColumnHasWorkflowStages = errors.New("column is referenced by a workflow stage with behaviours")

type BoardColumn struct {
	ID        uuid.UUID `json:"id"`
	Slug      string    `json:"slug"`
	Label     string    `json:"label"`
	Position  int       `json:"position"`
	IsBacklog bool      `json:"is_backlog"`
}

type BoardMember struct {
	AgentID uuid.UUID `json:"agent_id"`
}

type BoardSubscription struct {
	AgentID    uuid.UUID `json:"agent_id"`
	ColumnSlug string    `json:"column_slug"`
}

// AgentColumnSubscription is one column an agent subscribes to, with its
// optional per-column task-type filter — the shape
// GET/PUT /v1/agents/:agentId/subscriptions reads and writes. TaskTypes nil
// means "every type" (today's behaviour); a non-nil slice narrows dispatch on
// that column to only those types, mirroring board_columns' own
// agent_column_subscriptions.task_type_filter column.
type AgentColumnSubscription struct {
	ColumnSlug string
	TaskTypes  []string
}

// AgentColumnInstruction is text an agent is handed when a run dispatches it
// for a task that arrived in column_slug — "what to do when a task lands
// here", a per-agent column default the operator can override. It is a
// separate store from AgentColumnSubscription: the column that dispatches an
// agent need not be one it watches (a developer dispatched into need_revision
// by assignment still wants its instruction), and instructions must never
// change column-watch dispatch behaviour.
type AgentColumnInstruction struct {
	AgentID     uuid.UUID `json:"agent_id"`
	ColumnSlug  string    `json:"column_slug"`
	Instruction string    `json:"instruction"`
}

type BoardTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type SetBoardTransitionsRequest struct {
	Transitions []BoardTransition `json:"transitions"`
}

type BoardColumnInput struct {
	Slug      string `json:"slug"`
	Label     string `json:"label"`
	Position  int    `json:"position"`
	IsBacklog bool   `json:"is_backlog"`
}

type BoardSubscriptionInput struct {
	AgentID        uuid.UUID `json:"agent_id"`
	ColumnSlugs    []string  `json:"column_slugs"`
	TaskTypeFilter []string  `json:"task_type_filter,omitempty"`
}

type UpdateBoardColumnsRequest struct {
	Columns []BoardColumnInput `json:"columns"`
}

type SetBoardMembersRequest struct {
	AgentIDs []uuid.UUID `json:"agent_ids"`
}

type SetBoardSubscriptionsRequest struct {
	Subscriptions []BoardSubscriptionInput `json:"subscriptions"`
}

type UpdateBoardSettingsRequest struct {
	KeyPrefix string `json:"key_prefix"`
}

type BoardSettings struct {
	KeyPrefix string `json:"key_prefix"`
}

type WorkspaceConfig struct {
	Settings      BoardSettings       `json:"settings"`
	Columns       []BoardColumn       `json:"columns"`
	Members       []BoardMember       `json:"members"`
	Subscriptions []BoardSubscription `json:"subscriptions"`
	Transitions   []BoardTransition   `json:"transitions"`
}

func DefaultBoardColumnTemplate() []BoardColumnInput {
	out := []BoardColumnInput{
		{Slug: string(TaskColumnBacklog), Label: "Backlog", Position: 0, IsBacklog: true},
	}
	pos := 1
	for _, col := range BoardColumns {
		out = append(out, BoardColumnInput{
			Slug:      string(col),
			Label:     defaultColumnLabel(col),
			Position:  pos,
			IsBacklog: false,
		})
		pos++
	}
	return out
}

func defaultColumnLabel(col TaskColumn) string {
	labels := map[TaskColumn]string{
		TaskColumnTodo:         "Todo",
		TaskColumnInProgress:   "In Progress",
		TaskColumnAnalizReview: "Analiz Review",
		TaskColumnCodeReview:   "Code Review",
		TaskColumnReadyForQA:   "Ready for QA",
		TaskColumnInQA:         "In QA",
		TaskColumnNeedRevision: "Need Revision",
		TaskColumnPMUAT:        "PM UAT",
		TaskColumnHumanUAT:     "Human UAT",
		TaskColumnBlocked:      "Blocked",
		TaskColumnDone:         "Done",
		TaskColumnReleased:     "Released",
	}
	if label, ok := labels[col]; ok {
		return label
	}
	return string(col)
}
