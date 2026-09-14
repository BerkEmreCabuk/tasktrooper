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
	// LatestStatusByTasks bulk-resolves the most recent pipeline status (and
	// gate reason) per task in a single query — avoids N+1 queries when
	// enriching task lists.
	LatestStatusByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]domain.TaskPipelineDigest, error)
	// SupersedePending marks pending pipelines of the task as failed with note='superseded'.
	SupersedePending(ctx context.Context, taskID uuid.UUID) error
	// FailStaleRunning marks pending/running pipelines created before cutoff
	// (filters on created_at, not started_at) as failed with note='interrupted'.
	//
	// cutoff is load-bearing and must never be 0 on a shared deployment. It was
	// called with 0 from PipelineRunner.Start, meaning "fail every unfinished
	// pipeline in this tenant" — correct when a restart meant the only process
	// had died, catastrophic when one replica restarting kills every pipeline
	// the other replicas are actively polling.
	FailStaleRunning(ctx context.Context, cutoffMinutes int) error
	// ClaimTerminal writes a pipeline's terminal status ONLY if it is still
	// unfinished, and reports whether this caller was the one that wrote it.
	//
	// It is what makes finalize safe to reach twice. The in-process poll and
	// the gate sweeper can both decide the same pipeline is done — on one
	// replica the sweeper skipped anything in PipelineRunner.inflight, but that
	// map only ever held THIS process's pipelines, so on a second replica it
	// held nothing and the sweeper finalized a pipeline the first replica was
	// mid-poll on: two QA dispatches, two column moves, two comments, for one
	// commit. The status transition is the claim; only the winner may fire the
	// side effects.
	ClaimTerminal(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, bool, error)
	// ListUnfinished returns pipelines still pending/running, oldest first.
	// This is what the reconciling sweep walks: a pipeline nobody is driving
	// (its process died, its delivery was dropped) looks exactly like one that
	// is simply still going, and the only way to tell them apart is to ask
	// GitHub — which is what the sweeper does with each of these.
	ListUnfinished(ctx context.Context, limit int) ([]domain.TaskPipeline, error)
	// ListUnfinishedByHeadSHA returns the repository's unfinished pipelines for
	// one commit. A workflow_run webhook delivery carries a head SHA and
	// nothing else that identifies a task, so this is the whole join from
	// "GitHub finished a run" to "which card was waiting for it".
	ListUnfinishedByHeadSHA(ctx context.Context, repositoryID uuid.UUID, headSHA string) ([]domain.TaskPipeline, error)
	CreateJob(ctx context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error)
	UpdateJob(ctx context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error)
	ListJobs(ctx context.Context, pipelineID uuid.UUID) ([]domain.TaskPipelineJob, error)
}
