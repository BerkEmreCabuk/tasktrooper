package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type DeploymentRunStore interface {
	Upsert(ctx context.Context, run domain.DeploymentRun) (domain.DeploymentRun, error)
	Latest(ctx context.Context, repositoryID uuid.UUID, env string) (domain.DeploymentRun, error)
	LatestAll(ctx context.Context) ([]domain.DeploymentRun, error)
	ListByEnv(ctx context.Context, repositoryID uuid.UUID, env string, limit int) ([]domain.DeploymentRun, error)
	LastSuccessfulBefore(ctx context.Context, repositoryID uuid.UUID, env, excludeSHA string) (domain.DeploymentRun, error)
	ByRunID(ctx context.Context, repositoryID uuid.UUID, runID int64) (domain.DeploymentRun, error)
	Stamp(ctx context.Context, repositoryID uuid.UUID, runID int64, triggerSource, triggeredBy, rollbackOfSHA string) error
}

type DeployDispatchStore interface {
	Create(ctx context.Context, d domain.DeployDispatch) (domain.DeployDispatch, error)
	ListPending(ctx context.Context) ([]domain.DeployDispatch, error)
	Resolve(ctx context.Context, id uuid.UUID, state string, runID *int64) error
}

type OpsAuditStore interface {
	Log(ctx context.Context, entry domain.OpsAuditEntry) error
	List(ctx context.Context, repositoryID *uuid.UUID, limit int) ([]domain.OpsAuditEntry, error)
}

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

type ActionsClient interface {
	ListWorkflowRuns(ctx context.Context, owner, repo, workflowFile, branch string) ([]ActionsRun, error)
	DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string) error
	CreateTag(ctx context.Context, owner, repo, tag, sha string) error

	ListRunsForCommit(ctx context.Context, owner, repo, sha string) ([]ActionsRun, error)
	ListRunJobs(ctx context.Context, owner, repo string, runID int64) ([]ActionsJob, error)
	JobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error)
	CommitDeployStatus(ctx context.Context, owner, repo, sha string) (CommitDeploySignal, error)
}

type ActionsJob struct {
	ID          int64
	Name        string
	Status      string
	Conclusion  string
	HTMLURL     string
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type CommitDeploySignal struct {
	Kind        string
	State       string
	Contexts    []string
	Environment string
	Description string
	URL         string
}
