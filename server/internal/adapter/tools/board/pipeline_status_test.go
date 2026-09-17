package board

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeTaskManager implements TaskManager with only the pipeline-status paths
// wired up; the other methods are never exercised by this tool and just
// return zero values. taskRepoID is the repository the (single) task lives
// in: LatestTaskPipeline mimics repository.Service by rejecting any other
// repositoryID, and FindTaskRepositoryID returns it for the unscoped
// context fallback.
type fakeTaskManager struct {
	taskRepoID uuid.UUID
	pipeline   domain.TaskPipeline
	err        error
	gotRepoID  uuid.UUID
	// comments records what a tool wrote onto the card — the merge tool's
	// result note is the only record a human gets of what landed.
	comments  []domain.CreateTaskCommentRequest
	testCases []domain.TaskTestCase
	// criterionErr is returned by the three criterion-mutating methods below,
	// separate from err (LatestTaskPipeline's) so a test can fail one path
	// without also breaking pipeline-status coverage that shares this fake.
	criterionErr error
}

func (f *fakeTaskManager) ListTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeTaskManager) ListAllTasks(ctx context.Context) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeTaskManager) ListReadyTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeTaskManager) FindTaskRepositoryID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	return f.taskRepoID, nil
}

// GetTask mirrors repository.Service.GetTask, which reads through
// BoardTaskStore.Get: the row is scoped by repository, so the same task id
// asked for in any other repository comes back missing rather than readable.
// That is what makes it the membership check resolveTaskRepositoryID's fast
// path relies on.
func (f *fakeTaskManager) GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	if repositoryID == uuid.Nil || repositoryID != f.taskRepoID {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return domain.BoardTask{ID: taskID, RepositoryID: repositoryID}, nil
}
func (f *fakeTaskManager) DefaultRepositoryID(ctx context.Context) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (f *fakeTaskManager) CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeTaskManager) UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeTaskManager) DeleteTask(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	return nil
}
func (f *fakeTaskManager) ClaimTask(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeTaskManager) AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	f.comments = append(f.comments, req)
	return domain.TaskComment{}, nil
}
func (f *fakeTaskManager) AddDocument(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskDocumentRequest) (domain.TaskDocument, error) {
	return domain.TaskDocument{}, nil
}
func (f *fakeTaskManager) ListTaskCriteria(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	return nil, nil
}
func (f *fakeTaskManager) ReplaceAcceptanceCriteria(ctx context.Context, repositoryID, taskID uuid.UUID, items []domain.AcceptanceCriterionInput) ([]domain.AcceptanceCriterion, error) {
	return nil, nil
}
func (f *fakeTaskManager) SetTaskCriterionCompleted(ctx context.Context, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error) {
	if f.criterionErr != nil {
		return domain.AcceptanceCriterion{}, f.criterionErr
	}
	return domain.AcceptanceCriterion{}, nil
}
func (f *fakeTaskManager) SetTaskCriterionCanceled(ctx context.Context, criterionID uuid.UUID, canceled bool, reason, authorType, authorID string) (domain.AcceptanceCriterion, error) {
	if f.criterionErr != nil {
		return domain.AcceptanceCriterion{}, f.criterionErr
	}
	return domain.AcceptanceCriterion{ID: criterionID, Canceled: canceled, CancelReason: reason}, nil
}
func (f *fakeTaskManager) ReviewTaskCriterion(ctx context.Context, criterionID, agentID uuid.UUID, approved bool, note string) (domain.CriterionCheck, error) {
	if f.criterionErr != nil {
		return domain.CriterionCheck{}, f.criterionErr
	}
	return domain.CriterionCheck{}, nil
}
func (f *fakeTaskManager) ListTestCases(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error) {
	return f.testCases, nil
}
func (f *fakeTaskManager) RecordTestCases(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	for _, item := range items {
		f.testCases = append(f.testCases, domain.TaskTestCase{
			TaskID: taskID, Title: item.Title, Category: item.Category, Status: item.Status,
			Expected: item.Expected, Actual: item.Actual, Evidence: item.Evidence, Notes: item.Notes,
		})
	}
	return f.testCases, nil
}
func (f *fakeTaskManager) SetTestCaseResult(ctx context.Context, testCaseID uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error) {
	return domain.TaskTestCase{ID: testCaseID, Status: item.Status, Actual: item.Actual, Evidence: item.Evidence, Notes: item.Notes}, nil
}
func (f *fakeTaskManager) LatestTaskPipeline(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPipeline, error) {
	f.gotRepoID = repositoryID
	if repositoryID != f.taskRepoID {
		// Mirrors repository.Service.LatestTaskPipeline: s.tasks.Get with the
		// wrong repository fails before any pipeline data is touched.
		return domain.TaskPipeline{}, errors.New("task not found")
	}
	return f.pipeline, f.err
}

func (f *fakeTaskManager) TriggerRelease(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPipeline, error) {
	return domain.TaskPipeline{}, nil
}

func TestGetPipelineStatusTool_Success(t *testing.T) {
	longOutput := strings.Repeat("x", 5000) + "TAIL_MARKER"
	exitCode := 1
	created := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	repoID := uuid.New()
	fake := &fakeTaskManager{
		taskRepoID: repoID,
		pipeline: domain.TaskPipeline{
			Status:    domain.PipelineStatusFailed,
			Trigger:   domain.PipelineTriggerReadyForQA,
			CreatedAt: created,
			Note:      "",
			Jobs: []domain.TaskPipelineJob{
				{Name: "build", Status: domain.PipelineJobStatusSuccess, ExitCode: intPtr(0)},
				{Name: "test", Status: domain.PipelineJobStatusFailed, ExitCode: &exitCode, Output: longOutput},
			},
		},
	}
	tool := newGetPipelineStatusTool(&ToolKit{Tasks: fake})

	result := tool.Execute(context.Background(), `{"task_id":"`+uuid.New().String()+`"}`)
	if result.IsError {
		t.Fatalf("expected no error, got: %s", result.Content)
	}

	var payload struct {
		Status    string `json:"status"`
		Trigger   string `json:"trigger"`
		CreatedAt string `json:"created_at"`
		Note      string `json:"note"`
		Jobs      []struct {
			Name     string `json:"name"`
			Status   string `json:"status"`
			ExitCode *int   `json:"exit_code"`
			Output   string `json:"output"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if payload.Status != "failed" || payload.Trigger != "ready_for_qa" {
		t.Fatalf("unexpected status/trigger: %+v", payload)
	}
	if len(payload.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(payload.Jobs))
	}
	if payload.Jobs[0].Output != "" {
		t.Fatalf("expected no output for successful job, got %q", payload.Jobs[0].Output)
	}
	if len(payload.Jobs[1].Output) != 4000 {
		t.Fatalf("expected failed job output tail-truncated to 4000 chars, got %d", len(payload.Jobs[1].Output))
	}
	if !strings.HasSuffix(payload.Jobs[1].Output, "TAIL_MARKER") {
		t.Fatalf("expected truncated output to keep the tail, got suffix %q", payload.Jobs[1].Output[len(payload.Jobs[1].Output)-20:])
	}
	if fake.gotRepoID != repoID {
		t.Fatalf("expected resolved repository %s to be threaded into LatestTaskPipeline, got %s", repoID, fake.gotRepoID)
	}
}

// A task_id from another repository must not reach that repository's build
// logs: the run is contained to the repository it is bound to, and a planted
// task naming someone else's pipeline is exactly the prompt injection this
// boundary exists for. The refusal now says so instead of answering "board
// task not found", but it is still a refusal — the tool never fetches.
func TestGetPipelineStatusTool_CrossRepoTaskDenied(t *testing.T) {
	secret := "BUILD_LOG_FROM_THE_OTHER_REPO"
	contextRepo := uuid.New() // repository the agent is bound to
	otherRepo := uuid.New()   // repository the planted task lives in
	fake := &fakeTaskManager{
		taskRepoID: otherRepo,
		pipeline: domain.TaskPipeline{
			Status: domain.PipelineStatusFailed,
			Jobs: []domain.TaskPipelineJob{
				{Name: "test", Status: domain.PipelineJobStatusFailed, Output: secret},
			},
		},
	}
	tool := newGetPipelineStatusTool(&ToolKit{Tasks: fake})

	ctx := registry.ContextWithRepositoryID(context.Background(), contextRepo)
	result := tool.Execute(ctx, `{"task_id":"`+uuid.New().String()+`"}`)

	if strings.Contains(result.Content, secret) {
		t.Fatalf("expected the other repository's pipeline to stay hidden, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "different repository") && !strings.Contains(result.Content, "not found") {
		t.Fatalf("expected a refusal naming the repository mismatch, got: %s", result.Content)
	}
	// The refusal happens before the fetch: nothing is read, in either
	// repository. gotRepoID is only ever written by LatestTaskPipeline, so its
	// staying zero is the assertion that the tool never got that far.
	if fake.gotRepoID != uuid.Nil {
		t.Fatalf("expected the refusal before any pipeline fetch, but repository %s was read", fake.gotRepoID)
	}
}

func TestGetPipelineStatusTool_NoPipeline(t *testing.T) {
	fake := &fakeTaskManager{err: domain.ErrPipelineNotFound}
	tool := newGetPipelineStatusTool(&ToolKit{Tasks: fake})

	result := tool.Execute(context.Background(), `{"task_id":"`+uuid.New().String()+`"}`)
	if result.IsError {
		t.Fatalf("expected no-pipeline case to not be a tool error, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "no pipeline for this task") {
		t.Fatalf("expected 'no pipeline for this task' message, got: %s", result.Content)
	}
}

func TestGetPipelineStatusTool_StoreError(t *testing.T) {
	fake := &fakeTaskManager{err: errors.New("boom")}
	tool := newGetPipelineStatusTool(&ToolKit{Tasks: fake})

	result := tool.Execute(context.Background(), `{"task_id":"`+uuid.New().String()+`"}`)
	if !result.IsError {
		t.Fatalf("expected a tool error for a generic store failure")
	}
}

func TestGetPipelineStatusTool_InvalidTaskID(t *testing.T) {
	fake := &fakeTaskManager{}
	tool := newGetPipelineStatusTool(&ToolKit{Tasks: fake})

	result := tool.Execute(context.Background(), `{"task_id":"not-a-uuid"}`)
	if !result.IsError {
		t.Fatalf("expected invalid task_id to be a tool error")
	}
}

func TestGetPipelineStatusTool_RegisteredInExecutors(t *testing.T) {
	fake := &fakeTaskManager{}
	execs := NewExecutors(&ToolKit{Tasks: fake})
	found := false
	for _, e := range execs {
		if e.Name() == getPipelineStatusToolName {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q to be registered in NewExecutors", getPipelineStatusToolName)
	}
}

func intPtr(v int) *int { return &v }
