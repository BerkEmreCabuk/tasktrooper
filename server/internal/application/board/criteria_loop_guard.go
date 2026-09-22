package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type CriteriaReader interface {
	ListTaskCriteria(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error)
}

// After the guard trips, the reconciler's retry path is the only place that ever looks again.
type CriteriaLoopGuard struct {
	events   TaskEventHistory
	parker   ResourceParker
	criteria CriteriaReader
	comments TaskCommenter
	parks    *ParkJournal
}

func NewCriteriaLoopGuard(events TaskEventHistory, parker ResourceParker, criteria CriteriaReader) *CriteriaLoopGuard {
	return &CriteriaLoopGuard{events: events, parker: parker, criteria: criteria}
}

func (g *CriteriaLoopGuard) SetCommenter(c TaskCommenter) {
	if g != nil {
		g.comments = c
	}
}

func (g *CriteriaLoopGuard) SetParkJournal(j *ParkJournal) {
	if g != nil {
		g.parks = j
	}
}

func (g *CriteriaLoopGuard) Hold(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, runs []domain.TaskAgentRun) bool {
	if g == nil || g.events == nil || g.parker == nil {
		return false
	}
	if len(runs) < maxConsecutiveFailedRuns || !allUnsettledCriteriaFailures(runs) {
		return false
	}
	oldest := runs[len(runs)-1].CreatedAt

	history, err := g.events.ListByTask(ctx, task.ID, reviewLoopHistoryDepth)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("criteria loop guard: reading board history failed, leaving the retry in place")
		return false
	}
	if len(history) >= reviewLoopHistoryDepth && !historyReaches(history, oldest) {
		log.Warn().Str("task_id", task.ID.String()).Int("events", len(history)).
			Msg("criteria loop guard: board history window does not reach the streak, leaving the retry in place")
		return false
	}
	if at, ok := lastHumanEventAt(history); ok && !at.Before(oldest) {
		return false
	}

	log.Warn().
		Str("task_id", task.ID.String()).
		Int("consecutive_unsettled_failures", len(runs)).
		Msg("criteria loop guard: same acceptance criteria left open run after run with no human input, parking it")

	open := g.openCriteria(ctx, task.ID)
	// Park first, then explain - same ordering as ReviewLoopGuard, so the card says why before anything resumes.
	if !g.park(ctx, repositoryID, task, len(runs)) {
		return false
	}
	g.comment(ctx, repositoryID, task, len(runs), open)
	return true
}

func allUnsettledCriteriaFailures(runs []domain.TaskAgentRun) bool {
	if len(runs) == 0 {
		return false
	}
	for _, run := range runs {
		if !isUnsettledCriteriaRun(run) {
			return false
		}
	}
	return true
}

func (g *CriteriaLoopGuard) openCriteria(ctx context.Context, taskID uuid.UUID) []domain.AcceptanceCriterion {
	if g.criteria == nil {
		return nil
	}
	items, err := g.criteria.ListTaskCriteria(ctx, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).
			Msg("criteria loop guard: reading acceptance criteria for the park comment failed")
		return nil
	}
	open := make([]domain.AcceptanceCriterion, 0, len(items))
	for _, c := range items {
		if !c.Settled() {
			open = append(open, c)
		}
	}
	return open
}

func (g *CriteriaLoopGuard) comment(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, runCount int, open []domain.AcceptanceCriterion) {
	if g.comments == nil {
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("criteria loop: %d run in a row ended with the same acceptance criteria still open and no human input in between; parking for a human decision.\n\n", runCount))
	if len(open) > 0 {
		sb.WriteString("Still open:\n")
		for _, c := range open {
			sb.WriteString("- " + c.Text + "\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Kart `blocked` kolonunda bekliyor: kriterleri tamamlayın ya da cancel_criterion ile gerekçesiyle iptal edin, ardından kartı ilerletin.")
	if _, err := g.comments.AddComment(ctx, repositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    sb.String(),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("criteria loop guard: park comment failed")
	}
}

func (g *CriteriaLoopGuard) park(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, runCount int) bool {
	if g.parker == nil || task.Column == domain.TaskColumnBlocked {
		return false
	}
	detail := fmt.Sprintf("criteria loop: %d runs in a row left the same acceptance criteria open — waiting for a human decision", runCount)
	previous, err := g.parker.BlockOnResource(ctx, repositoryID, task.ID, domain.ResourceHumanDecision, detail)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("criteria loop guard: parking the task failed; leaving the retry in place instead")
		return false
	}
	if g.parks != nil {
		g.parks.Record(ctx, repositoryID, task, previous,
			domain.ResourceHumanDecision, domain.MoveReasonCriteriaLoopParked)
	}
	return true
}
