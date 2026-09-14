package repository

// The person a card belongs to, through the service that writes it.
//
// These live one layer ABOVE the store on purpose. The store column, its index
// and its INSERT were all correct while every task created or patched through
// the API came back with an empty assignee_user_id, because the service never
// copied the field off the request — so a store test passed on a board where no
// task could ever be given to anybody, and the runner refused every run with
// "no assignee, so there is no Mac to run it on".

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func newAssigneeFixture(members ...string) *assigneeFixture {
	repoID := uuid.New()
	tasks := &assigneeTaskStore{fakePackageTaskStore: &fakePackageTaskStore{tasks: map[uuid.UUID]domain.BoardTask{}}}
	svc := &Service{
		repos: &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks: tasks,
	}
	if members != nil {
		svc.SetMemberLister(func(context.Context) ([]string, error) { return members, nil })
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

// ---------------------------------------------------------------- create

func TestCreateTaskStoresTheAssignee(t *testing.T) {
	f := newAssigneeFixture("uid-ayse", "uid-mehmet")

	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeUserID: "uid-ayse"})

	assert.Equal(t, "uid-ayse", task.AssigneeUserID, "the response carries the assignee")
	assert.Equal(t, "uid-ayse", f.tasks.tasks[task.ID].AssigneeUserID, "and so does the row that was written")
}

func TestCreateTaskWithoutAnAssigneeLeavesItEmpty(t *testing.T) {
	f := newAssigneeFixture("uid-ayse")

	task := f.create(t, domain.CreateBoardTaskRequest{})

	assert.Empty(t, task.AssigneeUserID)
}

func TestCreateTaskRefusesANonMemberAssignee(t *testing.T) {
	f := newAssigneeFixture("uid-ayse")

	_, err := f.svc.CreateTask(context.Background(), f.repoID, domain.CreateBoardTaskRequest{
		Title:          "run something on a Mac",
		AssigneeUserID: "uid-stranger",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "uid-stranger", "the refusal names the uid it could not place")
	assert.Contains(t, err.Error(), "not a member of this workspace")
	assert.Empty(t, f.tasks.tasks, "and nothing was written")
}

// Self-hosted, the desktop bundle and every deployment with no roster table:
// there is nobody to check against, so an assignee is taken as given rather
// than refused for failing a lookup that cannot be made.
func TestCreateTaskAcceptsAnyAssigneeWithNoRoster(t *testing.T) {
	f := newAssigneeFixture()

	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeUserID: "uid-anybody"})

	assert.Equal(t, "uid-anybody", task.AssigneeUserID)
}

// ---------------------------------------------------------------- update

func TestUpdateTaskAppliesTheAssignee(t *testing.T) {
	f := newAssigneeFixture("uid-ayse", "uid-mehmet")
	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeUserID: "uid-ayse"})

	updated, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeUserID: domain.SetNullable("uid-mehmet"),
	})

	require.NoError(t, err)
	assert.Equal(t, "uid-mehmet", updated.AssigneeUserID)
	assert.Equal(t, "uid-mehmet", f.tasks.tasks[task.ID].AssigneeUserID)
}

// The reported symptom: assignee_agent_id in the very same PATCH was applied
// while assignee_user_id was dropped.
func TestUpdateTaskAppliesBothAssigneesFromOneRequest(t *testing.T) {
	f := newAssigneeFixture("uid-ayse")
	task := f.create(t, domain.CreateBoardTaskRequest{})

	agentID := uuid.New()
	updated, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeAgentID: domain.SetNullable(agentID),
		AssigneeUserID:  domain.SetNullable("uid-ayse"),
	})

	require.NoError(t, err)
	require.NotNil(t, updated.AssigneeAgentID)
	assert.Equal(t, agentID, *updated.AssigneeAgentID)
	assert.Equal(t, "uid-ayse", updated.AssigneeUserID)
}

// nil is "leave the person on the card alone" — the optional-pointer contract
// the field's own doc states, and the reason a PATCH that only moves a card
// cannot unassign it as a side effect.
func TestUpdateTaskLeavesTheAssigneeAloneWhenOmitted(t *testing.T) {
	f := newAssigneeFixture("uid-ayse")
	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeUserID: "uid-ayse"})

	title := "renamed"
	updated, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		Title: &title,
	})

	require.NoError(t, err)
	assert.Equal(t, "uid-ayse", updated.AssigneeUserID)
}

// The other half of that contract: a pointer to "" unassigns, and must not be
// mistaken for a non-member and refused.
func TestUpdateTaskClearsTheAssigneeWithAnEmptyString(t *testing.T) {
	f := newAssigneeFixture("uid-ayse")
	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeUserID: "uid-ayse"})

	updated, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeUserID: domain.SetNullable(""),
	})

	require.NoError(t, err)
	assert.Empty(t, updated.AssigneeUserID)
	assert.Empty(t, f.tasks.tasks[task.ID].AssigneeUserID)
}

func TestUpdateTaskRefusesANonMemberAssignee(t *testing.T) {
	f := newAssigneeFixture("uid-ayse")
	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeUserID: "uid-ayse"})

	_, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeUserID: domain.SetNullable("uid-stranger"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a member of this workspace")
	assert.Equal(t, "uid-ayse", f.tasks.tasks[task.ID].AssigneeUserID, "the previous assignee survives a refusal")
}

// `null` is the spelling a client with an object reference in hand reaches for
// to say "no reference", and until domain.Nullable it decoded to the same nil
// pointer as an omitted key — so it was a clear that silently did nothing.
// Both assignee fields read it the same way, because a client holding one is
// holding the other.
func TestUpdateTaskClearsBothAssigneesWithNull(t *testing.T) {
	f := newAssigneeFixture("uid-ayse")
	agentID := uuid.New()
	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeUserID: "uid-ayse", AssigneeAgentID: &agentID})
	require.NotNil(t, f.tasks.tasks[task.ID].AssigneeAgentID, "the fixture starts with an agent on the card")

	updated, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeAgentID: domain.ClearNullable[uuid.UUID](),
		AssigneeUserID:  domain.ClearNullable[string](),
	})

	require.NoError(t, err)
	assert.Nil(t, updated.AssigneeAgentID, "null unassigns the agent")
	assert.Empty(t, updated.AssigneeUserID, "null unassigns the person")
	assert.Nil(t, f.tasks.tasks[task.ID].AssigneeAgentID)
	assert.Empty(t, f.tasks.tasks[task.ID].AssigneeUserID)
}

// The wire, not the Go struct: this is the decode that used to lose the
// difference, so it is the one worth pinning.
func TestUpdateRequestTellsNullApartFromAnOmittedAssignee(t *testing.T) {
	var omitted domain.UpdateBoardTaskRequest
	require.NoError(t, json.Unmarshal([]byte(`{"title":"x"}`), &omitted))
	assert.False(t, omitted.AssigneeUserID.Present, "an omitted key leaves the person alone")
	assert.False(t, omitted.AssigneeAgentID.Present, "and the agent")

	var cleared domain.UpdateBoardTaskRequest
	require.NoError(t, json.Unmarshal([]byte(`{"assignee_user_id":null,"assignee_agent_id":null}`), &cleared))
	assert.True(t, cleared.AssigneeUserID.Present)
	assert.Nil(t, cleared.AssigneeUserID.Value)
	assert.True(t, cleared.AssigneeAgentID.Present)
	assert.Nil(t, cleared.AssigneeAgentID.Value)

	var set domain.UpdateBoardTaskRequest
	require.NoError(t, json.Unmarshal([]byte(`{"assignee_user_id":"uid-ayse"}`), &set))
	require.NotNil(t, set.AssigneeUserID.Value)
	assert.Equal(t, "uid-ayse", *set.AssigneeUserID.Value)
}

// The refusal a client has to branch on. It carries a sentinel so branching on
// the sentence is no longer the only option, and the sentence itself is
// unchanged because it is what the person reads.
func TestNonMemberRefusalIsTypedAndKeepsItsSentence(t *testing.T) {
	f := newAssigneeFixture("uid-ayse")
	task := f.create(t, domain.CreateBoardTaskRequest{})

	_, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeUserID: domain.SetNullable("uid-stranger"),
	})

	require.ErrorIs(t, err, domain.ErrAssigneeNotMember)
	assert.Contains(t, err.Error(), "is not a member of this workspace")
	assert.Contains(t, err.Error(), "has never opened the app cannot be assigned yet",
		"the sentence names the real cause, and clients must not have to parse it to know which refusal this is")
}
