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

// pass_to read off the stage's review verdict sweeps, not a fixed table: technical goes straight to human_uat, analiz_review is human-only.
func reviewExitColumn(wf domain.Workflow, column domain.TaskColumn) (domain.TaskColumn, bool) {
	target, ok := wf.Param(column, domain.BehaviourReviewVerdictSweep, "pass_to")
	if !ok || target == "" {
		return "", false
	}
	return domain.TaskColumn(target), true
}

// Must mirror Service.criteriaReviewGate: a disagreement makes the sweep ask the wrong role for a verdict.
func criterionReviewRole(wf domain.Workflow, column domain.TaskColumn) (domain.CriterionReviewRole, bool) {
	channel, ok := wf.Param(column, domain.BehaviourCriterionVerdict, "channel")
	if !ok {
		return "", false
	}
	switch channel {
	case "qa":
		return domain.CriterionReviewRoleQA, true
	case "pm":
		return domain.CriterionReviewRolePM, true
	default:
		return "", false
	}
}

func (r *Runner) missingVerdicts(ctx context.Context, job RunJob, role domain.CriterionReviewRole) []domain.AcceptanceCriterion {
	reader, ok := r.taskUpdater.(taskCriteriaReader)
	if !ok {
		return nil
	}
	items, err := reader.ListTaskCriteria(ctx, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review sweep: acceptance criteria unreadable")
		return nil
	}
	var missing []domain.AcceptanceCriterion
	for _, c := range items {
		ruled := false
		for _, check := range c.Checks {
			if check.Role == role {
				ruled = true
				break
			}
		}
		if !ruled {
			missing = append(missing, c)
		}
	}
	return missing
}

func (r *Runner) stuckVerdictNote(ctx context.Context, job RunJob) string {
	role, ok := criterionReviewRole(r.workflowFor(ctx, job.Task.TaskType), job.Task.Column)
	if !ok {
		return ""
	}
	missing := r.missingVerdicts(ctx, job, role)
	if len(missing) == 0 {
		return ""
	}
	texts := make([]string, 0, len(missing))
	for _, c := range missing {
		texts = append(texts, c.Text)
	}
	return fmt.Sprintf(" Geçiş kapısı %d kabul kriterini %s verdict'i olmadan geçirmiyor: %s.",
		len(missing), role, strings.Join(texts, "; "))
}

func (r *Runner) finalizeReviewVerdict(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	model string,
	policy domain.ToolPolicy,
	exit domain.TaskColumn,
) (bool, *domain.QuotaBlock) {
	if r.taskUpdater == nil {
		return false, nil
	}
	if role, ok := criterionReviewRole(r.workflowFor(ctx, job.Task.TaskType), job.Task.Column); ok {
		if missing := r.missingVerdicts(ctx, job, role); len(missing) > 0 {
			return false, nil
		}
	}

	ask := "Answer with ONE word and nothing else — no explanation, no tool call.\n" +
		"Based on the review you just completed: `APPROVE` if everything you required is satisfied and the work should move on to `" +
		string(exit) + "`, `REVISE` if anything you flagged still needs work.\n" +
		"This answer is recorded as your verdict and the board move is made from it, so it must match the review you wrote above."
	turn := append(append([]domain.Message{}, history...), domain.Message{Role: domain.RoleUser, Content: ask})

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("review_verdict_finalize_start", map[string]any{"column": string(job.Task.Column)})
	}
	resp, err := r.agentLoop.RunTask(ctx, turn, model, agentRec.ProviderType, policy,
		agent.WithLightModel(agentRec.Model),
		agent.WithCLILabel(job.Task.Key+" review-verdict", job.Task.Title))
	if err != nil {
		if quotaErr, ok := domain.QuotaBlockOf(err); ok {
			return false, quotaErr
		}
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review verdict finalize failed; task stays in its review column")
		return false, nil
	}

	target, ok := verdictColumn(resp.Message.Content, exit)
	if !ok {
		log.Info().Str("task_id", job.Task.ID.String()).Msg("review verdict finalize: no verdict in the answer, leaving the column alone")
		return false, nil
	}

	agentID := job.Run.AgentID
	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &target,
		Actor:        domain.TaskActorAgent,
		ActorAgentID: &agentID,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Str("target", string(target)).
			Msg("review verdict finalize: move refused")
		return false, nil
	}

	held := false
	if reader, ok := r.taskUpdater.(taskColumnReader); ok {
		if after, rerr := reader.GetTask(ctx, job.RepositoryID, job.Task.ID); rerr == nil {
			held = after.Column == job.Task.Column
		}
	}
	if held {
		log.Info().Str("task_id", job.Task.ID.String()).
			Msg("review verdict finalize: approval recorded, task held for human review")
		return true, nil
	}
	log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(target)).
		Msg("review verdict finalize: verdict recorded, task moved")
	return true, nil
}

func verdictColumn(answer string, exit domain.TaskColumn) (domain.TaskColumn, bool) {
	word := strings.ToUpper(strings.Trim(strings.TrimSpace(answer), "`*_.!\"' \n\t"))
	switch {
	case word == "APPROVE":
		return exit, true
	case word == "REVISE":
		return domain.TaskColumnNeedRevision, true
	default:
		return "", false
	}
}

// A review run's only exit is the reviewer calling move_board_task; a verdict that is stated but not acted on still becomes the move it decided on.
func (r *Runner) sweepReviewVerdict(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	resp domain.AgentResponse,
	model string,
	policy domain.ToolPolicy,
) *domain.QuotaBlock {
	if resp.Clarification != nil || resp.ResourceBlock != nil {
		return nil
	}
	wf := r.workflowFor(ctx, job.Task.TaskType)
	// No type check on purpose: only a stage whose review verdict sweeps name a pass_to ever sweeps, and analiz_review never carries one.
	exit, ok := reviewExitColumn(wf, job.Task.Column)
	if !ok {
		return nil
	}
	reader, ok := r.taskUpdater.(taskColumnReader)
	if !ok {
		return nil
	}
	fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review sweep: task re-read failed, leaving the column to the agent")
		return nil
	}
	if fresh.Column != job.Task.Column {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("Your review is finished but the task is still in `" + string(job.Task.Column) + "` — you did not record where it goes, " +
		"so the board shows it as still under review and nobody picks it up.\n")

	if role, ok := criterionReviewRole(wf, job.Task.Column); ok {
		if missing := r.missingVerdicts(ctx, job, role); len(missing) > 0 {
			sb.WriteString("\nFirst, the acceptance criteria you have not ruled on. The forward move is REFUSED while any of these " +
				"lacks your " + string(role) + " verdict — that refusal is what you hit if you already tried to move the task:\n")
			for _, c := range missing {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.ID, c.Text))
			}
			sb.WriteString("For EACH id above call review_criterion now, from what you executed in this run: approve it when your " +
				"own run covered it, reject it with an expected-vs-actual note when it failed or you could not exercise it. " +
				"Do not approve anything you did not observe.\n")
		}
	}

	sb.WriteString("\nThen leave the column, based on the verdict you just gave:\n" +
		"1. Everything you required is satisfied → call move_board_task to `" + string(exit) + "`.\n" +
		"2. Anything you flagged still needs work → call move_board_task to `need_revision`, and make sure your findings are on the task as a numbered comment.\n" +
		"If the move is refused, read the error: it names exactly what is missing, and fixing that and retrying the move is part of this run. " +
		"Do not re-review, do not start new testing, and do not change your verdict.")
	prompt := sb.String()

	rec := activity.FromContext(ctx)
	if rec != nil {
		rec.Step("review_verdict_sweep_start", map[string]any{"column": string(job.Task.Column)})
	}
	history = append(history,
		domain.Message{Role: domain.RoleAssistant, Content: resp.Message.Content},
		domain.Message{Role: domain.RoleUser, Content: prompt},
	)
	if _, err := r.agentLoop.RunTask(ctx, history, model, agentRec.ProviderType, policy,
		agent.WithLightModel(agentRec.Model),
		agent.WithCLILabel(job.Task.Key+" review-sweep", job.Task.Title)); err != nil {
		if quotaErr, ok := domain.QuotaBlockOf(err); ok {
			return quotaErr
		}
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review verdict sweep failed; task stays in its review column")
		return nil
	}
	if after, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID); err == nil && after.Column == job.Task.Column {
		log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(job.Task.Column)).
			Msg("review verdict sweep ran and the task is still in its review column")
		moved, quotaErr := r.finalizeReviewVerdict(ctx, job, agentRec, history, model, policy, exit)
		if quotaErr != nil {
			return quotaErr
		}
		if moved {
			return nil
		}
		if r.taskUpdater != nil {
			if _, cErr := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
				AuthorType: "system",
				Content: "Review tamamlandı ama kart hâlâ `" + string(job.Task.Column) + "` kolonunda: değerlendirme sonrası " +
					"`" + string(exit) + "` veya `need_revision` geçişi yapılmadı." + r.stuckVerdictNote(ctx, job) +
					" Kolonu elle taşımak gerekiyor.",
			}); cErr != nil {
				log.Warn().Err(cErr).Str("task_id", job.Task.ID.String()).Msg("review sweep: stuck-column comment failed")
			}
		}
	}
	return nil
}
