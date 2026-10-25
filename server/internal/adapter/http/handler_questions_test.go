package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type qTaskStore struct {
	port.BoardTaskStore
	mu    sync.Mutex
	tasks map[uuid.UUID]domain.BoardTask
}

func (f *qTaskStore) Get(_ context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[taskID]
	if !ok || t.RepositoryID != repositoryID {
		return domain.BoardTask{}, fmt.Errorf("%w: %s", domain.ErrBoardTaskNotFound, taskID)
	}
	return t, nil
}

func (f *qTaskStore) ReleaseAnalysisQuestionsBlock(_ context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[taskID]
	if !ok || task.BlockedResource != domain.ResourceAnalysisQuestions {
		return domain.BoardTask{}, false, nil
	}
	target := task.BlockedOriginColumn
	if target == "" {
		target = domain.TaskColumnInProgress
	}
	task.Column = target
	task.BlockedResource = ""
	task.BlockedOriginColumn = ""
	f.tasks[taskID] = task
	return task, true, nil
}

type qStore struct {
	mu    sync.Mutex
	items map[uuid.UUID]domain.TaskQuestion
}

func (f *qStore) Create(_ context.Context, q domain.TaskQuestion) (domain.TaskQuestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, existing := range f.items {
		if existing.TaskID == q.TaskID {
			n++
		}
	}
	q.ID = uuid.New()
	q.Key = fmt.Sprintf("Q%d", n+1)
	if q.Status == "" {
		q.Status = domain.QuestionStatusOpen
	}
	f.items[q.ID] = q
	return q, nil
}

func (f *qStore) Get(_ context.Context, taskID, id uuid.UUID) (domain.TaskQuestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.items[id]
	if !ok || q.TaskID != taskID {
		return domain.TaskQuestion{}, fmt.Errorf("%w: %s", domain.ErrQuestionNotFound, id)
	}
	return q, nil
}

func (f *qStore) GetByKey(_ context.Context, taskID uuid.UUID, key string) (domain.TaskQuestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, q := range f.items {
		if q.TaskID == taskID && q.Key == key {
			return q, nil
		}
	}
	return domain.TaskQuestion{}, domain.ErrQuestionNotFound
}

func (f *qStore) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskQuestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.TaskQuestion{}
	for _, q := range f.items {
		if q.TaskID == taskID {
			out = append(out, q)
		}
	}
	return out, nil
}

func (f *qStore) Update(_ context.Context, q domain.TaskQuestion) (domain.TaskQuestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[q.ID]; !ok {
		return domain.TaskQuestion{}, domain.ErrQuestionNotFound
	}
	f.items[q.ID] = q
	return q, nil
}

func (f *qStore) MarkSubmitted(_ context.Context, taskID uuid.UUID, at time.Time) ([]domain.TaskQuestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.TaskQuestion
	for id, q := range f.items {
		if q.TaskID != taskID || q.Status != domain.QuestionStatusAnswered || q.SubmittedAt != nil {
			continue
		}
		q.SubmittedAt = &at
		f.items[id] = q
		out = append(out, q)
	}
	return out, nil
}

type qFixture struct {
	app    *fiber.App
	repoID uuid.UUID
	taskID uuid.UUID
	tasks  *qTaskStore
	qs     *qStore
}

func newQuestionTestApp(t *testing.T, task domain.BoardTask) *qFixture {
	t.Helper()
	repoID, taskID := task.RepositoryID, task.ID
	f := &qFixture{
		repoID: repoID, taskID: taskID,
		tasks: &qTaskStore{tasks: map[uuid.UUID]domain.BoardTask{taskID: task}},
		qs:    &qStore{items: map[uuid.UUID]domain.TaskQuestion{}},
	}
	repos := newFakeDeployOpsRepositoryStore([]domain.Repository{{ID: repoID, Name: "tasktrooper"}})
	svc := repository.NewService(repos, f.tasks, nil, nil, nil, &annCommentStore{}, nil, nil, nil, nil)
	svc.SetQuestionStore(f.qs)
	h := &Handler{repositorySvc: svc}
	f.app = fiber.New()
	h.registerRepositoryRoutes(f.app)
	return f
}

func (f *qFixture) base() string {
	return "/v1/repositories/" + f.repoID.String() + "/tasks/" + f.taskID.String()
}

func (f *qFixture) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.app.Test(req, 5000)
	require.NoError(t, err)
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

func analizReviewTask(repoID, taskID uuid.UUID) domain.BoardTask {
	return domain.BoardTask{ID: taskID, RepositoryID: repoID, Key: "A-7", TaskType: "analiz", Column: domain.TaskColumnAnalizReview}
}

func TestListTaskQuestionsOrderedByKey(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	f := newQuestionTestApp(t, analizReviewTask(repoID, taskID))
	for i := 0; i < 2; i++ {
		_, err := f.qs.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "q", Kind: domain.QuestionKindTechnical, RecommendedAnswer: "a"})
		require.NoError(t, err)
	}

	status, raw := f.do(t, "GET", f.base()+"/questions", nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var body struct {
		Questions []domain.TaskQuestion `json:"questions"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Len(t, body.Questions, 2)
}

func TestListTaskQuestionsUnknownTask(t *testing.T) {
	f := newQuestionTestApp(t, analizReviewTask(uuid.New(), uuid.New()))
	status, _ := f.do(t, "GET", "/v1/repositories/"+f.repoID.String()+"/tasks/"+uuid.NewString()+"/questions", nil)
	assert.Equal(t, fiber.StatusNotFound, status)
}

func TestUpdateTaskQuestionColumnGuard(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	f := newQuestionTestApp(t, domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnInProgress})
	q, err := f.qs.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "q", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)

	status, raw := f.do(t, "PATCH", f.base()+"/questions/"+q.ID.String(), map[string]string{"answer": "an answer"})
	assert.Equal(t, fiber.StatusConflict, status, string(raw))
}

func TestUpdateTaskQuestionAnswerSavesAndEnforcesLimit(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	f := newQuestionTestApp(t, analizReviewTask(repoID, taskID))
	q, err := f.qs.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)

	status, raw := f.do(t, "PATCH", f.base()+"/questions/"+q.ID.String(), map[string]string{"answer": "  Use SQS.  "})
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var body struct {
		Question domain.TaskQuestion `json:"question"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.Equal(t, "Use SQS.", body.Question.Answer)
	assert.Equal(t, domain.QuestionStatusAnswered, body.Question.Status)

	tooLong := map[string]string{"answer": stringsRepeat("x", domain.MaxQuestionAnswerChars+1)}
	status, raw = f.do(t, "PATCH", f.base()+"/questions/"+q.ID.String(), tooLong)
	assert.Equal(t, fiber.StatusBadRequest, status, string(raw))
}

func TestUpdateTaskQuestionRefusesWithdrawn(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	f := newQuestionTestApp(t, analizReviewTask(repoID, taskID))
	q, err := f.qs.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "x", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)
	q.Status = domain.QuestionStatusWithdrawn
	_, err = f.qs.Update(context.Background(), q)
	require.NoError(t, err)

	status, raw := f.do(t, "PATCH", f.base()+"/questions/"+q.ID.String(), map[string]string{"answer": "x"})
	assert.Equal(t, fiber.StatusConflict, status, string(raw))
}

func TestSubmitTaskQuestionsRefusesPendingBlocking422(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnBlocked,
		BlockedResource: domain.ResourceAnalysisQuestions, BlockedOriginColumn: domain.TaskColumnInProgress}
	f := newQuestionTestApp(t, task)
	_, err := f.qs.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)

	status, raw := f.do(t, "POST", f.base()+"/questions/submit", nil)
	assert.Equal(t, fiber.StatusUnprocessableEntity, status, string(raw))
}

func TestSubmitTaskQuestionsHappyPath(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnBlocked,
		BlockedResource: domain.ResourceAnalysisQuestions, BlockedOriginColumn: domain.TaskColumnNeedRevision}
	f := newQuestionTestApp(t, task)
	q, err := f.qs.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)
	status, raw := f.do(t, "PATCH", f.base()+"/questions/"+q.ID.String(), map[string]string{"answer": "Use SQS."})
	require.Equal(t, fiber.StatusOK, status, string(raw))

	status, raw = f.do(t, "POST", f.base()+"/questions/submit", nil)
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var body struct {
		Submitted int              `json:"submitted"`
		Task      domain.BoardTask `json:"task"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.Equal(t, 1, body.Submitted)
	assert.Equal(t, domain.TaskColumnNeedRevision, body.Task.Column)
}

func TestSubmitTaskQuestionsColumnGuard(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	f := newQuestionTestApp(t, analizReviewTask(repoID, taskID))
	status, raw := f.do(t, "POST", f.base()+"/questions/submit", nil)
	assert.Equal(t, fiber.StatusConflict, status, string(raw))
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
