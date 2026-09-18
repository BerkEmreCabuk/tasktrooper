package board

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// VerdictStore is the slice of TaskColumnSpanStore the gate uses.
type VerdictStore interface {
	SetReviewVerdict(ctx context.Context, taskID uuid.UUID, column, verdict string) error
	OpenSpan(ctx context.Context, taskID uuid.UUID) (domain.TaskColumnSpan, bool, error)
}

// EscapeCharger charges a reviewer for an approval a human overturned.
type EscapeCharger interface {
	ApplyReviewEscape(ctx context.Context, task domain.BoardTask, agentID uuid.UUID)
}

// ReviewGate implements "human after agent" review.
//
// With require_human_review on, the reviewing agent still does the review: its
// approval is recorded as a verdict and the task waits for a human instead of
// advancing. That is what makes "the human found what the reviewer missed"
// observable at all — the flag used to skip the reviewing agent entirely, so
// the two never reviewed the same task and no escape could be attributed.
type ReviewGate struct {
	spans   VerdictStore
	escapes EscapeCharger
	// workflows/roles: see Dispatcher's own fields of the same name.
	workflows port.WorkflowReader
	roles     port.RoleResolver
}

func NewReviewGate(spans VerdictStore, escapes EscapeCharger) *ReviewGate {
	return &ReviewGate{spans: spans, escapes: escapes}
}

func (g *ReviewGate) SetWorkflows(w port.WorkflowReader)  { g.workflows = w }
func (g *ReviewGate) SetRoleResolver(r port.RoleResolver) { g.roles = r }

// InterceptAgentMove reports whether an agent's move out of a review column may
// proceed. An approval becomes a recorded verdict and is held; a rejection is
// recorded and allowed through, because human approval gates letting work
// through, not sending it back.
func (g *ReviewGate) InterceptAgentMove(ctx context.Context, task domain.BoardTask, from, to domain.TaskColumn, actor domain.TaskActor, repo domain.Repository) bool {
	if g == nil || g.spans == nil || actor != domain.TaskActorAgent || !repo.RequireHumanReview {
		return true
	}
	// code_review only, by default. pm_uat used to be held here too, which made
	// a human approve the same task twice: once to let it out of pm_uat and
	// again in human_uat, the column that exists for exactly that signature.
	// The duplicate bought nothing — the PM's agent verdict is already
	// recorded, and human_uat is where a person accepts the product. Which
	// stage that is is now hold_for_human_approval on the task's own workflow
	// rather than a literal TaskColumnCodeReview compare; an unreadable
	// workflow holds (fails closed) rather than waving the move through.
	if !g.holdsForHumanApproval(ctx, task.TaskType, from) {
		return true
	}
	verdict := domain.ReviewVerdictApprove
	if to == domain.TaskColumnNeedRevision {
		verdict = domain.ReviewVerdictReject
	}
	if err := g.spans.SetReviewVerdict(ctx, task.ID, string(from), verdict); err != nil {
		// Without a recorded verdict the escape cannot be attributed later, but
		// holding the task on a bookkeeping failure would strand it in review.
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("set review verdict failed")
	}
	return verdict == domain.ReviewVerdictReject
}

// holdsForHumanApproval reports whether a review-column exit at from is held
// for a human under this task's workflow. !ok (workflow unreadable) holds —
// the same fail-closed direction as every other gate here.
func (g *ReviewGate) holdsForHumanApproval(ctx context.Context, taskType domain.TaskType, from domain.TaskColumn) bool {
	if g.workflows == nil {
		return true
	}
	wf, err := g.workflows.Workflow(ctx, taskType)
	if err != nil {
		log.Warn().Err(err).Str("task_type", string(taskType)).
			Msg("review gate: workflow lookup failed, holding for human approval")
		return true
	}
	return wf.Has(from, domain.BehaviourHoldForHumanApproval)
}

// OnHumanRejection charges the reviewing agent when a human sends back
// something that agent approved at the same gate.
func (g *ReviewGate) OnHumanRejection(ctx context.Context, task domain.BoardTask, from domain.TaskColumn) {
	if g == nil || g.spans == nil || g.escapes == nil {
		return
	}
	span, ok, err := g.spans.OpenSpan(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("open span lookup failed")
		return
	}
	if !ok || span.BoardColumn != string(from) ||
		span.ReviewVerdict != domain.ReviewVerdictApprove || span.AgentID == nil {
		return
	}
	g.escapes.ApplyReviewEscape(ctx, task, *span.AgentID)
}
