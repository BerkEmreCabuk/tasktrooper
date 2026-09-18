package repository

// The agent a card is given to, through the service that writes it: the
// omitted / null / value contract the field carries on update.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// assigneeTaskStore is fakePackageTaskStore with a Create that actually
// persists — the shared fake's returns a zero task, which would hide exactly
// the field under test.
type assigneeTaskStore struct {
	*fakePackageTaskStore
}

func (a *assigneeTaskStore) Create(_ context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	task.ID = uuid.New()
	task.Key = domain.FormatTaskKey(task.TaskType, task.TaskNumber)
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

// An omitted key leaves the agent on the card alone, which is why a PATCH that
// only renames or moves a card cannot unassign it as a side effect.
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

// `null` is the spelling a client with an object reference in hand reaches for
// to say "no reference", and until domain.Nullable it decoded to the same nil
// pointer as an omitted key — so it was a clear that silently did nothing.
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

// The wire, not the Go struct: this is the decode that used to lose the
// difference, so it is the one worth pinning.
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
