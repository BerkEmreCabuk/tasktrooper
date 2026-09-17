package board

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// readyTaskManager records the repositoryID ListReadyTasks was called with and
// returns a scripted list, standing in for repository.Service.ListReadyTasks.
type readyTaskManager struct {
	*fakeTaskManager
	tasks        []domain.BoardTask
	gotRepoID    uuid.UUID
	calledWithID bool
}

func (r *readyTaskManager) ListReadyTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	r.gotRepoID = repositoryID
	r.calledWithID = true
	return r.tasks, nil
}

func newReadyKit(tasks ...domain.BoardTask) (*listReadyTasksTool, *readyTaskManager) {
	mgr := &readyTaskManager{fakeTaskManager: &fakeTaskManager{}, tasks: tasks}
	return &listReadyTasksTool{kit: &ToolKit{Tasks: mgr}}, mgr
}

func decodeReadyResult(t *testing.T, res domain.ToolResult) ([]domain.BoardTask, int) {
	t.Helper()
	var payload struct {
		Tasks []domain.BoardTask `json:"tasks"`
		Count int                `json:"count"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &payload))
	return payload.Tasks, payload.Count
}

func TestListReadyTasksDefaultReturnsAllWithCount(t *testing.T) {
	tool, _ := newReadyKit(
		domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnBacklog},
		domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnTodo},
	)

	res := tool.Execute(context.Background(), "")

	require.False(t, res.IsError, res.Content)
	tasks, count := decodeReadyResult(t, res)
	assert.Len(t, tasks, 2)
	assert.Equal(t, 2, count)
}

func TestListReadyTasksEmptyResultIsNotNull(t *testing.T) {
	tool, _ := newReadyKit()

	res := tool.Execute(context.Background(), "")

	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, `"tasks":[]`)
	tasks, count := decodeReadyResult(t, res)
	assert.Empty(t, tasks)
	assert.Equal(t, 0, count)
}

func TestListReadyTasksFiltersByColumn(t *testing.T) {
	tool, _ := newReadyKit(
		domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnBacklog},
		domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnTodo},
	)

	res := tool.Execute(context.Background(), `{"column":"todo"}`)

	require.False(t, res.IsError, res.Content)
	tasks, count := decodeReadyResult(t, res)
	require.Len(t, tasks, 1)
	assert.Equal(t, 1, count)
	assert.Equal(t, "T-2", tasks[0].Key)
}

func TestListReadyTasksInvalidColumnIsAnError(t *testing.T) {
	tool, _ := newReadyKit()

	res := tool.Execute(context.Background(), `{"column":"blocked"}`)

	require.True(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "invalid column")
}

func TestListReadyTasksAssignedToMeFiltersByAgent(t *testing.T) {
	me := uuid.New()
	other := uuid.New()
	tool, _ := newReadyKit(
		domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnTodo, AssigneeAgentID: &me},
		domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnTodo, AssigneeAgentID: &other},
		domain.BoardTask{ID: uuid.New(), Key: "T-3", Column: domain.TaskColumnTodo},
	)
	ctx := registry.ContextWithAgentID(context.Background(), me)

	res := tool.Execute(ctx, `{"assigned_to_me":true}`)

	require.False(t, res.IsError, res.Content)
	tasks, count := decodeReadyResult(t, res)
	require.Len(t, tasks, 1)
	assert.Equal(t, 1, count)
	assert.Equal(t, "T-1", tasks[0].Key)
}

func TestListReadyTasksAssignedToMeWithoutAgentIdentityIsAnError(t *testing.T) {
	tool, _ := newReadyKit(domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnTodo})

	res := tool.Execute(context.Background(), `{"assigned_to_me":true}`)

	require.True(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "no agent identity")
}

func TestListReadyTasksForwardsRepositoryIDFromContext(t *testing.T) {
	repoID := uuid.New()
	tool, mgr := newReadyKit()
	ctx := registry.ContextWithRepositoryID(context.Background(), repoID)

	res := tool.Execute(ctx, "")

	require.False(t, res.IsError, res.Content)
	assert.True(t, mgr.calledWithID)
	assert.Equal(t, repoID, mgr.gotRepoID)
}

func TestListReadyTasksWithNoRepositoryInContextAsksForAllRepositories(t *testing.T) {
	tool, mgr := newReadyKit()

	res := tool.Execute(context.Background(), "")

	require.False(t, res.IsError, res.Content)
	assert.True(t, mgr.calledWithID)
	assert.Equal(t, uuid.Nil, mgr.gotRepoID)
}
