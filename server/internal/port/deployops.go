package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DeploymentRunStore persists the local mirror of GitHub Actions deploy runs.
type DeploymentRunStore interface {
	// Upsert inserts or updates on (repository_id, run_id). Fields the
	// monitor does not know — trigger_source, triggered_by, rollback_of_sha
	// — are preserved on update when the incoming values are empty, so
	// reconciliation is never undone by a later poll.
	Upsert(ctx context.Context, run domain.DeploymentRun) (domain.DeploymentRun, error)
	// Latest returns the most recently started run for (repository, env), or
	// an error wrapping ErrNotFound when the env has never deployed.
	Latest(ctx context.Context, repositoryID uuid.UUID, env string) (domain.DeploymentRun, error)
	// LatestAll returns the most recent run per (repository, env) across every
	// repository — one query for the whole matrix.
	LatestAll(ctx context.Context) ([]domain.DeploymentRun, error)
	// ListByEnv returns runs newest-first, capped at limit.
	ListByEnv(ctx context.Context, repositoryID uuid.UUID, env string, limit int) ([]domain.DeploymentRun, error)
	// LastSuccessfulBefore returns the newest successful run for (repository,
	// env) whose head SHA differs from excludeSHA — the rollback target. It
	// returns an error wrapping ErrNotFound when there is no such run.
	LastSuccessfulBefore(ctx context.Context, repositoryID uuid.UUID, env, excludeSHA string) (domain.DeploymentRun, error)
	// ByRunID returns one run by its (repository, run id) key, or an error
	// wrapping ErrNotFound. It is how a locally-driven deploy's finish report
	// finds the row its start report created, so the update can merge onto
	// what is already stored instead of blanking it.
	ByRunID(ctx context.Context, repositoryID uuid.UUID, runID int64) (domain.DeploymentRun, error)
	// Stamp applies the attribution a reconciled dispatch carries.
	Stamp(ctx context.Context, repositoryID uuid.UUID, runID int64, triggerSource, triggeredBy, rollbackOfSHA string) error
}

// DeployDispatchStore persists dispatch intent awaiting reconciliation.
type DeployDispatchStore interface {
	Create(ctx context.Context, d domain.DeployDispatch) (domain.DeployDispatch, error)
	// ListPending returns every dispatch still in the pending state, oldest first.
	ListPending(ctx context.Context) ([]domain.DeployDispatch, error)
	// Resolve moves a dispatch to matched (with runID) or abandoned (runID nil).
	Resolve(ctx context.Context, id uuid.UUID, state string, runID *int64) error
}

// OpsAuditStore persists console action attempts.
type OpsAuditStore interface {
	Log(ctx context.Context, entry domain.OpsAuditEntry) error
	// List returns entries newest-first. A nil repositoryID means every repository.
	List(ctx context.Context, repositoryID *uuid.UUID, limit int) ([]domain.OpsAuditEntry, error)
}

// ActionsRun is one GitHub Actions run, reduced to what deployops needs.
type ActionsRun struct {
	ID           int64
	RunNumber    int
	HeadSHA      string
	HeadBranch   string
	Status       string
	Conclusion   string
	HTMLURL      string
	Event        string
	RunStartedAt time.Time
	UpdatedAt    time.Time
}

// ActionsClient is what deployops needs from GitHub Actions. The concrete
// implementation wraps the free functions in internal/adapter/vcs/github; this
// interface exists so the service and monitor are testable against fakes.
type ActionsClient interface {
	// ListWorkflowRuns returns recent runs of workflowFile. An empty branch
	// means every branch.
	ListWorkflowRuns(ctx context.Context, owner, repo, workflowFile, branch string) ([]ActionsRun, error)
	DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string) error
	// CreateTag points refs/tags/<tag> at sha. It is how a rollback names a
	// past commit, because workflow_dispatch accepts only a branch or tag.
	CreateTag(ctx context.Context, owner, repo, tag, sha string) error

	// The four below are the deploy watch's reads. They are on this interface
	// rather than on a second one because they answer the same question from
	// the same place — "what did GitHub see happen to this commit" — and a
	// second client would mean a second token source, a second base-URL
	// override and a second fake in every test that already has one.

	// ListRunsForCommit returns every Actions run whose head commit is sha.
	// This is the first deploy signal: a repository whose deploy is an Actions
	// job answers here.
	ListRunsForCommit(ctx context.Context, owner, repo, sha string) ([]ActionsRun, error)
	// ListRunJobs returns a run's jobs, which is what identifies the DEPLOY job
	// inside a run that also builds and tests, and what carries the job id the
	// logs are fetched by.
	ListRunJobs(ctx context.Context, owner, repo string, runID int64) ([]ActionsJob, error)
	// JobLogs returns a job's plain-text log. Untruncated: the caller decides
	// how much of it a model should see.
	JobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error)
	// CommitDeployStatus is the second and third deploy signals folded into
	// one call: the commit's combined status (what `vercel[bot]` writes, and
	// what `gh api repos/.../commits/<sha>/status` returns) and, failing that,
	// the GitHub Deployment opened against the commit. A repository that
	// deploys by push and runs no workflow of its own is only visible here.
	CommitDeployStatus(ctx context.Context, owner, repo, sha string) (CommitDeploySignal, error)
}

// ActionsJob is one job inside an Actions run.
type ActionsJob struct {
	ID          int64
	Name        string
	Status      string
	Conclusion  string
	HTMLURL     string
	StartedAt   *time.Time
	CompletedAt *time.Time
}

// CommitDeploySignal is what the non-Actions surfaces report about a commit.
//
// Kind is empty when nothing reported at all — which is a real and common
// answer ("this repository does not deploy on merge") and must not be confused
// with pending. GitHub returns state "pending" with total_count 0 for a commit
// nobody wrote a status for, and treating that as a running deploy is how a
// watch waits forever for something that was never going to happen.
type CommitDeploySignal struct {
	// Kind is "" (nothing), "commit_status" or "deployment_status".
	Kind string
	// State is normalized to success | pending | failure.
	State string
	// Contexts names the reporters ("Vercel", "vercel[bot]"), for the card.
	Contexts []string
	// Environment is the Deployment's environment name when Kind is
	// deployment_status ("Production", "Preview").
	Environment string
	// Description / URL are the reporter's own words and link.
	Description string
	URL         string
}
