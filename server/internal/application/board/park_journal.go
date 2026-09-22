package board

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// The park is the one move that must NOT go through the normal event path.
type ParkJournal struct {
	events port.BoardEventStore
	spans  port.TaskColumnSpanStore
}

func NewParkJournal(events port.BoardEventStore, spans port.TaskColumnSpanStore) *ParkJournal {
	return &ParkJournal{events: events, spans: spans}
}

func (j *ParkJournal) Record(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, from domain.TaskColumn, resource, reason string) {
	if j == nil || j.events == nil {
		return
	}
	if from == "" {
		from = task.Column
	}
	if from == domain.TaskColumnBlocked {
		return
	}
	payload := map[string]interface{}{
		"from_column":             string(from),
		"to_column":               string(domain.TaskColumnBlocked),
		"column":                  string(domain.TaskColumnBlocked),
		"resource":                resource,
		domain.EventPayloadActor:  domain.EventActorSystem,
		domain.EventPayloadReason: reason,
	}
	if task.AssigneeAgentID != nil {
		payload["assignee_agent_id"] = task.AssigneeAgentID.String()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("park journal: marshalling the move payload failed")
		return
	}
	event, err := j.events.Create(ctx, domain.BoardEvent{
		RepositoryID: repositoryID,
		TaskID:       task.ID,
		EventType:    domain.BoardEventTaskMoved,
		Payload:      raw,
	})
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("park journal: recording the park move failed")
		return
	}
	if j.spans == nil {
		return
	}
	if err := j.spans.RecordMove(ctx, repositoryID, task.ID, string(domain.TaskColumnBlocked), event.CreatedAt); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("park journal: recording the park column span failed")
	}
}
