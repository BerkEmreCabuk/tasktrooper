package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The three doc routes share a repository row and a board, so the fakes here
// are the smallest thing repodocs.Service consumes rather than a full
// port.RepositoryStore.

type fakeRepoDocsRepos struct {
	repo domain.Repository
}

func (f *fakeRepoDocsRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

func (f *fakeRepoDocsRepos) Update(_ context.Context, _ uuid.UUID, req domain.UpdateRepositoryRequest) (domain.Repository, error) {
	if req.Docs != nil {
		f.repo.Docs = *req.Docs
	}
	if req.SubProjects != nil {
		f.repo.SubProjects = *req.SubProjects
	}
	return f.repo, nil
}

func (f *fakeRepoDocsRepos) SetDocsTaskID(_ context.Context, _ uuid.UUID, taskID string) error {
	f.repo.DocsTaskID = taskID
	return nil
}

type fakeRepoDocsTasks struct {
	task domain.BoardTask
}

func (f *fakeRepoDocsTasks) CreateTask(_ context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.task = domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		Key:          "T-1",
		Title:        req.Title,
		Description:  req.Description,
		Column:       req.Column,
	}
	return f.task, nil
}

func (f *fakeRepoDocsTasks) GetTask(_ context.Context, _, taskID uuid.UUID) (domain.BoardTask, error) {
	return f.task, nil
}

type fakeRepoDocsMerger struct {
	merged bool
}

func (f *fakeRepoDocsMerger) MergeTaskPullRequest(_ context.Context, _, _ uuid.UUID) (domain.TaskPRMergeResult, error) {
	f.merged = true
	return domain.TaskPRMergeResult{Merged: true, PRNumber: 12, MergeCommitSHA: "abc1234", Message: "Merged pull request #12."}, nil
}

func newRepoDocsTestApp(t *testing.T) (*fiber.App, uuid.UUID, *fakeRepoDocsTasks, *fakeRepoDocsMerger) {
	t.Helper()
	repoID := uuid.New()
	repos := &fakeRepoDocsRepos{repo: domain.Repository{ID: repoID, Name: "app", Kind: domain.RepoKindBackend}}
	tasks := &fakeRepoDocsTasks{}
	merger := &fakeRepoDocsMerger{}

	svc := repodocs.NewService(repos)
	svc.SetTaskCreator(tasks)
	svc.SetTaskPRMerger(merger)

	h := &Handler{repoDocsSvc: svc}
	app := fiber.New()
	h.registerRepoDocsRoutes(app)
	return app, repoID, tasks, merger
}

func postJSON(t *testing.T, app *fiber.App, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	_, err = rec.Body.ReadFrom(resp.Body)
	require.NoError(t, err)
	return rec
}

func TestCreateRepoDocsBundleTaskReturnsTheTaskID(t *testing.T) {
	app, repoID, tasks, _ := newRepoDocsTestApp(t)
	rec := postJSON(t, app, "/v1/repositories/"+repoID.String()+"/docs/setup-task",
		`{"items":[{"kind":"architecture"},{"kind":"local_run","path":""}]}`)
	require.Equal(t, fiber.StatusCreated, rec.Code)

	var body struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, tasks.task.ID.String(), body.TaskID)
	require.Contains(t, tasks.task.Description, ".ai/architecture.md")
	require.Contains(t, tasks.task.Description, "scripts/dev.sh")
}

func TestCreateRepoDocsBundleTaskRejectsAnUnknownKind(t *testing.T) {
	app, repoID, _, _ := newRepoDocsTestApp(t)
	rec := postJSON(t, app, "/v1/repositories/"+repoID.String()+"/docs/setup-task", `{"items":[{"kind":"vibes"}]}`)
	require.Equal(t, fiber.StatusBadRequest, rec.Code)
}

// The per-kind route still works, and the static bundle route does not shadow
// it — they differ in their last segment, not only in the parameter.
func TestPerKindDocRouteStillWorks(t *testing.T) {
	app, repoID, tasks, _ := newRepoDocsTestApp(t)
	rec := postJSON(t, app, "/v1/repositories/"+repoID.String()+"/docs/coding_standards/setup-task", "")
	require.Equal(t, fiber.StatusCreated, rec.Code)
	require.Contains(t, tasks.task.Title, ".ai/coding-standards.md")
}

func TestGetRepoDocsTaskIsEmptyBeforeABundleAndFullAfter(t *testing.T) {
	app, repoID, tasks, _ := newRepoDocsTestApp(t)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/docs/task", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	var empty repodocs.DocsTaskStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&empty))
	require.Equal(t, repodocs.DocsTaskStatus{}, empty)

	require.Equal(t, fiber.StatusCreated, postJSON(t, app,
		"/v1/repositories/"+repoID.String()+"/docs/setup-task", `{"items":[{"kind":"architecture"}]}`).Code)
	tasks.task.PRURL = "https://github.com/acme/app/pull/12"
	tasks.task.PRNumber = 12
	tasks.task.Column = domain.TaskColumnDone

	resp, err = app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/docs/task", nil))
	require.NoError(t, err)
	var status repodocs.DocsTaskStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
	require.Equal(t, tasks.task.ID.String(), status.TaskID)
	require.Equal(t, "done", status.Column)
	require.Equal(t, "https://github.com/acme/app/pull/12", status.PRURL)
}

func TestMergeRepoDocsTask(t *testing.T) {
	app, repoID, _, merger := newRepoDocsTestApp(t)
	require.Equal(t, fiber.StatusCreated, postJSON(t, app,
		"/v1/repositories/"+repoID.String()+"/docs/setup-task", `{"items":[{"kind":"architecture"}]}`).Code)

	rec := postJSON(t, app, "/v1/repositories/"+repoID.String()+"/docs/task/merge", "")
	require.Equal(t, fiber.StatusOK, rec.Code)
	require.True(t, merger.merged)

	var result domain.TaskPRMergeResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.True(t, result.Merged)
	require.Equal(t, 12, result.PRNumber)

	// Cleared, so a second merge has nothing to act on.
	require.Equal(t, fiber.StatusBadRequest,
		postJSON(t, app, "/v1/repositories/"+repoID.String()+"/docs/task/merge", "").Code)
}

func TestRepoDocsRoutesRejectABadRepositoryID(t *testing.T) {
	app, _, _, _ := newRepoDocsTestApp(t)
	require.Equal(t, fiber.StatusBadRequest,
		postJSON(t, app, "/v1/repositories/not-a-uuid/docs/setup-task", `{"items":[{"kind":"architecture"}]}`).Code)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/not-a-uuid/docs/task", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}
