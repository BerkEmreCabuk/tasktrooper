package board

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ParkJournal writes the board-history half of a park: the task.moved event the
// timeline renders, and the column-span close the KPI ledger needs.
//
// It exists because a park is the one move that must NOT go through
// Dispatcher.Dispatch, even though Dispatch is where every other move's event
// and span are written. Three reasons, and any one of them is enough:
//
//   - a blocked task is dispatch-suspended anyway (isDispatchSuspendedTask), so
//     the only part of Dispatch that would run is the part written here;
//   - Dispatch fires the push notifier on every task.moved, and a park is the
//     one move nobody wants woken up for;
//   - the park happens INSIDE a run, i.e. inside the dispatch path that started
//     it — re-entering it from there is a loop waiting for a reason to close.
//
// So the two writes are made directly, with the same payload keys Dispatch and
// repository.Service put on an ordinary move, which is what makes a park read
// on the board exactly like any other system move rather than like a card that
// silently vanished into `blocked`.
//
// Nothing here can fail a park. By the time it is called the card is already
// blocked and the run row already carries whatever the resume needs — the
// durable half is done — so a missing history row costs a line in the timeline
// and never the park itself.
type ParkJournal struct {
	events port.BoardEventStore
	spans  port.TaskColumnSpanStore
}

// NewParkJournal wires the journal. Both halves are optional: no event store
// disables it entirely, no span store still writes the event.
func NewParkJournal(events port.BoardEventStore, spans port.TaskColumnSpanStore) *ParkJournal {
	return &ParkJournal{events: events, spans: spans}
}

// Record notes that task moved from `from` into `blocked` waiting on resource,
// with reason as the system_reason the UI renders.
//
// from is the column the store reported before the park; "" falls back to the
// task's own column, which is what the caller was looking at when the run
// started. A task that was ALREADY blocked records nothing: no column changed,
// and repository.Service guards its own move event the same way.
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
		"from_column": string(from),
		"to_column":   string(domain.TaskColumnBlocked),
		// `column` and `assignee_agent_id` are what Dispatcher.Dispatch stamps on
		// every event it writes; consumers read the destination off the payload
		// rather than off the task, so a park missing them would render as a
		// half-filled row.
		"column":   string(domain.TaskColumnBlocked),
		"resource": resource,
		// A park is the control plane's move. Without the actor the UI attributes
		// it to the human looking at it — the same bug MoveReason* was added for.
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
	// Closes the visit the run was in. Without it the open span stays open for
	// the whole park, so a task that waited six hours on a quota reads as six
	// hours of in_progress work in every time-in-column KPI.
	if err := j.spans.RecordMove(ctx, repositoryID, task.ID, string(domain.TaskColumnBlocked), event.CreatedAt); err != nil {
		// A missing span costs a KPI data point, never the park — same trade
		// Dispatcher.Dispatch makes for an ordinary move.
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("park journal: recording the park column span failed")
	}
}
