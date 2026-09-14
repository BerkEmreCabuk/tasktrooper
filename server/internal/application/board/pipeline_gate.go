package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// PipelineGateWindow is the longest a task may sit in code_review waiting for a
// build/test result before the reviewer is dispatched anyway.
//
// FORTY-FIVE MINUTES, and the number is derived rather than chosen:
//
//   - pipelineMaxWait is 30 minutes. That is the in-process poll's own budget,
//     and it is the thing that produces a REAL verdict — success, or a failure
//     that routes the task to need_revision. The gate window must be strictly
//     longer, or the board would give up on a pipeline that was about to
//     answer, and a red build would be converted into an opened review gate.
//     Everything below the window is therefore a poll that ran out; everything
//     at it is a poll that is not running at all.
//   - the remaining 15 minutes is slack for the two ways a pipeline outlives
//     its poller: a pod replaced mid-run (the queue is in memory, the row is
//     not), and GitHub Actions queue time on a busy free-tier account, where a
//     run can be `queued` for ten minutes before a runner picks it up.
//
// It is a ceiling, not a target. Every faster answer pre-empts it: the webhook
// resolves a finished run in seconds, the reconciling sweep resolves one every
// PipelineGateSweeperInterval, and the CI-unavailable and no-checks paths open
// the gate immediately without waiting at all. A card should reach this
// deadline only when nothing anywhere can say what happened — which is exactly
// the state the three wedged cards were in.
const PipelineGateWindow = 45 * time.Minute

// PipelineGateSweeperInterval is how often unfinished pipelines are re-asked.
//
// Two minutes, matching DeploySweeper rather than WorkOrderSweeper's one, and
// for the same reason: what it asks costs a GitHub round-trip per unfinished
// pipeline, where the work-order sweep asks the database next door. Unfinished
// pipelines are a small set by construction (they leave it the moment they
// finish), so this is cheap; and the latency it adds sits on top of paths that
// are already minutes long.
const PipelineGateSweeperInterval = 2 * time.Minute

// pipelineNoRunGrace is how long after a pipeline is created the board still
// believes an Actions run might appear for its commit.
//
// Five minutes. A run appears in the Actions API as `queued` within seconds of
// the event that triggers it — the wait for a RUNNER is long, the wait for the
// RUN RECORD is not. So a commit with no run at all after five minutes is not a
// slow queue: it is a workflow whose triggers do not match this branch, a
// repository with Actions turned off, or an account with no minutes left. That
// is a decidable "CI cannot answer", and it is answered here rather than 40
// minutes later by the timeout, because the timeout would say the same thing
// with less information and much later.
const pipelineNoRunGrace = 5 * time.Minute

// pipelineGateSweepBatch bounds one pass. Larger than the deploy sweeper's
// because unfinished pipelines accumulate after a restart (every pipeline the
// dead pod was polling is suddenly ownerless at once), and the point of the
// sweep is to clear exactly that backlog.
const pipelineGateSweepBatch = 100

// UnfinishedPipelineLister is the slice of the pipeline store the sweeper reads.
type UnfinishedPipelineLister interface {
	ListUnfinished(ctx context.Context, limit int) ([]domain.TaskPipeline, error)
}

// The two GitHub reads the gate resolver makes, behind package vars.
//
// Indirected for the same reason deployRef's two calls are (see the comment on
// createReleaseTag): the decision this file makes — open the review gate, or
// send the task back for revision — is the bug it was written to fix, and a
// decision that consequential has to be assertable without an HTTP round-trip.
// A test that could not distinguish "GitHub is out of quota" from "GitHub says
// the build failed" would not be testing anything. The production values are
// the free functions themselves; only the internal tests replace them.
var (
	gateListRunsByHeadSHA = githubapi.ListRunsByHeadSHA
	gateListRunJobs       = githubapi.ListRunJobs
)

// PipelineGateSweeper is the reconciling poll that keeps a missing signal from
// wedging the board.
//
// It exists because the code-review gate had exactly one key. Dispatcher defers
// the reviewing architect on a move into code_review, and the ONLY thing that
// ever re-enters the dispatcher is PipelineRunner.finalize reaching a terminal
// state inside the process that started the pipeline. Every way that can fail
// to happen — a pod replaced mid-poll, a queue overflow, a webhook that was
// never registered for the event, a CI account with no minutes left — left the
// card in code_review with a spinner and no agent, permanently, with no error
// anywhere saying so.
//
// It is shaped like DeploySweeper and not like DeviceSweeper for the reason
// that split those two: what it asks about is per-PIPELINE. Two unfinished
// pipelines are waiting on two different commits, and the newer one's answer
// may well arrive first — so this lists without claiming, asks about each, and
// settles only the ones that can be settled.
type PipelineGateSweeper struct {
	store    UnfinishedPipelineLister
	resolver *PipelineRunner
	window   time.Duration
}

func NewPipelineGateSweeper(store UnfinishedPipelineLister, resolver *PipelineRunner, window time.Duration) *PipelineGateSweeper {
	if window <= 0 {
		window = PipelineGateWindow
	}
	return &PipelineGateSweeper{store: store, resolver: resolver, window: window}
}

// Start runs the sweep on interval until ctx ends.
//
// It sweeps immediately on boot, and here that is not a nicety — it is the main
// event. A restart is the single most common way a pipeline loses its poller,
// so the moment after a restart is the moment the largest number of cards are
// wedged. Waiting a full interval to notice would add the interval to every one
// of them.
func (s *PipelineGateSweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.store == nil || s.resolver == nil {
		return
	}
	if interval <= 0 {
		interval = PipelineGateSweeperInterval
	}
	go func() {
		tenant.Sweep(ctx, "pipeline_gate", s.sweep)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tenant.Sweep(ctx, "pipeline_gate", s.sweep)
			}
		}
	}()
	log.Info().Dur("interval", interval).Dur("gate_window", s.window).Msg("pipeline gate sweeper started")
}

// sweep resolves every unfinished pipeline it can. Exposed separately from
// Start so tests can drive it without a clock.
func (s *PipelineGateSweeper) sweep(ctx context.Context) {
	unfinished, err := s.store.ListUnfinished(ctx, pipelineGateSweepBatch)
	if err != nil {
		log.Warn().Err(err).Msg("pipeline gate sweeper: listing unfinished pipelines failed")
		return
	}
	for _, p := range unfinished {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := s.resolver.ResolveUnfinished(ctx, p, s.window); err != nil {
			// A pipeline that cannot be resolved this pass is left exactly as it
			// was and asked again next pass. The one thing that must not happen
			// is opening the gate on an error: that would dispatch a reviewer
			// because the control plane had a bad minute.
			log.Warn().Err(err).Str("pipeline_id", p.ID.String()).
				Msg("pipeline gate sweeper: resolving a pipeline failed")
		}
	}
}

// ResolveByHeadSHA settles every unfinished pipeline of a repository that is
// waiting on one commit. This is the webhook's entry point: a workflow_run or
// check_suite delivery names a head SHA and a conclusion, and this turns that
// into the task_pipelines write and the board move it implies.
//
// It returns how many pipelines it looked at, which is what the HTTP layer
// echoes back so a delivery that matched nothing is visibly distinct from one
// that did work.
func (p *PipelineRunner) ResolveByHeadSHA(ctx context.Context, repositoryID uuid.UUID, headSHA string) (int, error) {
	headSHA = strings.TrimSpace(headSHA)
	if p == nil || p.store == nil || headSHA == "" {
		return 0, nil
	}
	pipelines, err := p.store.ListUnfinishedByHeadSHA(ctx, repositoryID, headSHA)
	if err != nil {
		return 0, err
	}
	for _, pipeline := range pipelines {
		// The window is irrelevant here: a webhook is evidence that GitHub has
		// something to say, so this pass either reads a terminal result or
		// leaves the pipeline alone. Passing the full window is what stops a
		// delivery for one job of a multi-job gate from timing the gate out
		// while its siblings are still running.
		if rerr := p.ResolveUnfinished(ctx, pipeline, PipelineGateWindow); rerr != nil {
			log.Warn().Err(rerr).Str("pipeline_id", pipeline.ID.String()).
				Msg("webhook: resolving a pipeline from a workflow event failed")
		}
	}
	return len(pipelines), nil
}

// ResolveUnfinished asks GitHub what happened to a pipeline this process is not
// driving, and settles it: a real result finalizes normally (success hands the
// task to its reviewer, failure sends it to need_revision), and an answer that
// can never come opens the gate with a reason recorded.
//
// window is how long the pipeline is allowed to stay unresolved before the gate
// opens on time alone. Everything else short-circuits it — see the ordering of
// the checks below, which is deliberately "can this ever be answered" BEFORE
// "has it been answered", because the cases that can never be answered are the
// ones that would otherwise wait out the full window for nothing.
func (p *PipelineRunner) ResolveUnfinished(ctx context.Context, pipeline domain.TaskPipeline, window time.Duration) error {
	if p == nil || p.store == nil {
		return nil
	}
	if window <= 0 {
		window = PipelineGateWindow
	}
	// Somebody in this process is already polling it. Two resolvers on one
	// pipeline would write two sets of job rows for the same run.
	if _, busy := p.inflight.Load(pipeline.ID); busy {
		return nil
	}
	// A deploy pipeline is not a review gate: a task parked on one is parked on
	// the deploy-watch resource and DeploySweeper is what releases it. Settling
	// it from here would move a task nothing here understands.
	if isDeployTrigger(pipeline.Trigger) {
		return nil
	}

	fresh, err := p.store.Get(ctx, pipeline.ID)
	if err != nil {
		if errors.Is(err, domain.ErrPipelineNotFound) {
			return nil
		}
		return err
	}
	if fresh.Status != domain.PipelineStatusPending && fresh.Status != domain.PipelineStatusRunning {
		return nil
	}

	if p.taskReader == nil {
		return fmt.Errorf("pipeline gate: no task reader wired, cannot resolve pipeline %s", fresh.ID)
	}
	task, err := p.taskReader.GetTask(ctx, fresh.RepositoryID, fresh.TaskID)
	if err != nil {
		return fmt.Errorf("pipeline gate: read task: %w", err)
	}
	job := pipelineJob{Pipeline: fresh, RepositoryID: fresh.RepositoryID, Task: task}

	// The card moved on while the pipeline hung — a human dragged it, the
	// reviewer was dispatched by some other path. Settle the ROW so it stops
	// showing a spinner, but fire no side effects: DispatchQA on a task that is
	// no longer in code_review would put an agent on whatever column it reached.
	if task.Column != domain.TaskColumnCodeReview {
		return p.settleQuietly(ctx, fresh, "the task left code_review before this pipeline reported")
	}

	expired := time.Since(fresh.CreatedAt) >= window

	// 1. Nothing was ever configured to run. Decidable without GitHub, and the
	//    fastest possible answer.
	var mappings []domain.RepositoryPipelineJob
	if p.jobs != nil {
		if m, jerr := p.jobs.ListByRepository(ctx, fresh.RepositoryID); jerr != nil {
			log.Warn().Err(jerr).Str("repository_id", fresh.RepositoryID.String()).
				Msg("pipeline gate: listing job mappings failed")
		} else {
			mappings = m
		}
	}
	targets := filterMappings(mappings, domain.PipelineTargetJob,
		domain.PipelineCategoryValidate, domain.PipelineCategoryBuild,
		domain.PipelineCategoryTest, domain.PipelineCategoryMutationTest)
	if len(targets) == 0 {
		return p.openGate(ctx, job, fresh, domain.PipelineGateReasonNoCI,
			"no validate/build/test job is mapped for this repository, so there was never a check to wait for")
	}

	// 2. We cannot ask. Not the same as an answer, so this only opens the gate
	//    once the window has run out.
	token := ""
	if p.tokens != nil {
		if t, terr := p.tokens(ctx); terr == nil {
			token = strings.TrimSpace(t)
		}
	}
	if token == "" {
		if expired {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonCIUnavailable,
				"GitHub is not connected, so no build result can reach the board")
		}
		return nil
	}
	owner, repoName, ok := "", "", false
	if repo, rerr := p.repos.ResolveRepository(ctx, fresh.RepositoryID); rerr == nil {
		owner, repoName, ok = githubapi.ParseOwnerRepo(repo.RemoteURL)
	}
	if !ok {
		if expired {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonCIUnavailable,
				"this repository has no GitHub remote, so its Actions runs cannot be read")
		}
		return nil
	}
	// A pipeline written before head_sha existed, or one that died before it
	// resolved its git info. There is nothing to ask about; only the clock can
	// settle it. This is the path the three originally-wedged cards take.
	if strings.TrimSpace(fresh.HeadSHA) == "" {
		if expired {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonTimeout,
				"this pipeline never recorded the commit it was about, so its result cannot be looked up")
		}
		return nil
	}
	gitInfo := domain.TaskGitInfo{Owner: owner, Repo: repoName, HeadSHA: fresh.HeadSHA}

	// 3. Ask GitHub.
	runs, err := gateListRunsByHeadSHA(ctx, token, owner, repoName, fresh.HeadSHA)
	if err != nil {
		if githubapi.IsCIUnavailable(err) {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonCIUnavailable,
				"GitHub refused to report Actions runs for this repository ("+err.Error()+")")
		}
		log.Warn().Err(err).Str("sha", fresh.HeadSHA).Msg("pipeline gate: listing runs by head sha failed")
		if expired {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonTimeout,
				"no build result arrived within "+window.String()+" and GitHub could not be reached to ask why")
		}
		return nil
	}
	if len(runs) == 0 {
		if time.Since(fresh.CreatedAt) >= pipelineNoRunGrace {
			return p.openGate(ctx, job, fresh, domain.PipelineGateReasonCIUnavailable,
				"no GitHub Actions run exists for "+domain.ShortSHA(fresh.HeadSHA)+
					" — CI is disabled, out of quota, or no workflow triggers on this branch")
		}
		return nil
	}

	byName := map[string]githubapi.RunJob{}
	for _, run := range runs {
		runJobs, jerr := gateListRunJobs(ctx, token, owner, repoName, run.ID)
		if jerr != nil {
			continue
		}
		for _, rj := range runJobs {
			mergeJob(byName, rj)
		}
	}

	// 4. A complete answer settles it, whichever way it went. This is the path
	//    that sends a genuinely FAILED build to need_revision rather than
	//    opening the review gate on it — finalize does that, and it is the same
	//    finalize the in-process poll calls.
	if done, jobs, status := p.evaluate(ctx, fresh.ID, gitInfo, token, targets, byName, false); done {
		fresh = p.markProvider(ctx, fresh, domain.PipelineProviderGitHubActions)
		return p.finalize(ctx, job, fresh, status, jobs)
	}
	if !expired {
		return nil
	}
	// 5. Out of time with an incomplete answer. If any check that DID report
	//    reported red, this is a failed build with a straggler attached, and a
	//    failed build must go to need_revision — never to an opened gate. Only
	//    when nothing red has been seen is the deadline a gate-open.
	if anyReportedFailure(targets, byName) {
		done, jobs, status := p.evaluate(ctx, fresh.ID, gitInfo, token, targets, byName, true)
		if done {
			fresh = p.markProvider(ctx, fresh, domain.PipelineProviderGitHubActions)
			return p.finalize(ctx, job, fresh, status, jobs)
		}
	}
	return p.openGate(ctx, job, fresh, domain.PipelineGateReasonTimeout,
		"no build result arrived within "+window.String())
}

// anyReportedFailure reports whether any mapped check has completed with a
// non-success conclusion. It is the difference between "we ran out of patience"
// and "the build is red and one job is still hanging".
func anyReportedFailure(targets []mappingTarget, byName map[string]githubapi.RunJob) bool {
	for _, t := range targets {
		rj, ok := byName[t.ref]
		if ok && rj.Status == "completed" && rj.Conclusion != "success" {
			return true
		}
	}
	return false
}

// openGate ends the wait: the pipeline is recorded as skipped with the reason
// why, a note is left on the card, and finalize dispatches the reviewer.
//
// Skipped, not success, and for the reason migration 067 introduced that status:
// nothing built and nothing tested, so painting it green would make the gate
// look satisfied by work that never happened. PipelineStatusOpensGate lets it
// through anyway, which is the whole point — the board keeps moving, and it
// keeps moving with an honest record of what it moved on.
func (p *PipelineRunner) openGate(ctx context.Context, job pipelineJob, pipeline domain.TaskPipeline, reason, note string) error {
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	pipeline.Provider = domain.PipelineProviderNone
	pipeline.GateReason = reason
	pipeline.Note = note

	marker := domain.TaskPipelineJob{
		PipelineID: pipeline.ID,
		Name:       gateReasonLabel(reason),
		Command:    reason,
		Status:     domain.PipelineJobStatusSkipped,
		Output:     note,
		Position:   0,
	}
	created, err := p.store.CreateJob(finCtx, marker)
	if err != nil {
		created = marker
	}

	// The card says why. Without it the user sees a spinner stop and an
	// architect appear, with nothing anywhere distinguishing that from CI
	// having passed — which is the exact confusion this whole change exists to
	// remove.
	if p.tasks != nil {
		if _, cerr := p.tasks.AddComment(finCtx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content: "Code review başlatıldı ama arkasında yeşil bir pipeline YOK — " +
				gateReasonLabel(reason) + ": " + note + ".",
		}); cerr != nil {
			log.Warn().Err(cerr).Str("task_id", job.Task.ID.String()).Msg("pipeline gate: comment failed")
		}
	}

	log.Warn().
		Str("task_id", job.Task.ID.String()).
		Str("pipeline_id", pipeline.ID.String()).
		Str("gate_reason", reason).
		Str("note", note).
		Msg("code review gate opened without a build result")

	return p.finalize(ctx, job, pipeline, domain.PipelineStatusSkipped, []domain.TaskPipelineJob{created})
}

// settleQuietly marks a pipeline terminal with NO side effects. Used when the
// task has already left code_review: the row must stop claiming to be running,
// but nothing about the board should move because of it.
func (p *PipelineRunner) settleQuietly(ctx context.Context, pipeline domain.TaskPipeline, note string) error {
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	finishedAt := time.Now()
	pipeline.Status = domain.PipelineStatusSkipped
	pipeline.Note = note
	pipeline.FinishedAt = &finishedAt
	pipeline.Jobs = nil
	if _, err := p.store.Update(finCtx, pipeline); err != nil {
		return err
	}
	log.Info().Str("pipeline_id", pipeline.ID.String()).Str("note", note).
		Msg("pipeline settled without side effects")
	return nil
}

// gateReasonLabel is the short human phrase for a gate reason, used for the job
// row's name and the task comment. The SPA renders its own translated strings
// from the code; this is what the API and the logs say.
func gateReasonLabel(reason string) string {
	switch reason {
	case domain.PipelineGateReasonNoCI:
		return "no CI configured"
	case domain.PipelineGateReasonCIUnavailable:
		return "CI unavailable"
	case domain.PipelineGateReasonTimeout:
		return "CI did not report in time"
	case domain.PipelineGateReasonDisabled:
		return "gate disabled for this repository"
	default:
		return "gate opened"
	}
}

// compile-time proof the postgres store satisfies what the sweeper reads.
var _ UnfinishedPipelineLister = (port.TaskPipelineStore)(nil)
