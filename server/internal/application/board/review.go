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

// reviewDiffLimit is how much of the branch diff a review run receives.
//
// A working run gets the diff as a reminder of what it has already written, so
// a head-truncated slice is enough. A reviewer's whole job is the diff, and it
// is told to read it completely — handing it 8 KB of a 30 KB change would make
// that instruction unsatisfiable and every verdict a guess about the rest.
const reviewDiffLimit = 24000

// ErrReviewPRMissing ends a code_review run that has no pull request to review.
var ErrReviewPRMissing = errors.New("code review has no pull request for the task branch")

// isReviewColumn reports whether a column's work is judging someone else's
// change rather than producing one.
//
// The runner treats these runs differently in three places — no post-run build
// gate, no commit/push, a bigger diff budget — because a reviewer that builds,
// fixes and pushes has stopped reviewing and started implementing, and then
// signs off on its own code at the same gate.
func isReviewColumn(column domain.TaskColumn) bool {
	switch column {
	case domain.TaskColumnCodeReview, domain.TaskColumnAnalizReview, domain.TaskColumnPMUAT:
		return true
	default:
		return false
	}
}

// producesADiff reports whether a run in this column may have its workspace
// built, committed and pushed when it ends.
//
// It is isReviewColumn plus done, and done is here for a sharper reason than
// the review columns are. A run dispatched into done exists to MERGE the task's
// pull request and delete its branch; the post-run commit would then push the
// task branch straight back onto origin — re-creating a branch the merge just
// deleted, seconds after deleting it, and re-opening the question of what is on
// it. The build gate is skipped for the same reason it is skipped for a
// reviewer: nothing was written, so there is nothing to verify, and a fix round
// on a merged task would be an agent editing code that has already shipped.
func producesADiff(column domain.TaskColumn) bool {
	return !isReviewColumn(column) && column != domain.TaskColumnDone
}

// ensureReviewPR guarantees the task branch has a pull request before the
// reviewer reads it, and returns its URL.
//
// Entering code_review already fires a PR-open attempt, but that path is
// async and best-effort while the developer's branch is pushed at the END of
// its run: an attempt that raced the push left no PR at all, and the review
// then happened on a branch nobody could see. Here the branch is on origin (or
// gets published) before the reviewer starts, so "the changes are in the PR" is
// true rather than hopeful.
func (r *Runner) ensureReviewPR(ctx context.Context, workspace string) (string, error) {
	if r.git == nil || workspace == "" {
		return "", nil
	}
	url, err := r.git.EnsurePullRequest(ctx, workspace)
	if err == nil {
		return url, nil
	}
	// The most common reason a PR cannot be opened is a branch origin has never
	// seen. Publish it (without committing the tree — see PushBranch) and retry
	// once; anything still failing after that is a real configuration problem.
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

// reviewPRContext resolves the PR the reviewer is about to judge and renders
// the context message naming it. A repository with no origin has no PR to open
// and is reviewed from the diff alone (self-hosted/local repos); everywhere
// else a missing PR is an error the caller turns into a failed run.
//
// It is also where the task learns which PR it is in: this is the first moment in
// a task's life the PR is guaranteed to exist, so recording it here means the
// board (and the human's task chat) can name the PR from then on without a
// working copy and a GitHub round-trip.
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

// revisionPRComments renders the reviewer's notes ON THE PULL REQUEST for the
// run that has to act on them.
//
// The board comment and the PR comment are two different conversations, and a
// reviewer uses both: the hand-back reason goes on the card, the line-by-line
// "this null check is wrong" goes on the PR. Only the card's comments were ever
// injected, so a revision run acted on half the feedback and pushed a fix the
// reviewer had already explained was not what they asked for. Read-only and
// best effort: GitHub being unreachable degrades the context, it does not fail
// the run.
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

// failRunNoPR ends a code_review run that could not be given a pull request.
// The task stays in code_review (the work is not wrong, it is unreviewable) and
// the comment names what is missing, so the retry has something to act on
// instead of a silent second failure.
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

// reviewDiffMessage renders the branch diff for the run that receives it. The
// heading differs by audience on purpose: the reviewer is told this is the
// complete change under review, the implementer that it is their own work so
// far.
func reviewDiffMessage(column domain.TaskColumn, diff string) string {
	limit := 8000
	heading := "## Task branch diff (changes made for this task so far)"
	if isReviewColumn(column) {
		limit = reviewDiffLimit
		heading = "## Pull request diff — the complete change you are reviewing (task branch vs its base)"
	}
	if len(diff) > limit {
		diff = domain.TruncateHead(diff, limit) + "\n…(truncated)"
	}
	return heading + "\n```diff\n" + strings.TrimSpace(diff) + "\n```"
}
