package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/taskkey"
)

type assigneeTaskStore struct {
	*fakePackageTaskStore
}

func (a *assigneeTaskStore) Create(_ context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	task.ID = uuid.New()
	prefix := "T"
	if wf, ok := workflowtest.Default().Workflows[task.TaskType]; ok {
		prefix = wf.Type.KeyPrefix
	}
	task.Key = taskkey.FormatTaskKey(prefix, task.TaskNumber)
	a.tasks[task.ID] = task
	return task, nil
}

type assigneeFixture struct {
	svc    *Service
	repoID uuid.UUID
	tasks  *assigneeTaskStore
}

func newAssigneeFixture() *assigneeFixture {
	repoID := uuid.New()
	tasks := &assigneeTaskStore{fakePackageTaskStore: &fakePackageTaskStore{tasks: map[uuid.UUID]domain.BoardTask{}}}
	fx := workflowtest.Default()
	svc := &Service{
		repos:     &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:     tasks,
		workflows: fx.Reader(),
		roles:     fx.Resolver(),
	}
	return &assigneeFixture{svc: svc, repoID: repoID, tasks: tasks}
}

func (f *assigneeFixture) create(t *testing.T, req domain.CreateBoardTaskRequest) domain.BoardTask {
	t.Helper()
	if req.Title == "" {
		req.Title = "run something on a Mac"
	}
	task, err := f.svc.CreateTask(context.Background(), f.repoID, req)
	require.NoError(t, err)
	return task
}

func TestUpdateTaskAppliesTheAgent(t *testing.T) {
	f := newAssigneeFixture()
	task := f.create(t, domain.CreateBoardTaskRequest{})

	agentID := uuid.New()
	updated, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeAgentID: domain.SetNullable(agentID),
	})

	require.NoError(t, err)
	require.NotNil(t, updated.AssigneeAgentID)
	assert.Equal(t, agentID, *updated.AssigneeAgentID)
}

func TestUpdateTaskLeavesTheAgentAloneWhenOmitted(t *testing.T) {
	f := newAssigneeFixture()
	agentID := uuid.New()
	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeAgentID: &agentID})

	title := "renamed"
	updated, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		Title: &title,
	})

	require.NoError(t, err)
	require.NotNil(t, updated.AssigneeAgentID)
	assert.Equal(t, agentID, *updated.AssigneeAgentID)
}

func TestUpdateTaskClearsTheAgentWithNull(t *testing.T) {
	f := newAssigneeFixture()
	agentID := uuid.New()
	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeAgentID: &agentID})
	require.NotNil(t, f.tasks.tasks[task.ID].AssigneeAgentID, "the fixture starts with an agent on the card")

	updated, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeAgentID: domain.ClearNullable[uuid.UUID](),
	})

	require.NoError(t, err)
	assert.Nil(t, updated.AssigneeAgentID, "null unassigns the agent")
	assert.Nil(t, f.tasks.tasks[task.ID].AssigneeAgentID)
}

func TestUpdateRequestTellsNullApartFromAnOmittedAssignee(t *testing.T) {
	var omitted domain.UpdateBoardTaskRequest
	require.NoError(t, json.Unmarshal([]byte(`{"title":"x"}`), &omitted))
	assert.False(t, omitted.AssigneeAgentID.Present, "an omitted key leaves the agent alone")

	var cleared domain.UpdateBoardTaskRequest
	require.NoError(t, json.Unmarshal([]byte(`{"assignee_agent_id":null}`), &cleared))
	assert.True(t, cleared.AssigneeAgentID.Present)
	assert.Nil(t, cleared.AssigneeAgentID.Value)

	agentID := uuid.New()
	var set domain.UpdateBoardTaskRequest
	require.NoError(t, json.Unmarshal([]byte(`{"assignee_agent_id":"`+agentID.String()+`"}`), &set))
	require.NotNil(t, set.AssigneeAgentID.Value)
	assert.Equal(t, agentID, *set.AssigneeAgentID.Value)
}
