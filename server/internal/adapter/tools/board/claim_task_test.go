package board

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// claimingTaskManager serves one task by key and fails every claim with err —
// the shape the store produces when the claim update matched no row.
type claimingTaskManager struct {
	*fakeTaskManager
	task domain.BoardTask
	err  error
}

func (c *claimingTaskManager) ListTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	return []domain.BoardTask{c.task}, nil
}

func (c *claimingTaskManager) ListAllTasks(ctx context.Context) ([]domain.BoardTask, error) {
	return []domain.BoardTask{c.task}, nil
}

func (c *claimingTaskManager) ClaimTask(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) (domain.BoardTask, error) {
	if c.err != nil {
		return domain.BoardTask{}, c.err
	}
	claimed := c.task
	claimed.AssigneeAgentID = &agentID
	return claimed, nil
}

func newClaimKit(err error) (*claimTaskTool, context.Context) {
	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-3", Title: "Push notifications"}
	mgr := &claimingTaskManager{fakeTaskManager: &fakeTaskManager{taskRepoID: repoID}, task: task, err: err}
	ctx := registry.ContextWithAgentID(context.Background(), uuid.New())
	return &claimTaskTool{kit: &ToolKit{Tasks: mgr}}, ctx
}

// A claim the store refuses because somebody else holds the task used to come
// back as "claim assignee: no rows in result set" — a database sentence with no
// task in it and no next step, which the run answered by claiming again. The
// refusal now names the task and the rule.
func TestClaimTaskNamesTheAgentHoldingIt(t *testing.T) {
	tool, ctx := newClaimKit(fmt.Errorf("claim assignee: %w: T-3", domain.ErrTaskAlreadyClaimed))

	res := tool.Execute(ctx, `{"task_id":"T-3"}`)

	require.True(t, res.IsError, res.Content)
	assert.Equal(t, "task T-3 is already assigned to another agent; only unassigned tasks or tasks assigned to you can be claimed", res.Content)
	assert.NotContains(t, res.Content, "no rows")
}

// The other half of the same "no rows": the task is not in this repository at
// all. Told apart, because retrying is pointless here and looking the task up
// is the only thing that helps.
func TestClaimTaskSaysWhenTheTaskIsNotInThisRepository(t *testing.T) {
	tool, ctx := newClaimKit(fmt.Errorf("claim assignee: %w: %s", domain.ErrBoardTaskNotFound, uuid.New()))

	res := tool.Execute(ctx, `{"task_id":"T-3"}`)

	require.True(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "task T-3 was not found in this repository")
	assert.Contains(t, res.Content, "list_board_tasks")
}

// Anything else is passed through untouched: mapping must not swallow the
// errors it was not written for.
func TestClaimTaskPassesThroughUnrecognisedErrors(t *testing.T) {
	tool, ctx := newClaimKit(fmt.Errorf("connection refused"))

	res := tool.Execute(ctx, `{"task_id":"T-3"}`)

	require.True(t, res.IsError, res.Content)
	assert.Equal(t, "connection refused", res.Content)
}

func TestClaimTaskSucceedsByBoardKey(t *testing.T) {
	tool, ctx := newClaimKit(nil)

	res := tool.Execute(ctx, `{"task_id":"T-3"}`)

	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "T-3")
}
