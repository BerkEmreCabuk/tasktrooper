package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeStageEvidence is the span ledger reduced to what the gate asks it: which
// columns this task has been in, and what verdict its latest visit carried.
type fakeStageEvidence struct {
	verdicts map[string]string
	err      error
}

func (f *fakeStageEvidence) LatestVerdicts(context.Context, uuid.UUID) (map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.verdicts, nil
}

// visited builds a span history with no recorded verdicts — the shape every
// repository has while require_human_review is off, which is the majority.
func visited(columns ...domain.TaskColumn) *fakeStageEvidence {
	v := make(map[string]string, len(columns))
	for _, c := range columns {
		v[string(c)] = ""
	}
	return &fakeStageEvidence{verdicts: v}
}

// withVerdict stamps the latest visit to one column, the way ReviewGate does
// while require_human_review is on.
func (f *fakeStageEvidence) withVerdict(col domain.TaskColumn, verdict string) *fakeStageEvidence {
	f.verdicts[string(col)] = verdict
	return f
}

// fakeColumns is a ColumnValidator for a board whose columns were customised:
// only the slugs it was given exist.
type fakeColumns struct {
	slugs map[string]bool
}

func boardWith(columns ...domain.TaskColumn) *fakeColumns {
	m := make(map[string]bool, len(columns))
	for _, c := range columns {
		m[string(c)] = true
	}
	return &fakeColumns{slugs: m}
}

func (f *fakeColumns) ValidateColumn(_ context.Context, slug string) error {
	if f.slugs[slug] {
		return nil
	}
	return fmt.Errorf("invalid column: %s", slug)
}
func (f *fakeColumns) ValidateTransition(context.Context, string, string) error { return nil }

// fakeDeployPipelines serves a task's pipeline history to the release gate.
type fakeDeployPipelines struct {
	fakeReleasePipelineStore
	runs []domain.TaskPipeline
	err  error
}

func (f *fakeDeployPipelines) ListByTask(context.Context, uuid.UUID) ([]domain.TaskPipeline, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.runs, nil
}

func deployRun(trigger domain.PipelineTrigger, status domain.PipelineStatus) domain.TaskPipeline {
	return domain.TaskPipeline{ID: uuid.New(), Trigger: trigger, Status: status}
}

// fakePipelineJobs answers "does this repository map a real workflow for this
// deploy category" — the same question PipelineRunner.deployMapped asks.
type fakePipelineJobs struct {
	jobs []domain.RepositoryPipelineJob
	err  error
}

func (f *fakePipelineJobs) ListByRepository(context.Context, uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	return f.jobs, f.err
}
func (f *fakePipelineJobs) ListAll(context.Context) ([]domain.RepositoryPipelineJob, error) {
	return f.jobs, f.err
}
func (f *fakePipelineJobs) ReplaceForRepository(_ context.Context, _ uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	return jobs, nil
}

func mappedProdWorkflow() *fakePipelineJobs {
	return &fakePipelineJobs{jobs: []domain.RepositoryPipelineJob{{
		Category:   domain.PipelineCategoryProdDeploy,
		TargetKind: domain.PipelineTargetWorkflow,
		TargetRef:  "deploy-prod.yml",
	}}}
}

// done means "this passed its review chain". Which stages that is depends on
// the task type, and the evidence is the span ledger — every visit the task
// ever made — so rework through need_revision does not erase a passed stage.
func TestReviewChainGate(t *testing.T) {
	cases := []struct {
		name     string
		taskType domain.TaskType
		spans    *fakeStageEvidence
		prev     domain.TaskColumn
		target   domain.TaskColumn
		off      bool
		wantErr  error
		wantSaid []string // fragments the block must name, so it is actionable
	}{
		{
			name:     "task with the full chain reaches done",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
		},
		{
			name:     "bug with the full chain reaches done",
			taskType: domain.TaskTypeBug,
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
		},
		{
			name:     "human_uat is not required",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
		},
		{
			name:     "a task that bounced through need_revision and came back still passes",
			taskType: domain.TaskTypeTask,
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision,
				domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target: domain.TaskColumnDone,
		},
		{
			name:     "in_progress straight to done is refused",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnTodo, domain.TaskColumnInProgress),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"code review", "QA", "UAT"},
		},
		{
			name:     "missing code review is named",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnInQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"code review", "code_review"},
		},
		{
			name:     "missing QA is named, and being queued for it is not passing it",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskColumnPMUAT),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"QA", "in_qa"},
		},
		{
			name:     "missing UAT is named",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"UAT", "pm_uat"},
		},
		{
			name:     "released is gated too, so done cannot be skipped around",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnInProgress),
			prev:     domain.TaskColumnHumanUAT,
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"released"},
		},
		{
			// Otherwise every task already sitting in done on the day a
			// repository opts in would be stranded there, including the ones
			// the pipeline runner is about to release for real.
			name:     "the ordinary done to released promotion is not re-checked",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnInProgress, domain.TaskColumnDone),
			prev:     domain.TaskColumnDone,
			target:   domain.TaskColumnReleased,
		},
		{
			name:     "a stage whose latest visit was rejected has not been passed",
			taskType: domain.TaskTypeTask,
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT,
				domain.TaskColumnNeedRevision).withVerdict(domain.TaskColumnCodeReview, domain.ReviewVerdictReject),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewStageRejected,
			wantSaid: []string{"code review"},
		},
		{
			name:     "an approved verdict is not a rejection",
			taskType: domain.TaskTypeTask,
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT).
				withVerdict(domain.TaskColumnCodeReview, domain.ReviewVerdictApprove).
				withVerdict(domain.TaskColumnPMUAT, domain.ReviewVerdictApprove),
			target: domain.TaskColumnDone,
		},
		{
			name:     "analiz needs only analiz_review",
			taskType: domain.TaskTypeAnaliz,
			spans:    visited(domain.TaskColumnInProgress, domain.TaskColumnAnalizReview),
			target:   domain.TaskColumnDone,
		},
		{
			name:     "analiz without its review is refused",
			taskType: domain.TaskTypeAnaliz,
			spans:    visited(domain.TaskColumnInProgress),
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"analiz review", "analiz_review"},
		},
		{
			name:     "analiz is never asked for QA or UAT",
			taskType: domain.TaskTypeAnaliz,
			spans:    visited(domain.TaskColumnAnalizReview),
			target:   domain.TaskColumnReleased,
		},
		{
			name:     "moves that are not done or released are never gated",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnInProgress),
			target:   domain.TaskColumnNeedRevision,
		},
		{
			name:     "the gate is off unless the repository opted in",
			taskType: domain.TaskTypeTask,
			spans:    visited(domain.TaskColumnInProgress),
			target:   domain.TaskColumnDone,
			off:      true,
		},
		{
			name:     "unreadable history fails closed",
			taskType: domain.TaskTypeTask,
			spans:    &fakeStageEvidence{err: errors.New("boom: pool exhausted")},
			target:   domain.TaskColumnDone,
			wantErr:  domain.ErrReviewChainIncomplete,
			wantSaid: []string{"could not be read"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{spans: tc.spans, workflows: workflowtest.Default().Reader()}
			repo := domain.Repository{ID: uuid.New(), RequireReviewChain: !tc.off}
			task := domain.BoardTask{ID: uuid.New(), Key: "APP-7", TaskType: tc.taskType}

			err := svc.reviewChainGate(context.Background(), repo, task, tc.prev, tc.target)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("want the move allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
			for _, want := range tc.wantSaid {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("want %q named in the block, got: %v", want, err)
				}
			}
		})
	}
}

// A repository that armed the gate but has no span ledger wired cannot prove
// anything about any task, and an unprovable review chain is a block.
func TestReviewChainGateWithoutSpanStoreFailsClosed(t *testing.T) {
	svc := &Service{}
	err := svc.reviewChainGate(context.Background(),
		domain.Repository{RequireReviewChain: true},
		domain.BoardTask{ID: uuid.New(), TaskType: domain.TaskTypeTask},
		domain.TaskColumnInProgress, domain.TaskColumnDone)
	if !errors.Is(err, domain.ErrReviewChainIncomplete) {
		t.Fatalf("want ErrReviewChainIncomplete, got %v", err)
	}
}

// board_columns is user-editable. A stage whose column this board does not have
// cannot be reached by anybody, so requiring it would park every task instead of
// checking anything — the rest of the chain is still enforced.
func TestReviewChainGateSkipsStagesTheBoardDoesNotHave(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), RequireReviewChain: true}
	task := domain.BoardTask{ID: uuid.New(), Key: "APP-9", TaskType: domain.TaskTypeTask}

	// A board with no QA columns at all: code review and UAT still apply.
	noQA := &Service{
		spans:     visited(domain.TaskColumnCodeReview, domain.TaskColumnPMUAT),
		columns:   boardWith(domain.TaskColumnCodeReview, domain.TaskColumnPMUAT, domain.TaskColumnDone),
		workflows: workflowtest.Default().Reader(),
	}
	if err := noQA.reviewChainGate(context.Background(), repo, task, domain.TaskColumnPMUAT, domain.TaskColumnDone); err != nil {
		t.Fatalf("a board without in_qa must not be deadlocked by the QA stage: %v", err)
	}

	missingUAT := &Service{
		spans:     visited(domain.TaskColumnCodeReview),
		columns:   boardWith(domain.TaskColumnCodeReview, domain.TaskColumnPMUAT, domain.TaskColumnDone),
		workflows: workflowtest.Default().Reader(),
	}
	err := missingUAT.reviewChainGate(context.Background(), repo, task, domain.TaskColumnCodeReview, domain.TaskColumnDone)
	if !errors.Is(err, domain.ErrReviewChainIncomplete) {
		t.Fatalf("the stages the board DOES have must still be enforced, got %v", err)
	}
	if !strings.Contains(err.Error(), "pm_uat") {
		t.Fatalf("want pm_uat named, got: %v", err)
	}
}

// released means "live in production". The evidence is a successful deploy
// pipeline for this very task — not a state, and not a pipeline that ran nothing.
func TestReleaseDeployGate(t *testing.T) {
	cases := []struct {
		name     string
		taskType domain.TaskType
		runs     []domain.TaskPipeline
		listErr  error
		prodJobs *fakePipelineJobs
		target   domain.TaskColumn
		off      bool
		wantErr  error
		wantSaid []string
		noStore  bool
	}{
		{
			name:     "a successful prod deploy releases",
			taskType: domain.TaskTypeTask,
			runs:     []domain.TaskPipeline{deployRun(domain.PipelineTriggerProdDeploy, domain.PipelineStatusSuccess)},
			target:   domain.TaskColumnReleased,
		},
		{
			name:     "no deploy at all is refused",
			taskType: domain.TaskTypeTask,
			runs:     nil,
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReleaseNotDeployed,
			wantSaid: []string{"prod deploy finish"},
		},
		{
			name:     "a failed prod deploy is refused",
			taskType: domain.TaskTypeTask,
			runs:     []domain.TaskPipeline{deployRun(domain.PipelineTriggerProdDeploy, domain.PipelineStatusFailed)},
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReleaseNotDeployed,
		},
		{
			name:     "a skipped prod deploy ran nothing and is refused, with the reason",
			taskType: domain.TaskTypeTask,
			runs:     []domain.TaskPipeline{deployRun(domain.PipelineTriggerProdDeploy, domain.PipelineStatusSkipped)},
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReleaseNotDeployed,
			wantSaid: []string{"no prod deploy workflow is mapped"},
		},
		{
			name:     "a stage deploy is not a production deploy",
			taskType: domain.TaskTypeTask,
			runs:     []domain.TaskPipeline{deployRun(domain.PipelineTriggerStageDeploy, domain.PipelineStatusSuccess)},
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReleaseNotDeployed,
		},
		{
			name:     "preprod releases when the repo maps no prod workflow",
			taskType: domain.TaskTypeTask,
			runs:     []domain.TaskPipeline{deployRun(domain.PipelineTriggerPreProdDeploy, domain.PipelineStatusSuccess)},
			target:   domain.TaskColumnReleased,
		},
		{
			name:     "preprod alone is not enough when prod IS mapped",
			taskType: domain.TaskTypeTask,
			runs:     []domain.TaskPipeline{deployRun(domain.PipelineTriggerPreProdDeploy, domain.PipelineStatusSuccess)},
			prodJobs: mappedProdWorkflow(),
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReleaseNotDeployed,
		},
		{
			name:     "preprod then a successful prod deploy releases on a repo that maps prod",
			taskType: domain.TaskTypeTask,
			runs: []domain.TaskPipeline{
				deployRun(domain.PipelineTriggerPreProdDeploy, domain.PipelineStatusSuccess),
				deployRun(domain.PipelineTriggerProdDeploy, domain.PipelineStatusSuccess),
			},
			prodJobs: mappedProdWorkflow(),
			target:   domain.TaskColumnReleased,
		},
		{
			name:     "an analiz task ships no code and needs no deploy",
			taskType: domain.TaskTypeAnaliz,
			runs:     nil,
			target:   domain.TaskColumnReleased,
		},
		{
			name:     "entering done is not this gate's business",
			taskType: domain.TaskTypeTask,
			runs:     nil,
			target:   domain.TaskColumnDone,
		},
		{
			name:     "the gate is off unless the repository opted in",
			taskType: domain.TaskTypeTask,
			runs:     nil,
			target:   domain.TaskColumnReleased,
			off:      true,
		},
		{
			name:     "unreadable deploy history fails closed",
			taskType: domain.TaskTypeTask,
			listErr:  errors.New("boom: pool exhausted"),
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReleaseNotDeployed,
			wantSaid: []string{"could not be read"},
		},
		{
			name:     "no pipeline ledger fails closed",
			taskType: domain.TaskTypeTask,
			noStore:  true,
			target:   domain.TaskColumnReleased,
			wantErr:  domain.ErrReleaseNotDeployed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{workflows: workflowtest.Default().Reader()}
			if !tc.noStore {
				svc.pipelineStore = &fakeDeployPipelines{runs: tc.runs, err: tc.listErr}
			}
			if tc.prodJobs != nil {
				svc.pipelineJobs = tc.prodJobs
			}
			repo := domain.Repository{ID: uuid.New(), RequireReleaseDeploy: !tc.off}
			task := domain.BoardTask{ID: uuid.New(), Key: "APP-7", TaskType: tc.taskType}

			err := svc.releaseDeployGate(context.Background(), repo, task, tc.target)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("want the move allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
			for _, want := range tc.wantSaid {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("want %q named in the block, got: %v", want, err)
				}
			}
		})
	}
}

// The gates have to bite on the real move path, for every mover. An agent's
// move_task, a human's drag and the pipeline runner's own release all land in
// UpdateTask, and "done means reviewed" cannot depend on who typed it.
func TestUpdateTaskEnforcesLifecycleGatesForEveryActor(t *testing.T) {
	agentID := uuid.New()

	cases := []struct {
		name    string
		actor   domain.TaskActor
		from    domain.TaskColumn
		to      domain.TaskColumn
		spans   *fakeStageEvidence
		runs    []domain.TaskPipeline
		wantErr error
	}{
		{
			name:    "human cannot drag an unreviewed task to done",
			actor:   domain.TaskActorHuman,
			from:    domain.TaskColumnInProgress,
			to:      domain.TaskColumnDone,
			spans:   visited(domain.TaskColumnInProgress),
			wantErr: domain.ErrReviewChainIncomplete,
		},
		{
			name:    "agent cannot move an unreviewed task to done either",
			actor:   domain.TaskActorAgent,
			from:    domain.TaskColumnInProgress,
			to:      domain.TaskColumnDone,
			spans:   visited(domain.TaskColumnInProgress),
			wantErr: domain.ErrReviewChainIncomplete,
		},
		{
			name:  "a reviewed task reaches done",
			actor: domain.TaskActorHuman,
			from:  domain.TaskColumnHumanUAT,
			to:    domain.TaskColumnDone,
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA,
				domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT),
		},
		{
			name:  "done to released without a deploy is refused",
			actor: domain.TaskActorHuman,
			from:  domain.TaskColumnDone,
			to:    domain.TaskColumnReleased,
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA,
				domain.TaskColumnPMUAT, domain.TaskColumnDone),
			wantErr: domain.ErrReleaseNotDeployed,
		},
		{
			name:  "done to released after a successful prod deploy is allowed",
			actor: domain.TaskActorSystem, // the pipeline runner's own release move
			from:  domain.TaskColumnDone,
			to:    domain.TaskColumnReleased,
			spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA,
				domain.TaskColumnPMUAT, domain.TaskColumnDone),
			runs: []domain.TaskPipeline{deployRun(domain.PipelineTriggerProdDeploy, domain.PipelineStatusSuccess)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoID, taskID := uuid.New(), uuid.New()
			tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
				ID:              taskID,
				RepositoryID:    repoID,
				Key:             "APP-42",
				TaskType:        domain.TaskTypeTask,
				Column:          tc.from,
				AssigneeAgentID: &agentID,
			}}
			svc := &Service{
				repos: &fakeReleaseRepoStore{repo: domain.Repository{
					ID:                   repoID,
					RequireReviewChain:   true,
					RequireReleaseDeploy: true,
				}},
				tasks:         tasks,
				spans:         tc.spans,
				pipelineStore: &fakeDeployPipelines{runs: tc.runs},
				workflows:     workflowtest.Default().Reader(),
			}

			target := tc.to
			_, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
				Column:       &target,
				Actor:        tc.actor,
				ActorAgentID: &agentID,
			})

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
				if tasks.updated.Column == target {
					t.Fatalf("a blocked move must not be persisted, task landed in %s", tasks.updated.Column)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tasks.updated.Column != target {
				t.Fatalf("want the task moved to %s, got %s", target, tasks.updated.Column)
			}
		})
	}
}

// Nothing changes for a repository that never opted in: the same unreviewed
// move that is refused above is allowed here, which is what makes this shippable
// against boards that are mid-flight, have no QA agent, or use custom columns.
func TestUpdateTaskLeavesUnOptedRepositoriesAlone(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{
		ID: taskID, RepositoryID: repoID, TaskType: domain.TaskTypeTask, Column: domain.TaskColumnInProgress,
	}}
	svc := &Service{
		repos:         &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:         tasks,
		spans:         visited(domain.TaskColumnInProgress),
		pipelineStore: &fakeDeployPipelines{},
	}

	for _, target := range []domain.TaskColumn{domain.TaskColumnDone, domain.TaskColumnReleased} {
		col := target
		if _, err := svc.UpdateTask(context.Background(), repoID, taskID, domain.UpdateBoardTaskRequest{
			Column: &col,
			Actor:  domain.TaskActorHuman,
		}); err != nil {
			t.Fatalf("an un-opted repository must behave exactly as before, got %v moving to %s", err, target)
		}
	}
}
