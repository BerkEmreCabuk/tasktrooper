package board

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// The half of a board run that happens on somebody else's computer.
//
// Two things live here: getting the task's checkout onto the assignee's Mac,
// and parking the card when that Mac is not there. They are together because
// they are the same fact seen twice — a run needs a laptop, and the laptop is
// either open or it is not.

// RemoteTaskDirRe keeps a repository name to what the Mac will accept as one
// path segment (its own rule: no separators, no leading dash, because these
// names become git argv). Sanitising here rather than discovering the refusal
// on the far end means the error, when there is one, is about the repository
// and not about a regular expression in another repository.
var RemoteTaskDirRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// prepareRemoteWorkspace clones the task's repository onto the assignee's Mac
// and returns where it landed.
//
// The layout is <workspace>/repos/<repo>/task-<id>, which is the same shape the
// local path uses (<workspace_root>/task-<id>) with the repository name added:
// a person opening that folder in Finder should be able to tell whose code they
// are looking at, and one flat pile of task-<uuid> directories cannot.
//
// The BRANCH is where remote and local genuinely differ, and the difference is
// not hidden. Locally, git.EnsureTaskWorkspace cuts feature/<key> from
// origin/<default> when it does not exist. The Mac deliberately will not: it
// clones and checks out, and everything else — branching, committing, pushing —
// belongs to the Claude Code session, because that is where the task's judgement
// is. So the first run of a task is handed the default branch and the session
// cuts the branch itself; the second run finds the branch on origin and gets it.
// runInstruction tells the session which of the two it is looking at.
func (r *Runner) prepareRemoteWorkspace(ctx context.Context, job RunJob, repo domain.Repository) (port.PreparedWorkspace, error) {
	member := strings.TrimSpace(job.Task.AssigneeUserID)
	if member == "" {
		// Not a park. Waiting produces no assignee, and hiding this in
		// `blocked` behind a message about a Mac would send someone to check a
		// laptop when what is missing is a name on a card.
		return port.PreparedWorkspace{}, domain.UnassignedRunError(job.Task.Key)
	}
	remote := strings.TrimSpace(repo.RemoteURL)
	if remote == "" {
		return port.PreparedWorkspace{}, fmt.Errorf(
			"repository %q has no remote_url on record, and a Mac can only be given a URL to clone — "+
				"re-import it from GitHub (or set its remote) before agents can work on it", repo.Name)
	}

	dir := RemoteTaskDir(repo, job.Task)
	branch := domain.TaskBranchName(job.Task)
	prepared, err := r.workspaces.Prepare(ctx, member, remote, dir, branch)
	if err != nil {
		return port.PreparedWorkspace{}, err
	}
	log.Info().
		Str("task_key", job.Task.Key).
		Str("member", member).
		Str("workspace", prepared.Rel).
		Str("branch", prepared.Branch).
		Bool("cloned", prepared.Cloned).
		Msg("task workspace prepared on the assignee's Mac")
	return prepared, nil
}

// RemoteTaskDir is the path under the Mac's repos folder for one task.
func RemoteTaskDir(repo domain.Repository, task domain.BoardTask) string {
	name := RemoteTaskDirRe.ReplaceAllString(strings.TrimSpace(repo.Name), "-")
	name = strings.Trim(name, "-.")
	if name == "" || !isSafeLeadingChar(name[0]) {
		// A repository named entirely in characters the Mac will not use as a
		// path segment still has to go somewhere, and its id is unambiguous.
		name = "repo-" + repo.ID.String()
	}
	return name + "/task-" + task.ID.String()
}

func isSafeLeadingChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// parkOnRunner ends a run that could not happen because the assignee's Mac is
// not connected, and leaves the card waiting rather than failed.
//
// It is parkOnQuota's twin and deliberately shaped like it, with one thing
// missing: there is no resume time to record. A quota reopens at an instant the
// CLI printed; a laptop opens when a person opens it, and nothing in this
// process can predict that. So the durable half is just the run row's summary,
// and the way back is application/board/runner_sweeper.go asking the control
// plane whether that member's Mac has come back.
//
// The run is recorded as COMPLETED rather than failed, for the same reason the
// quota park is: nothing failed. Failing would spend one of the task's three
// consecutive-failure lives on somebody's lid being shut, and would put "the
// run failed" on a task that has not started.
func (r *Runner) parkOnRunner(ctx context.Context, job RunJob, run domain.TaskAgentRun, agentRec domain.Agent, block *domain.RunnerBlock) error {
	run.Status = domain.TaskAgentRunStatusCompleted
	run.Summary = truncateHead(block.BoardDetail(), 500)

	pctx, cancelPersist := persistCtx(ctx)
	defer cancelPersist()
	if _, err := r.runs.Update(pctx, run); err != nil {
		// Unlike the quota park there is nothing on this row the resume needs —
		// the sweeper probes the control plane, not the database — so a failed
		// write costs the summary and nothing else. Logged, and the park below
		// still happens, because the card is the part that matters.
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).
			Msg("recording the offline-Mac park on the run row failed")
	}

	if r.blocker != nil {
		previous, err := r.blocker.BlockOnResource(pctx, job.RepositoryID, job.Task.ID,
			domain.ResourceRunnerNotAttached, run.Summary)
		if err != nil {
			// An unparked card sits in its working column with no live run,
			// which the reconciler eventually re-dispatches — into the same
			// absent Mac, and it parks again. Logged rather than failed for the
			// reason on parkOnResource: a queueing problem is not the task's
			// fault.
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).
				Msg("parking a task on the assignee's offline Mac failed")
		} else {
			r.parks.Record(pctx, job.RepositoryID, job.Task, previous,
				domain.ResourceRunnerNotAttached, domain.MoveReasonRunnerOffline)
		}
	}
	log.Info().
		Str("task_id", job.Task.ID.String()).
		Str("task_key", job.Task.Key).
		Str("member", job.Task.AssigneeUserID).
		Str("agent", agentRec.Name).
		Bool("not_ready", block.NotReady).
		Msg("task parked: the assignee's Mac is not connected")
	return nil
}

// maxConsecutiveRunnerParks is how many times in a row one task may park on an
// absent Mac before the next park becomes a plain failure.
//
// The same livelock brake maxConsecutiveQuotaParks is, and it is needed for a
// stronger reason here: this park has no clock at all, so a probe that is wrong
// in a way that REPEATS — a control plane answering 409 for a Mac that is
// actually attached, a member removed from the tenant whose cards nobody
// reassigned — would cycle the card for as long as the board exists, with every
// individual step looking correct.
//
// Higher than the quota's five, because an offline laptop is ordinary and a
// spent subscription is not: a person on a two-day trip should find their tasks
// waiting, not failed. Twenty parks at the sweeper's five-minute interval is
// most of two hours of continuous absence before anything is called a failure,
// and the card keeps waiting either way — what changes past the cap is that a
// human is finally told.
const maxConsecutiveRunnerParks = 20

// runnerParkStreak counts how many of the task's most recent runs — newest
// first, current run excluded — parked on an absent Mac, stopping at the first
// run that did anything else.
//
// A park leaves no distinguishing column on task_agent_runs (unlike the quota,
// which stamps quota_resume_at), so the marker is the summary the park wrote.
// That is why parkOnRunner's summary is a fixed sentence from
// RunnerBlock.BoardDetail rather than free text.
func runnerParkStreak(prevRuns []domain.TaskAgentRun, currentRunID string) int {
	streak := 0
	for _, prev := range prevRuns {
		if prev.ID.String() == currentRunID {
			continue
		}
		if !isRunnerParkSummary(prev.Summary) {
			return streak
		}
		streak++
	}
	return streak
}

func isRunnerParkSummary(summary string) bool {
	summary = strings.TrimSpace(summary)
	for _, prefix := range runnerParkSummaryPrefixes {
		if strings.HasPrefix(summary, prefix) {
			return true
		}
	}
	return false
}

// runnerParkSummaryPrefixes is the opening of EVERY sentence
// RunnerBlock.BoardDetail can return, and it has to be every one of them.
//
// There are two because the block has two states, and missing the second was a
// live bug: a Mac that attaches but never becomes ready parks the card on
// `not_ready` forever, and with only the "Waiting for" prefix listed here that
// streak counted zero every time — so the one park with no clock also had no
// brake, in exactly the case the brake was written for.
//
// Kept next to the READER rather than derived from the writer, so a reworded
// sentence fails TestEveryParkSentenceIsRecognisedAsAPark instead of silently
// switching the cap off. That test is the other half of this constant and the
// two must be changed together.
var runnerParkSummaryPrefixes = []string{
	"Waiting for the assignee's Mac",
	"The assignee's Mac is connected but still starting up",
}

// parkOrGiveUpOnRunner is parkOnRunner with the livelock brake in front of it,
// and it is what BOTH park sites call.
//
// There are two: the workspace prepare (before the session starts) and the
// executor (after it has). They are far apart in the run and the second one
// already has the task's run history in hand, which is why the brake used to
// live only there — and that was the bug this exists to close. A card that
// parks during PREPARE, every time, is exactly the repeating false positive the
// cap is for: the sweeper says the Mac is back, the prepare says it is not, the
// card parks again, and every individual step looks correct forever. Reading
// the history costs one query on a path that only runs when a run is already
// being abandoned.
//
// prevRuns may be supplied by a caller that already has it, and is fetched here
// otherwise. A failed fetch parks rather than gives up: an unknown streak must
// not be read as a long one, because the give-up is the destructive answer.
func (r *Runner) parkOrGiveUpOnRunner(
	ctx context.Context,
	job RunJob,
	run domain.TaskAgentRun,
	agentRec domain.Agent,
	block *domain.RunnerBlock,
	prevRuns []domain.TaskAgentRun,
	fail func(error) error,
) error {
	if prevRuns == nil && r.runs != nil {
		if rows, err := r.runs.ListByTask(ctx, job.Task.ID, quotaParkHistoryDepth); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).
				Msg("offline-Mac park: previous run lookup failed, parking without checking the cap")
		} else {
			prevRuns = rows
		}
	}
	if streak := runnerParkStreak(prevRuns, run.ID.String()); streak >= maxConsecutiveRunnerParks {
		log.Warn().Str("task_id", job.Task.ID.String()).Int("consecutive_parks", streak).
			Msg("offline-Mac park cap reached, failing the run instead of parking it again")
		return fail(runnerParkGiveUp(streak, block))
	}
	return r.parkOnRunner(ctx, job, run, agentRec, block)
}

// runnerParkGiveUp is the error a task gets once it has parked too many times.
func runnerParkGiveUp(streak int, block *domain.RunnerBlock) error {
	return fmt.Errorf(
		"this task has waited for the assignee's Mac %d times in a row without a run starting (%v). "+
			"Failing it instead of waiting again: either that person has not opened the TaskTrooper desktop app "+
			"in a long stretch, or the card is assigned to somebody who no longer has a machine on this workspace",
		streak, block)
}
