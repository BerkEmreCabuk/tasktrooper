package board

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// THREE, one under the smallest number healthy work can hit, and safe because every human touch resets it.
const maxReviewLoopEntries = 3

const reviewLoopHistoryDepth = 500

type TaskEventHistory interface {
	ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.BoardEvent, error)
}

// The general brake on review ping-pong: a task kept arriving in need_revision with no human weighing in is being circulated, not reviewed.
type ReviewLoopGuard struct {
	events   TaskEventHistory
	parker   ResourceParker
	comments TaskCommenter
	parks    *ParkJournal
}

func NewReviewLoopGuard(events TaskEventHistory, parker ResourceParker) *ReviewLoopGuard {
	return &ReviewLoopGuard{events: events, parker: parker}
}

func (g *ReviewLoopGuard) SetCommenter(c TaskCommenter) {
	if g != nil {
		g.comments = c
	}
}

func (g *ReviewLoopGuard) SetParkJournal(j *ParkJournal) {
	if g != nil {
		g.parks = j
	}
}

func (g *ReviewLoopGuard) Hold(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, currentEventID uuid.UUID) bool {
	if g == nil || g.events == nil {
		return false
	}
	history, err := g.events.ListByTask(ctx, task.ID, reviewLoopHistoryDepth)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("review loop guard: reading board history failed, dispatching normally")
		return false
	}
	if currentEventID == uuid.Nil || !containsEvent(history, currentEventID) {
		log.Warn().Str("task_id", task.ID.String()).
			Msg("review loop guard: board history window does not reach this move, dispatching normally")
		return false
	}
	entries := reviewLoopEntries(history)
	if entries < maxReviewLoopEntries {
		return false
	}

	log.Warn().
		Str("task_id", task.ID.String()).
		Int("need_revision_entries", entries).
		Msg("review loop guard: task sent back repeatedly with no human input, parking it")

	if !g.park(ctx, repositoryID, task, entries) {
		return false
	}
	g.comment(ctx, repositoryID, task, entries)
	return true
}

func reviewLoopEntries(history []domain.BoardEvent) int {
	n := 0
	for i := len(history) - 1; i >= 0; i-- {
		event := history[i]
		payload := decodeEventPayload(event.Payload)
		if eventIsHuman(event, payload) {
			return n
		}
		if event.EventType != domain.BoardEventTaskMoved {
			continue
		}
		if col, ok := movedIntoColumn(payload); ok && col == string(domain.TaskColumnNeedRevision) {
			n++
		}
	}
	return n
}

func eventIsHuman(event domain.BoardEvent, payload map[string]interface{}) bool {
	if event.ActorUserID != nil && *event.ActorUserID != "" {
		return true
	}
	actor, _ := payload[domain.EventPayloadActor].(string)
	if actor == domain.EventActorHuman {
		return true
	}
	if event.EventType == domain.BoardEventTaskCommented {
		if authorType, _ := payload["author_type"].(string); authorType == "user" || authorType == "human" {
			return true
		}
	}
	return false
}

func movedIntoColumn(payload map[string]interface{}) (string, bool) {
	if payload == nil {
		return "", false
	}
	for _, synthetic := range []string{"reconciled", "resumed", domain.EventPayloadResumedResource} {
		if _, ok := payload[synthetic]; ok {
			return "", false
		}
	}
	to, ok := payload["to_column"].(string)
	if !ok || to == "" {
		return "", false
	}
	if from, _ := payload["from_column"].(string); from == to {
		return "", false
	}
	return to, true
}

func lastHumanEventAt(history []domain.BoardEvent) (time.Time, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		event := history[i]
		if eventIsHuman(event, decodeEventPayload(event.Payload)) {
			return event.CreatedAt, true
		}
	}
	return time.Time{}, false
}

func decodeEventPayload(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
}

func containsEvent(history []domain.BoardEvent, id uuid.UUID) bool {
	for _, event := range history {
		if event.ID == id {
			return true
		}
	}
	return false
}

func reviewLoopGateApplies(input DispatchInput) bool {
	if input.EventType != domain.BoardEventTaskMoved {
		return false
	}
	if input.Task.Column != domain.TaskColumnNeedRevision {
		return false
	}
	to, ok := movedIntoColumn(input.Payload)
	return ok && to == string(domain.TaskColumnNeedRevision)
}

func (g *ReviewLoopGuard) comment(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, entries int) {
	if g.comments == nil {
		return
	}
	if _, err := g.comments.AddComment(ctx, repositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content: fmt.Sprintf("review loop: sent back %d times without human input; parking for a human decision. "+
			"Aradaki turlarda hiçbir insan bu karta dokunmadı ve sonuç değişmedi, bu yüzden geliştirici tekrar "+
			"başlatılmadı. Kart `blocked` kolonunda bekliyor: ne yapılması gerektiğine karar verip elle ilerletin.", entries),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review loop guard: park comment failed")
	}
}

func (g *ReviewLoopGuard) park(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, entries int) bool {
	if g.parker == nil || task.Column == domain.TaskColumnBlocked {
		return false
	}
	detail := fmt.Sprintf("review loop: sent back %d times with no human input — waiting for a human decision", entries)
	previous, err := g.parker.BlockOnResource(ctx, repositoryID, task.ID, domain.ResourceHumanDecision, detail)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("review loop guard: parking the task failed; dispatching normally instead")
		return false
	}
	g.parks.Record(ctx, repositoryID, task, previous,
		domain.ResourceHumanDecision, domain.MoveReasonReviewLoopParked)
	return true
}
