package board

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type VerdictStore interface {
	SetReviewVerdict(ctx context.Context, taskID uuid.UUID, column, verdict string) error
	OpenSpan(ctx context.Context, taskID uuid.UUID) (domain.TaskColumnSpan, bool, error)
}

type EscapeCharger interface {
	ApplyReviewEscape(ctx context.Context, task domain.BoardTask, agentID uuid.UUID)
}

type ReviewGate struct {
	spans     VerdictStore
	escapes   EscapeCharger
	workflows port.WorkflowReader
	roles     port.RoleResolver
}

func NewReviewGate(spans VerdictStore, escapes EscapeCharger) *ReviewGate {
	return &ReviewGate{spans: spans, escapes: escapes}
}

func (g *ReviewGate) SetWorkflows(w port.WorkflowReader)  { g.workflows = w }
func (g *ReviewGate) SetRoleResolver(r port.RoleResolver) { g.roles = r }

// Approval becomes a verdict and is held; rejection is allowed through - human gates let work through, not send it back.
func (g *ReviewGate) InterceptAgentMove(ctx context.Context, task domain.BoardTask, from, to domain.TaskColumn, actor domain.TaskActor, repo domain.Repository) bool {
	if g == nil || g.spans == nil || actor != domain.TaskActorAgent || !repo.RequireHumanReview {
		return true
	}
	// pm_uat is not held: that made humans approve twice, and human_uat exists for exactly that signature.
	if !g.holdsForHumanApproval(ctx, task.TaskType, from) {
		return true
	}
	verdict := domain.ReviewVerdictApprove
	if to == domain.TaskColumnNeedRevision {
		verdict = domain.ReviewVerdictReject
	}
	if err := g.spans.SetReviewVerdict(ctx, task.ID, string(from), verdict); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("set review verdict failed")
	}
	return verdict == domain.ReviewVerdictReject
}

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
