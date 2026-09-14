package domain

import (
	"time"

	"github.com/google/uuid"
)

// Deploy run lifecycle, mirroring GitHub Actions' own vocabulary.
const (
	RunStatusQueued     = "queued"
	RunStatusInProgress = "in_progress"
	RunStatusCompleted  = "completed"

	RunConclusionSuccess   = "success"
	RunConclusionFailure   = "failure"
	RunConclusionCancelled = "cancelled"
)

// Who caused a run. External is the default: a push, a schedule, or a
// dispatch this system did not make.
//
// Local is the odd one: the run never existed on GitHub at all. It is a
// break-glass deploy driven from somebody's machine (the repos' own
// scripts/release-local.sh) while Actions cannot run, reported back so the
// console still answers "what is live where". Its RunID is negative — see
// LocalRunID — because GitHub's run ids are positive, so the two can share the
// (repository_id, run_id) key without ever colliding.
const (
	TriggerSourceUI       = "ui"
	TriggerSourceRollback = "rollback"
	TriggerSourceExternal = "external"
	TriggerSourceLocal    = "local"
)

// LocalRunID mints the synthetic run id of a locally-driven deploy: the
// negation of the millisecond it started. Negative keeps it out of GitHub's
// id space, and monotonic keeps two local runs of the same repository apart.
func LocalRunID(t time.Time) int64 { return -t.UnixMilli() }

// IsLocalRun reports whether a run id belongs to a locally-driven deploy
// rather than a GitHub Actions run. The finish half of a local report refuses
// anything else, so a caller cannot rewrite a real Actions run by passing its
// id.
func IsLocalRun(runID int64) bool { return runID < 0 }

// DeployDispatch lifecycle. A dispatch starts pending, becomes matched when
// the monitor finds its run, and is abandoned if no run shows up in time.
const (
	DispatchStatePending   = "pending"
	DispatchStateMatched   = "matched"
	DispatchStateAbandoned = "abandoned"

	DispatchKindDeploy   = "deploy"
	DispatchKindRollback = "rollback"
)

// Ops audit actions.
const (
	OpsActionDeploy       = "deploy"
	OpsActionRollback     = "rollback"
	OpsActionStoreSubmit  = "store_submit"
	OpsActionStoreRelease = "store_release"
	OpsActionStorePromote = "store_promote"
	OpsActionStoreRollout = "store_rollout"
	OpsActionStoreHalt    = "store_halt"
	OpsActionStoreResume  = "store_resume"
	OpsActionStoreVerify  = "store_verify"

	OpsOutcomeOK    = "ok"
	OpsOutcomeError = "error"
)

// DeploymentRun is one GitHub Actions deploy run, mirrored locally so the
// console can answer "what is live where" and "what was the last good ref"
// without hitting the GitHub API on every page load.
type DeploymentRun struct {
	ID            uuid.UUID  `json:"id"`
	RepositoryID  uuid.UUID  `json:"repository_id"`
	Env           string     `json:"env"`
	RunID         int64      `json:"run_id"`
	RunNumber     int        `json:"run_number"`
	WorkflowFile  string     `json:"workflow_file"`
	HeadSHA       string     `json:"head_sha"`
	HeadRef       string     `json:"head_ref"`
	Status        string     `json:"status"`
	Conclusion    string     `json:"conclusion"`
	HTMLURL       string     `json:"html_url"`
	TriggerSource string     `json:"trigger_source"`
	TriggeredBy   string     `json:"triggered_by"`
	RollbackOfSHA string     `json:"rollback_of_sha"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// DeployDispatch records the intent behind a dispatch this system made.
// GitHub's dispatch endpoint returns 204 with no run id, so the run can only
// be attributed after the fact — the monitor reconciles these.
type DeployDispatch struct {
	ID            uuid.UUID `json:"id"`
	RepositoryID  uuid.UUID `json:"repository_id"`
	Env           string    `json:"env"`
	WorkflowFile  string    `json:"workflow_file"`
	Ref           string    `json:"ref"`
	Kind          string    `json:"kind"`
	RollbackOfSHA string    `json:"rollback_of_sha"`
	Actor         string    `json:"actor"`
	State         string    `json:"state"`
	MatchedRunID  *int64    `json:"matched_run_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// OpsAuditEntry records one console action attempt, successful or not.
type OpsAuditEntry struct {
	ID           uuid.UUID         `json:"id"`
	RepositoryID *uuid.UUID        `json:"repository_id,omitempty"`
	Action       string            `json:"action"`
	Target       string            `json:"target"`
	Actor        string            `json:"actor"`
	Detail       map[string]string `json:"detail"`
	Outcome      string            `json:"outcome"`
	Error        string            `json:"error"`
	CreatedAt    time.Time         `json:"created_at"`
}
