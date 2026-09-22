package board

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

const reviewDiffLimit = 24000

var ErrReviewPRMissing = errors.New("code review has no pull request for the task branch")

func isReviewColumn(wf domain.Workflow, column domain.TaskColumn) bool {
	return wf.Has(column, domain.BehaviourReviewOnly)
}

// A run in done exists to MERGE and delete the branch; the post-run commit would push that branch back onto origin seconds later.
func producesADiff(wf domain.Workflow, column domain.TaskColumn) bool {
	return !isReviewColumn(wf, column) && column != domain.TaskColumnDone
}

func (r *Runner) ensureReviewPR(ctx context.Context, workspace string) (string, error) {
	if r.git == nil || workspace == "" {
		return "", nil
	}
	url, err := r.git.EnsurePullRequest(ctx, workspace)
	if err == nil {
		return url, nil
	}
	firstErr := err
	if pushErr := r.git.PushBranch(ctx, workspace); pushErr != nil {
		return "", fmt.Errorf("%w: %v (publishing the branch also failed: %v)", ErrReviewPRMissing, firstErr, pushErr)
	}
	url, err = r.git.EnsurePullRequest(ctx, workspace)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrReviewPRMissing, err)
	}
	return url, nil
}

func (r *Runner) reviewPRContext(ctx context.Context, workspace string, taskID uuid.UUID) (string, error) {
	url, err := r.ensureReviewPR(ctx, workspace)
	if err != nil {
		if r.git != nil && r.git.OriginURL(ctx, workspace) == "" {
			log.Warn().Err(err).Str("workspace", workspace).
				Msg("code review: repository has no origin, reviewing the branch diff without a pull request")
			return "", nil
		}
		return "", err
	}
	if url == "" {
		return "", nil
	}
	recordTaskPR(ctx, r.prRecorder, taskID, url)
	return "## Pull request under review: " + url + "\n" +
		"The diff below is exactly what this PR changes. Review those changes — read the rest of the repository " +
		"whenever you need it to judge them, but never review files the PR does not touch.", nil
}

func (r *Runner) revisionPRComments(ctx context.Context, job RunJob) string {
	if r.prReader == nil {
		return ""
	}
	pr, err := r.prReader.PullRequest(ctx, job.RepositoryID, job.Task.ID, false)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("revision context: pull request read failed")
		return ""
	}
	if !pr.Known || (len(pr.ReviewComments) == 0 && len(pr.Comments) == 0) {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Pull request review comments (" + pr.URL + ")\n")
	sb.WriteString("These are the reviewer's notes on the PR itself — they are part of the revision feedback, not a separate topic. " +
		"Fix what they point at in this run, and answer anything you disagree with using comment_on_pull_request.\n")
	write := func(c domain.PullRequestComment) {
		where := ""
		if c.Path != "" {
			where = " " + c.Path
			if c.Line > 0 {
				where += fmt.Sprintf(":%d", c.Line)
			}
		}
		body := c.Body
		if len(body) > 1500 {
			body = domain.TruncateHead(body, 1500) + "…"
		}
		sb.WriteString(fmt.Sprintf("- [%s%s] %s\n", c.Author, where, strings.TrimSpace(body)))
	}
	for _, c := range pr.ReviewComments {
		write(c)
	}
	for _, c := range pr.Comments {
		write(c)
	}
	return sb.String()
}

func (r *Runner) failRunNoPR(ctx context.Context, job RunJob, run domain.TaskAgentRun, cause error) error {
	reason := "Code review did not start: the task branch has no pull request. " +
		"A review is done on the PR, so the branch must be pushed and a PR opened before code_review. Details: " + cause.Error()

	if r.taskUpdater != nil {
		if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    truncateHead(reason, 3000),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("missing-PR review comment failed")
		}
	}

	log.Warn().Err(cause).Str("task_id", job.Task.ID.String()).Str("run_id", run.ID.String()).
		Msg("code review run rejected: no pull request for the task branch")

	run.Status = domain.TaskAgentRunStatusFailed
	run.Summary = truncateHead(reason, 500)
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if _, updateErr := r.runs.Update(pctx, run); updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaskAgentRunNotFound) {
			log.Info().Str("run_id", run.ID.String()).Msg("run row gone, task deleted mid-run")
			return nil
		}
		return updateErr
	}
	return fmt.Errorf("%w (task %s)", ErrReviewPRMissing, job.Task.ID)
}

func reviewDiffMessage(wf domain.Workflow, column domain.TaskColumn, diff string) string {
	limit := 8000
	heading := "## Task branch diff (changes made for this task so far)"
	if isReviewColumn(wf, column) {
		limit = reviewDiffLimit
		heading = "## Pull request diff — the complete change you are reviewing (task branch vs its base)"
	}
	if len(diff) > limit {
		diff = domain.TruncateHead(diff, limit) + "\n…(truncated)"
	}
	return heading + "\n```diff\n" + strings.TrimSpace(diff) + "\n```"
}
