package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The deploy watch: what happened to the commit a task's merge produced.
//
// Everything here is keyed on ONE thing — board_tasks.merge_commit_sha, the
// squash commit merge_task_pull_request recorded (migration 104). Not the
// branch, not "the latest deploy of this environment", not a time window. A
// default branch carries everyone else's merges too, and "the latest deploy"
// answers a question about the environment rather than about the task; both
// were how a task could be reported as released on the strength of somebody
// else's deploy.
//
// Two repositories in the same install ship in two completely different ways,
// and both had to be supported without either one knowing about the other:
//
//   - a backend with a GitHub Actions deploy job that runs on push to the
//     default branch. The signal is the Actions run for the merge commit.
//   - a frontend on a push-to-deploy host (Vercel), which runs no Actions job at
//     all. Its bot writes a GitHub **commit status** and a GitHub
//     **Deployment** against the same commit, and that is the signal — read
//     over the GitHub API, with no provider credentials anywhere in this
//     repository.
//
// So the watch is one task-scoped abstraction over three signals, resolved in
// that order and reported as one state the agent can act on.

// DeployWatchState is the settled question "did this task's commit reach
// production, and did it work".
type DeployWatchState string

const (
	// DeployWatchPending is a deploy that has started and not finished. It is
	// the state that must never be waited on inside a tool call: the agent
	// parks (ResourceDeployWatch) and is re-dispatched when it settles.
	DeployWatchPending DeployWatchState = "pending"
	// DeployWatchSuccess is a finished, green deploy of this exact commit.
	DeployWatchSuccess DeployWatchState = "success"
	// DeployWatchFailure is a finished, red deploy of this exact commit.
	DeployWatchFailure DeployWatchState = "failure"
	// DeployWatchNoSignal means the commit is on the branch and nothing —
	// no Actions run, no commit status, no Deployment — reports anything about
	// deploying it. It is NOT a failure: a repository that deploys on a manual
	// schedule, or not at all, lands here and there is nothing to roll back.
	DeployWatchNoSignal DeployWatchState = "no_signal"
	// DeployWatchUnknown is the watch itself failing (no merge commit
	// recorded, GitHub unreachable, repository coordinates unresolvable). It
	// is reported rather than guessed at, because every other state authorises
	// an action and "I could not look" authorises none of them.
	DeployWatchUnknown DeployWatchState = "unknown"
)

// Settled reports whether the state is one the agent can act on. Only pending
// is unsettled — no_signal and unknown are answers, not waits.
func (s DeployWatchState) Settled() bool { return s != DeployWatchPending }

// Deploy watch signal kinds, reported so an agent (and a human reading the
// task) can tell "the deploy job went green" from "Vercel said success".
const (
	DeploySignalActionsRun      = "actions_run"
	DeploySignalCommitStatus    = "commit_status"
	DeploySignalDeploymentState = "deployment_status"
	DeploySignalNone            = "none"
)

// DeployWatchJob is one job inside the Actions run that carried the deploy. It
// is what get_deploy_logs is pointed at when the deploy failed.
type DeployWatchJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url,omitempty"`
}

// DeployWatchStatus is the whole answer for one task.
type DeployWatchStatus struct {
	TaskID       uuid.UUID        `json:"task_id"`
	TaskKey      string           `json:"task_key,omitempty"`
	RepositoryID uuid.UUID        `json:"repository_id"`
	Env          string           `json:"env"`
	MergeSHA     string           `json:"merge_commit_sha"`
	State        DeployWatchState `json:"state"`
	// Signal names which of the three sources produced State.
	Signal string `json:"signal"`
	// Detail is one sentence a human can read on the card.
	Detail string `json:"detail,omitempty"`
	// RunID / RunURL identify the Actions run when Signal is actions_run.
	RunID  int64  `json:"run_id,omitempty"`
	RunURL string `json:"run_url,omitempty"`
	// FailedJob is the job to fetch logs for. Nil unless the deploy failed
	// through an Actions run.
	FailedJob *DeployWatchJob `json:"failed_job,omitempty"`
	// Contexts are the commit-status contexts that produced a commit_status
	// verdict ("Vercel", "vercel[bot]/deploy", …).
	Contexts []string `json:"contexts,omitempty"`
	// HealthURL / LogsURL are the environment's own two endpoints, carried
	// here so the agent does not need a second call to find them.
	HealthURL string `json:"health_url,omitempty"`
	LogsURL   string `json:"logs_url,omitempty"`
	// AutoRollback is the target's policy, which decides whether a failure is
	// rolled back by the agent or written up for a human.
	AutoRollback bool `json:"auto_rollback"`
	// HealthWindowUntil is set on a successful deploy: until then, an incident
	// opened on this environment is attributed to THIS task.
	HealthWindowUntil *time.Time `json:"health_window_until,omitempty"`
	CheckedAt         time.Time  `json:"checked_at"`
}

// Failed reports a deploy that finished red — the trigger for the rollback
// half of the loop.
func (s DeployWatchStatus) Failed() bool { return s.State == DeployWatchFailure }

// ReleaseAttribution names the task a production incident belongs to, keyed on
// the commit that was deployed.
//
// It replaces nothing and adds one thing: today's correlation
// (prodops/remedy.go, deployCorrelationWindow) asks "did SOME deploy of this
// environment finish in the last 45 minutes", which yields a generic
// "roll back the last release" with no task on it. This asks "which task's
// merge commit is what production is currently running", which yields a
// specific card, a specific rollback plan and a specific agent to wake.
type ReleaseAttribution struct {
	TaskID     uuid.UUID `json:"task_id"`
	TaskKey    string    `json:"task_key,omitempty"`
	Title      string    `json:"title,omitempty"`
	MergeSHA   string    `json:"merge_commit_sha"`
	Env        string    `json:"env"`
	DeployedAt time.Time `json:"deployed_at"`
}

// RollbackMechanism is HOW a release is undone, chosen by what the repository
// actually has rather than by configuration.
type RollbackMechanism string

const (
	// RollbackMechanismWorkflow is the existing one: tag the last known-good
	// commit and workflow_dispatch the deploy workflow at that tag
	// (deployops.Service.Rollback). It needs a deploy workflow to dispatch.
	RollbackMechanismWorkflow RollbackMechanism = "workflow_dispatch"
	// RollbackMechanismRevert is for a repository with NO deploy workflow —
	// the push-to-deploy case, where the host redeploys whatever the default
	// branch points at. There is nothing to dispatch; what actually redeploys
	// is a new commit on that branch, so the rollback is `git revert` of the
	// merge commit followed by a push.
	//
	// A squash merge produces an ordinary single-parent commit, so this is a
	// plain `git revert <sha>` — no -m, which is only for a true merge commit
	// and which would fail here with "mainline was specified but commit is not
	// a merge".
	RollbackMechanismRevert RollbackMechanism = "revert_push"
)

// TaskRollbackResult is what a rollback attempt did, successful or not.
type TaskRollbackResult struct {
	RolledBack bool              `json:"rolled_back"`
	Mechanism  RollbackMechanism `json:"mechanism,omitempty"`
	Env        string            `json:"env"`
	// Ref is the tag dispatched (workflow mechanism) or the branch pushed
	// (revert mechanism).
	Ref string `json:"ref,omitempty"`
	// RevertSHA is the revert commit for the revert mechanism.
	RevertSHA string `json:"revert_sha,omitempty"`
	// RolledBackFrom is the commit that was live and is now being undone.
	RolledBackFrom string `json:"rolled_back_from,omitempty"`
	// RolledBackTo is the commit production is being returned to (workflow
	// mechanism only; a revert has no single prior commit to name).
	RolledBackTo string    `json:"rolled_back_to,omitempty"`
	IncidentID   uuid.UUID `json:"incident_id,omitempty"`
	// Proposed is true when auto_rollback is OFF: nothing was executed, the
	// incident carries the proposal and a human has to confirm it.
	Proposed bool `json:"proposed"`
	// ManualSteps are the parts of the task's own rollback plan that this
	// system cannot perform — a migration reversal, a feature flag in somebody
	// else's console, a cache to purge by hand. They are reported LOUDLY
	// rather than skipped: a rollback that claims to be complete when half of
	// it was not is worse than one that asks.
	ManualSteps []string `json:"manual_steps,omitempty"`
	Message     string   `json:"message"`
}

// Rollback refusals. Each one is a state only a board or a human action can
// change, so the tool tells the model not to retry — same contract as the
// merge gate's sentinels.
var (
	// ErrRollbackNotMerged is a task whose change never landed. There is
	// nothing in production to undo.
	ErrRollbackNotMerged = errors.New("this task has no merge commit recorded — nothing of it is in production, so there is nothing to roll back")
	// ErrRollbackNotOwner is the authorization that replaces the human's typed
	// confirmation for an agent actor: the task asking to roll back must be the
	// task whose commit production is currently running.
	ErrRollbackNotOwner = errors.New("this task's merge commit is not what the environment is currently running — another release has shipped since, and rolling back now would undo somebody else's change")
	// auto_rollback being off is deliberately NOT an error sentinel. It is a
	// successful call that executed nothing and returned a written-up proposal
	// (TaskRollbackResult.Proposed), because the agent has to be able to report
	// that outcome as the correct one — an error result reads to a model as a
	// broken system it should work around, and the workaround it reaches for is
	// another way to change production.

	// ErrRollbackNoTrigger is asking to roll back a release that did not fail.
	ErrRollbackNoTrigger = errors.New("this task's deploy did not fail and its environment is healthy — a rollback needs a failed deploy or an open incident attributed to this release")
	// ErrRollbackNoMechanism is a repository with neither a deploy workflow
	// nor a resolvable default branch to revert on.
	ErrRollbackNoMechanism = errors.New("this repository has no deploy workflow to dispatch and no resolvable git working copy to revert on — the rollback cannot be performed from here")
	// ErrRollbackColumn is a rollback asked for from a column that never
	// released anything.
	ErrRollbackColumn = errors.New("a release can only be rolled back from `done` or `released` — this task has not been released")
)

// Deploy watch tool names. Named in domain for the same reason
// MergePullRequestToolName is: the policy layer decides things about them by
// name in more than one place, and a string literal that only matched in one of
// them would silently widen what a run may do.
const (
	DeployStatusToolName    = "get_task_deploy_status"
	DeployLogsToolName      = "get_deploy_logs"
	RollbackReleaseToolName = "rollback_task_release"
)

// ReleaseControlTools are the deploy-watch tools that CHANGE production. Only
// rollback does; the other two read. It is a list rather than a constant
// because the verdict-column narrowing iterates it.
var ReleaseControlTools = []string{RollbackReleaseToolName}

// IsReleaseControlTool reports whether name is a tool that can change what is
// running in production.
func IsReleaseControlTool(name string) bool {
	for _, t := range ReleaseControlTools {
		if t == name {
			return true
		}
	}
	return false
}

// ReleaseTagForCommit is the tag a release is dispatched at.
//
// workflow_dispatch accepts only a branch or a tag name, never a bare SHA
// (which is why deployops.Service.Rollback tags too), so shipping a specific
// commit means naming it. The name is derived from the COMMIT rather than from
// the clock — unlike the rollback tag, which is an event and wants a timestamp
// — because a release is a fact about a commit: re-releasing the same commit
// must reuse the same tag rather than accumulate one per attempt, and
// "already exists" is then a success rather than a failure.
func ReleaseTagForCommit(sha string) string {
	return "release/" + ShortSHA(strings.TrimSpace(sha))
}

// TaskRollbackRunbook renders the task's OWN rollback instructions — the
// rollback_plan, before_deploy and after_deploy fields a developer filled in —
// as the text a rollback run has to read and follow.
//
// This is the half of a rollback no mechanism can perform. `git revert` undoes
// code; it does not reverse a migration, turn a feature flag back off, purge a
// CDN or tell an on-call human that a manual switch has to be flipped back. The
// developer who wrote the change is the only one who knew which of those
// applied, they wrote it down in these fields, and until now the fields were
// only ever read on the way OUT (postPreDeployChecklist, postDeployNotes) —
// never on the way back.
//
// Empty when the task recorded none, so a rollback of a task with no plan says
// "no rollback plan was recorded" rather than pretending one was followed.
func TaskRollbackRunbook(task BoardTask) string {
	var sections []string
	if plan := trimmedTaskField(task.RollbackPlan); plan != "" {
		sections = append(sections, "Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):\n"+plan)
	}
	if before := trimmedTaskField(task.BeforeDeploy); before != "" {
		sections = append(sections, "What had to happen BEFORE this was deployed (each of these may need undoing, in reverse order):\n"+before)
	}
	if after := trimmedTaskField(task.AfterDeploy); after != "" {
		sections = append(sections, "What was done AFTER the deploy (undo anything here that is now pointing at code that no longer exists):\n"+after)
	}
	return strings.Join(sections, "\n\n")
}

// HasRollbackRunbook reports whether the task carries any rollback instructions
// at all.
func HasRollbackRunbook(task BoardTask) bool { return TaskRollbackRunbook(task) != "" }

func trimmedTaskField(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
