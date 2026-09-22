package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const criteriaSweepRounds = 3

// The stages where a run is expected to move the task's own criteria forward:
// the queue it is claimed from, the column the work happens in, and the
// rework bounce. Not intake/parked/terminal (nothing runs there), not
// review/approval, and not a stage that records verdicts — there the criteria
// are someone else's work being judged, not the runner's to tick off.
func sweepsOwnCriteria(wf domain.Workflow, column domain.TaskColumn) bool {
	if wf.Has(column, domain.BehaviourCriterionVerdict) {
		return false
	}
	switch wf.KindOf(column) {
	case domain.StageKindQueue, domain.StageKindWork, domain.StageKindRework:
		return true
	default:
		return false
	}
}

func (r *Runner) sweepOpenCriteria(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	resp domain.AgentResponse,
	model string,
	policy domain.ToolPolicy,
) (domain.AgentResponse, bool, *domain.QuotaBlock) {
	// Sweeping is for the stages where the work itself happens: a reviewer is
	// judging someone else's criteria, and a terminal/parked card has nobody
	// to nag. Derived from the stage's kind rather than carried as a flag —
	// "should this stage chase its own open criteria" has exactly one right
	// answer per kind, so it was never a decision worth exposing.
	wf := r.workflowFor(ctx, job.Task.TaskType)
	if !sweepsOwnCriteria(wf, job.Task.Column) {
		return resp, true, nil
	}
	open := r.openCriteria(ctx, job)
	if len(open) == 0 {
		return resp, true, nil
	}

	rec := activity.FromContext(ctx)
	if rec != nil {
		rec.Step("criteria_sweep_start", map[string]any{"open": len(open)})
	}

	history = append(history, domain.Message{Role: domain.RoleAssistant, Content: resp.Message.Content})

	for round := 1; round <= criteriaSweepRounds; round++ {
		history = append(history, domain.Message{Role: domain.RoleUser, Content: criteriaSweepPrompt(open, round)})
		if _, err := r.agentLoop.RunTask(ctx, history, model, agentRec.ProviderType, policy,
			agent.WithLightModel(agentRec.Model),
			agent.WithCLILabel(fmt.Sprintf("%s criteria-sweep %d", job.Task.Key, round), job.Task.Title)); err != nil {
			if quotaErr, ok := domain.QuotaBlockOf(err); ok {
				return resp, true, quotaErr
			}
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Int("round", round).
				Msg("acceptance criteria sweep failed; leaving the criteria as they stand")
			return resp, true, nil
		}

		still := r.openCriteria(ctx, job)
		if len(still) == 0 {
			if rec != nil {
				rec.Step("criteria_sweep_settled", map[string]any{"rounds": round})
			}
			return resp, true, nil
		}
		open = still
		if rec != nil {
			rec.Step("criteria_sweep_round", map[string]any{"round": round, "open": len(still)})
		}
	}

	log.Info().Str("task_id", job.Task.ID.String()).Int("open", len(open)).Int("rounds", criteriaSweepRounds).
		Msg("acceptance criteria still open after the sweep loop")
	r.reportUnsettledCriteria(ctx, job, open)
	return resp, false, nil
}

func criteriaSweepPrompt(open []domain.AcceptanceCriterion, round int) string {
	var sb strings.Builder
	if round == 1 {
		sb.WriteString("Before this run is closed, settle its acceptance criteria. These are still open:\n")
	} else {
		sb.WriteString(fmt.Sprintf("These acceptance criteria are STILL open after round %d of this check:\n", round-1))
	}
	for _, c := range open {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.ID, c.Text))
	}
	sb.WriteString("\nFor EACH id above, do exactly one of three things now:\n" +
		"1. You implemented it in this run → call set_criterion_completed with that id.\n" +
		"2. It is deliberately NOT being done (out of scope, superseded, impossible as written) → call cancel_criterion with that id and a concrete reason. " +
		"That reason is stored on the criterion and posted as a task comment, so say it in a sentence a person can act on.\n" +
		"3. You overlooked it, or ran out of time → DO THE WORK NOW, in this run, then tick it with set_criterion_completed.\n")
	if round == 1 {
		sb.WriteString("Do not tick anything you did not implement. " +
			"The hand-off to code_review is refused while any criterion is open, so a criterion you silently skip parks your finished work in this column.")
	} else {
		sb.WriteString("Answer 3 is the expected one at this point: you have already had a round to say the criterion was out of scope, " +
			"and you did not. Implement what is missing and tick it, or cancel it with a reason — an unanswered criterion parks this task " +
			"and a human has to come and find out why. Do not reply with a summary of what you would do; make the change.")
	}
	return sb.String()
}

func (r *Runner) reportUnsettledCriteria(ctx context.Context, job RunJob, open []domain.AcceptanceCriterion) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("This run ended with %d acceptance criterion/criteria unsettled after %d completion checks:\n", len(open), criteriaSweepRounds))
	for _, c := range open {
		sb.WriteString("- " + c.Text + "\n")
	}
	sb.WriteString("\nThey were neither implemented nor cancelled with a reason, so the task stays in this column: the hand-off to code_review is refused while a criterion is open. Either the work is still missing, or the criterion needs a decision only a person can make.")
	if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		Content:    sb.String(),
		AuthorType: "system",
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("unsettled criteria comment failed")
	}
}

const unsettledCriteriaMarker = "unsettled acceptance criteria"

func unsettledCriteriaSummary(open int) string {
	return fmt.Sprintf("%s: %d still open after the criteria sweep", unsettledCriteriaMarker, open)
}

func isUnsettledCriteriaRun(run domain.TaskAgentRun) bool {
	return run.Status == domain.TaskAgentRunStatusFailed && strings.HasPrefix(run.Summary, unsettledCriteriaMarker)
}
