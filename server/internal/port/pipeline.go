package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type TaskPipelineStore interface {
	Create(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error)
	Update(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error)
	Get(ctx context.Context, id uuid.UUID) (domain.TaskPipeline, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskPipeline, error)
	LatestByTask(ctx context.Context, taskID uuid.UUID) (domain.TaskPipeline, error)
	// Bulk-resolves the most recent pipeline status per task in one query —
	// avoids N+1 when enriching task lists.
	LatestStatusByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]domain.TaskPipelineDigest, error)
	SupersedePending(ctx context.Context, taskID uuid.UUID) error
	// cutoff filters on created_at, not started_at, and must never be 0 on a
	// shared deployment: 0 means "fail every unfinished pipeline", killing the
	// pipelines other replicas are actively polling when one restart fails.
	FailStaleRunning(ctx context.Context, cutoffMinutes int) error
	// Writes the terminal status ONLY if the pipeline is still unfinished; the
	// status transition is the claim, and only the winner may fire side
	// effects — what keeps finalize safe to reach twice.
	ClaimTerminal(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, bool, error)
	// A pipeline nobody is driving looks exactly like one still going; the
	// only way to tell them apart is to ask GitHub, which the sweeper does.
	ListUnfinished(ctx context.Context, limit int) ([]domain.TaskPipeline, error)
	// A workflow_run webhook carries a head SHA and nothing else identifying a
	// task, so this is the whole join from "GitHub finished a run" to "which
	// card was waiting for it".
	ListUnfinishedByHeadSHA(ctx context.Context, repositoryID uuid.UUID, headSHA string) ([]domain.TaskPipeline, error)
	CreateJob(ctx context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error)
	UpdateJob(ctx context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error)
	ListJobs(ctx context.Context, pipelineID uuid.UUID) ([]domain.TaskPipelineJob, error)
}
