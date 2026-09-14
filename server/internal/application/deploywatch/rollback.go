package deploywatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Rollbacker is the tag-and-dispatch rollback, narrowed to the agent-actor
// entry point. deploywatch never calls the human Rollback: that one takes a
// typed confirmation phrase, and a phrase an agent types is not a
// confirmation (see deployops.AgentRollbackAuthorization).
type Rollbacker interface {
	RollbackForTask(ctx context.Context, in deployops.RollbackInput, auth deployops.AgentRollbackAuthorization) (domain.DeployDispatch, error)
}

// GitReverter is the second rollback mechanism: undo the merge commit on the
// default branch and push, which is what actually redeploys a repository whose
// host builds every push.
type GitReverter interface {
	// RevertCommitOnDefaultBranch reverts sha on origin's default branch and
	// pushes. It returns the revert commit. It must never force-push and must
	// fail loudly rather than resolve a conflict on its own.
	RevertCommitOnDefaultBranch(ctx context.Context, rootPath, sha, message string) (string, error)
	HasGit(rootPath string) bool
}

// IncidentIngester opens or updates the production incident every rollback —
// executed or merely proposed — is recorded against.
type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

// Rollback triggers, in order of what they mean rather than of what they cost.
const (
	// RollbackTriggerDeployFailed is the deploy of the task's merge commit
	// finishing red.
	RollbackTriggerDeployFailed = "deploy_failed"
	// RollbackTriggerHealthIncident is the post-release health window going red
	// — an incident attributed to this task's release.
	RollbackTriggerHealthIncident = "health_incident"
)

// RollbackRequest is what a rollback attempt needs.
type RollbackRequest struct {
	RepositoryID uuid.UUID
	TaskID       uuid.UUID
	Env          string
	// Trigger is why. An empty trigger is refused: a rollback whose reason
	// nobody recorded is a rollback nobody can review afterwards.
	Trigger string
	// AgentName is the role acting, for the audit entry and the card.
	AgentName string
	// Note is the agent's own sentence about what it observed.
	Note string
	// IncidentID links this rollback to an already-open incident when the
	// caller has one (the health-window path does).
	IncidentID uuid.UUID
}

// Rollback undoes a task's release, or writes down exactly why it did not.
//
// The shape of this function is the whole feature:
//
//  1. refuse anything that is not this task's release to undo (column,
//     merge commit, ownership of what is live, an actual trigger);
//  2. if auto_rollback is OFF, stop here — open the incident, write the
//     proposal, put the task's own rollback plan on the card, and return
//     Proposed. A human confirms through the unchanged HTTP endpoint;
//  3. if auto_rollback is ON, execute — by workflow dispatch where a deploy
//     workflow exists, by revert-and-push where the host deploys on push;
//  4. either way, report the parts of the task's own rollback plan that no
//     mechanism here can perform, LOUDLY, as ManualSteps.
//
// Step 4 is the one that is easy to leave out and expensive to leave out. `git
// revert` undoes code. It does not reverse a migration, turn a feature flag
// back off, purge a CDN or un-send an email. The developer who wrote the change
// knew which of those applied and wrote it into rollback_plan/before_deploy —
// fields that until now were only ever read on the way OUT. A rollback that
// silently skips them and reports success is worse than one that asks.
func (s *Service) Rollback(ctx context.Context, req RollbackRequest) (domain.TaskRollbackResult, error) {
	if s.tasks == nil || s.targets == nil {
		return domain.TaskRollbackResult{}, ErrNotConfigured
	}
	env := strings.TrimSpace(req.Env)
	if env == "" {
		env = domain.DeployEnvProd
	}
	if strings.TrimSpace(req.Trigger) == "" {
		return domain.TaskRollbackResult{}, errors.New("a rollback needs a trigger: deploy_failed or health_incident")
	}

	task, err := s.tasks.Get(ctx, req.RepositoryID, req.TaskID)
	if err != nil {
		return domain.TaskRollbackResult{}, err
	}
	// Only a task that has been released can have a release rolled back. The
	// two columns are both accepted because the board reaches them in either
	// order depending on the repository: a repo with a prod workflow moves the
	// card to `released` on a green deploy, while a push-to-deploy repo's card
	// is still sitting in `done` when its change is already live.
	if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
		return domain.TaskRollbackResult{}, domain.ErrRollbackColumn
	}
	mergeSHA := strings.TrimSpace(task.MergeCommitSHA)
	if mergeSHA == "" {
		return domain.TaskRollbackResult{}, domain.ErrRollbackNotMerged
	}
	if err := s.assertOwnsLiveRelease(ctx, req.RepositoryID, env, mergeSHA); err != nil {
		return domain.TaskRollbackResult{}, err
	}
	if err := s.assertTrigger(ctx, task, req); err != nil {
		return domain.TaskRollbackResult{}, err
	}

	target, err := s.targets.Get(ctx, req.RepositoryID, "", env)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return domain.TaskRollbackResult{}, err
	}

	result := domain.TaskRollbackResult{
		Env:            env,
		RolledBackFrom: mergeSHA,
		IncidentID:     req.IncidentID,
		ManualSteps:    manualRollbackSteps(task),
	}

	if !target.AutoRollback {
		// The proposal path. Nothing is executed and this is NOT an error: the
		// agent has to be able to report a correct refusal on the card, and an
		// error result reads to a model as a broken system it should work
		// around.
		result.Proposed = true
		result.Message = s.proposeRollback(ctx, task, target, env, mergeSHA, req)
		return result, nil
	}

	executed, err := s.executeRollback(ctx, task, repoRollbackContext{
		Env:       env,
		MergeSHA:  mergeSHA,
		Trigger:   req.Trigger,
		AgentName: req.AgentName,
	})
	if err != nil {
		s.reportRollback(ctx, task, env, "Rollback FAILED: "+err.Error()+
			"\n\nProduction is still running this release. Escalate to a human now.", req)
		return result, err
	}
	result.RolledBack = true
	result.Mechanism = executed.Mechanism
	result.Ref = executed.Ref
	result.RevertSHA = executed.RevertSHA
	result.RolledBackTo = executed.RolledBackTo
	result.Message = executed.Message

	s.reportRollback(ctx, task, env, rollbackReport(result, task), req)
	return result, nil
}

// assertTrigger checks that something actually went wrong.
//
// A rollback with no trigger is a rollback nobody asked for, and it is the one
// mistake a model makes readily: told to watch a deploy and given a rollback
// tool, an unlucky run will use it on a deploy that was merely slow. So the
// claimed trigger is verified rather than believed:
//
//	deploy_failed    — re-resolve the watch. Only DeployWatchFailure counts;
//	                   pending, success and no_signal are all refusals.
//	health_incident  — the environment must be unhealthy. That claim is made by
//	                   the automatic path, which arrives with an incident id, or
//	                   by an agent that was woken by one; either way there is a
//	                   note to record and this is not re-derivable from GitHub,
//	                   so it is accepted with the note carried into the incident
//	                   and the audit.
func (s *Service) assertTrigger(ctx context.Context, task domain.BoardTask, req RollbackRequest) error {
	if req.Trigger != RollbackTriggerDeployFailed {
		return nil
	}
	status, err := s.statusForTask(ctx, task)
	if err != nil {
		// Fail closed: an unverifiable trigger is not a verified one.
		return fmt.Errorf("deploy watch: could not confirm the deploy failed, refusing to roll back: %w", err)
	}
	if status.State == domain.DeployWatchFailure {
		return nil
	}
	return fmt.Errorf("%w (the deploy of %s is %s, not failed)",
		domain.ErrRollbackNoTrigger, domain.ShortSHA(status.MergeSHA), status.State)
}

// assertOwnsLiveRelease is the agent-actor authorization: the task may only
// undo the release that is actually live, and only if that release is its own.
//
// It is the direct replacement for the human's typed confirmation, and it is
// checked HERE — against the deployment-run ledger — rather than being asserted
// by whatever called the tool. A task whose commit is no longer what production
// runs is asking to undo somebody else's change, which is the exact failure the
// typed phrase exists to prevent on the human path.
//
// No deployment run at all is accepted, deliberately: a push-to-deploy
// repository with the deploy monitor off has an empty ledger, and refusing
// there would make the rollback unreachable for precisely the repositories that
// need mechanism (ii). Nothing contradicts ownership in that case, and the
// board's own record (this task merged, nothing has merged since) is what
// remains.
func (s *Service) assertOwnsLiveRelease(ctx context.Context, repositoryID uuid.UUID, env, mergeSHA string) error {
	if s.runs == nil {
		return nil
	}
	latest, err := s.runs.Latest(ctx, repositoryID, env)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		// Fail CLOSED. An unreadable ledger is not evidence of ownership, and
		// a rollback authorized because a query failed is not authorized.
		return fmt.Errorf("deploy watch: reading the live deployment for %s failed, refusing to roll back on unknown state: %w", env, err)
	}
	live := strings.TrimSpace(latest.HeadSHA)
	if live == "" || strings.EqualFold(live, mergeSHA) {
		return nil
	}
	return fmt.Errorf("%w (live: %s, this task: %s)",
		domain.ErrRollbackNotOwner, domain.ShortSHA(live), domain.ShortSHA(mergeSHA))
}

type repoRollbackContext struct {
	Env       string
	MergeSHA  string
	Trigger   string
	AgentName string
}

type executedRollback struct {
	Mechanism    domain.RollbackMechanism
	Ref          string
	RevertSHA    string
	RolledBackTo string
	Message      string
}

// executeRollback picks the mechanism by what the repository actually has, not
// by configuration.
//
// A repository with a deploy workflow is rolled back by re-running that
// workflow at the last known-good commit — the existing, cloud-free
// deployops.Service.Rollback (tag + workflow_dispatch), reached through its
// agent-actor entry point.
//
// A repository WITHOUT one deploys on push, and there is nothing to dispatch:
// what redeploys it is a new commit on the default branch. So the rollback is
// `git revert <merge sha>` and a push. That is not a second-best substitute for
// the workflow path, it is the only thing that works there — dispatching a
// workflow that does not exist would report success and change nothing in
// production.
func (s *Service) executeRollback(ctx context.Context, task domain.BoardTask, rc repoRollbackContext) (executedRollback, error) {
	auth := deployops.AgentRollbackAuthorization{
		TaskID:        task.ID,
		TaskKey:       task.Key,
		OwnedMergeSHA: rc.MergeSHA,
		Trigger:       rc.Trigger,
		AgentName:     rc.AgentName,
	}
	actor := "agent"
	if rc.AgentName != "" {
		actor = "agent:" + rc.AgentName
	}

	if s.rollbacks != nil {
		dispatch, err := s.rollbacks.RollbackForTask(ctx, deployops.RollbackInput{
			RepositoryID: task.RepositoryID,
			Env:          rc.Env,
			Actor:        actor,
		}, auth)
		switch {
		case err == nil:
			return executedRollback{
				Mechanism:    domain.RollbackMechanismWorkflow,
				Ref:          dispatch.Ref,
				RolledBackTo: dispatch.RollbackOfSHA,
				Message: fmt.Sprintf("Rolled back %s by dispatching %s at %s (tag %s), returning production to %s.",
					rc.Env, dispatch.WorkflowFile, domain.ShortSHA(dispatch.RollbackOfSHA), dispatch.Ref, domain.ShortSHA(dispatch.RollbackOfSHA)),
			}, nil
		case errors.Is(err, deployops.ErrNoWorkflowMapping):
			// The push-to-deploy case. Fall through to the revert.
		default:
			return executedRollback{}, err
		}
	}
	return s.revertRollback(ctx, task, rc)
}

// revertRollback is mechanism (ii): revert the merge commit on the default
// branch and push it.
//
// A squash merge produces an ordinary single-parent commit, so this is a plain
// `git revert <sha>` — NOT `git revert -m 1 <sha>`, which is for a true merge
// commit and fails on this one with "mainline was specified but commit is not a
// merge". The adapter enforces the rest of the contract: no force, no automatic
// conflict resolution, and a push failure surfaces instead of being retried
// into something destructive.
func (s *Service) revertRollback(ctx context.Context, task domain.BoardTask, rc repoRollbackContext) (executedRollback, error) {
	if s.git == nil || s.repos == nil {
		return executedRollback{}, domain.ErrRollbackNoMechanism
	}
	repo, err := s.repos.Get(ctx, task.RepositoryID)
	if err != nil {
		return executedRollback{}, err
	}
	root := strings.TrimSpace(repo.RootPath)
	if root == "" || !s.git.HasGit(root) {
		return executedRollback{}, domain.ErrRollbackNoMechanism
	}
	message := fmt.Sprintf("revert: roll back %s (%s)\n\nThis reverts commit %s.\nRolled back automatically by TaskTrooper: %s.",
		task.Key, task.Title, rc.MergeSHA, rc.Trigger)
	revertSHA, err := s.git.RevertCommitOnDefaultBranch(ctx, root, rc.MergeSHA, message)
	if err != nil {
		return executedRollback{}, fmt.Errorf("reverting %s on the default branch: %w", domain.ShortSHA(rc.MergeSHA), err)
	}
	return executedRollback{
		Mechanism: domain.RollbackMechanismRevert,
		Ref:       revertSHA,
		RevertSHA: revertSHA,
		Message: fmt.Sprintf("Rolled back %s by reverting %s on the default branch and pushing (%s). "+
			"This repository has no deploy workflow — it deploys on push, so the revert commit IS the rollback deploy.",
			rc.Env, domain.ShortSHA(rc.MergeSHA), domain.ShortSHA(revertSHA)),
	}, nil
}

// manualRollbackSteps is everything in the task's own rollback plan that this
// system cannot perform.
//
// It is deliberately blunt: every recorded step is listed as manual, because
// this package has no way to tell which of them a mechanical rollback happened
// to cover. Over-reporting costs the agent one paragraph on the card;
// under-reporting costs a production database still carrying a migration whose
// code has been reverted.
func manualRollbackSteps(task domain.BoardTask) []string {
	var steps []string
	if task.HasMigration {
		steps = append(steps, "This task changed the DATABASE SCHEMA. Reverting the code does NOT reverse the migration — "+
			"the schema is still whatever the release left it as. Say so explicitly on the task and name who has to reverse it; do not report the rollback as complete.")
	}
	if runbook := domain.TaskRollbackRunbook(task); runbook != "" {
		steps = append(steps, "The task recorded its own rollback instructions. Perform each of them yourself and report what you did, "+
			"or say clearly which ones you could not:\n"+runbook)
	} else {
		steps = append(steps, "No rollback plan was recorded on this task, so nothing beyond the code revert has been undone. "+
			"Check by hand whether this change had a config, flag or data step, and say on the task that no plan existed.")
	}
	return steps
}

// proposeRollback is the auto_rollback=false path: write the rollback down
// where a human will act on it, execute nothing.
func (s *Service) proposeRollback(ctx context.Context, task domain.BoardTask, target domain.DeployTarget, env, mergeSHA string, req RollbackRequest) string {
	msg := fmt.Sprintf("Rollback PROPOSED, not executed — auto_rollback is off for %s.\n\n"+
		"What went wrong: %s (%s).\n"+
		"What is live: %s, merged from %s.\n"+
		"Proposed action: roll %s back off this commit.\n\n"+
		"A human has to confirm it: POST /v1/repositories/%s/deploy/%s/rollback with the repository name as the confirmation phrase. "+
		"Turning on auto_rollback for this target is what would let this be done automatically next time.",
		env, firstNonEmpty(req.Note, "the release failed"), req.Trigger,
		domain.ShortSHA(mergeSHA), task.Key, env, task.RepositoryID, env)
	if steps := manualRollbackSteps(task); len(steps) > 0 {
		msg += "\n\nEven once the code is rolled back, these are not automatic:\n- " + strings.Join(steps, "\n- ")
	}
	s.reportRollback(ctx, task, env, msg, req)
	_ = target
	return msg
}

// reportRollback puts the outcome on the card AND into an incident.
//
// Both, always, and for different readers: the card is where the task's own
// history lives and where the next agent looks, while the incident is what the
// notifier pushes to a device and what prodops' remedy machinery already knows
// how to render. A rollback that exists only in a tool result is a rollback
// nobody finds afterwards.
func (s *Service) reportRollback(ctx context.Context, task domain.BoardTask, env, message string, req RollbackRequest) {
	if s.comments != nil {
		if _, err := s.comments.AddComment(ctx, task.RepositoryID, task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    message,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("deploy watch: rollback comment failed")
		}
	}
	if s.incidents == nil {
		return
	}
	severity := domain.IncidentSeverityHigh
	if env == domain.DeployEnvProd {
		severity = domain.IncidentSeverityCritical
	}
	if _, err := s.incidents.Ingest(ctx, domain.IncidentInput{
		RepositoryID: task.RepositoryID,
		Env:          env,
		Source:       domain.IncidentSourceDeploy,
		Severity:     severity,
		Title:        fmt.Sprintf("Release rollback: %s (%s)", task.Key, env),
		Detail:       message,
		// Fingerprinted on the task, so a rollback and its follow-ups fold into
		// one incident rather than opening one per attempt.
		Fingerprint: domain.IncidentFingerprint("release-rollback", env, task.ID.String()),
		Payload: map[string]any{
			"task_id":          task.ID.String(),
			"task_key":         task.Key,
			"merge_commit_sha": task.MergeCommitSHA,
			"trigger":          req.Trigger,
			"agent":            req.AgentName,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("deploy watch: rollback incident ingest failed")
	}
}

func rollbackReport(result domain.TaskRollbackResult, task domain.BoardTask) string {
	var sb strings.Builder
	sb.WriteString(result.Message)
	sb.WriteString("\n\nThis is the MECHANICAL half only. ")
	if len(result.ManualSteps) > 0 {
		sb.WriteString("The following were NOT undone by it:\n- ")
		sb.WriteString(strings.Join(result.ManualSteps, "\n- "))
	}
	if after := domain.TaskRollbackRunbook(task); after == "" {
		sb.WriteString("\n\n(The task recorded no rollback plan, which is itself worth fixing before the next release.)")
	}
	return sb.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// AutoRollbackDecision is what the automatic path (a failed deploy, or an
// incident inside a health window) concluded. It exists so the caller — which
// is a background sweep, not an agent — can log and notify without re-deriving
// any of it.
type AutoRollbackDecision struct {
	Attribution domain.ReleaseAttribution
	Result      domain.TaskRollbackResult
	At          time.Time
}
