package domain

import (
	"github.com/google/uuid"
)

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
