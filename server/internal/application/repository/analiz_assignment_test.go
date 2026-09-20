package repository

// CreateTask's task-type assignee resolution: a new task's assignee comes
// from its type's assignee_role_id/assignee_mode (port.RoleResolver's
// AssigneeForNewTask), not from a hardcoded analiz-vs-everything-else special
// case read off settings. See workflowtest.Default for the fixture this
// exercises: analiz is assignee_mode=override with role "analyst", which
// resolves (in the "all default" scenario) to the agent "system-architect"
// for every area — the direct replacement for the old
// analiz_assignee_backend/frontend/mobile settings this test used to cover.

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestCreateTaskOverridesAssigneeForAnOverrideModeType(t *testing.T) {
	pmChoice := uuid.New()
	f := newAssigneeFixture()
	f.svc.repos = &fakeReleaseRepoStore{repo: domain.Repository{ID: f.repoID, Kind: domain.RepoKindBackend}}

	task := f.create(t, domain.CreateBoardTaskRequest{
		TaskType:        "analiz",
		AssigneeAgentID: &pmChoice,
	})

	require.NotNil(t, task.AssigneeAgentID)
	assert.Equal(t, workflowtest.AgentID("system-architect"), *task.AssigneeAgentID,
		"an override-mode type's role always wins over the requested assignee")
}

func TestCreateTaskDoesNotOverrideAssigneeForANoneModeType(t *testing.T) {
	pmChoiceID := uuid.New()
	f := newAssigneeFixture()
	f.svc.repos = &fakeReleaseRepoStore{repo: domain.Repository{ID: f.repoID, Kind: domain.RepoKindBackend}}

	task := f.create(t, domain.CreateBoardTaskRequest{
		TaskType:        "task",
		AssigneeAgentID: &pmChoiceID,
	})

	require.NotNil(t, task.AssigneeAgentID)
	assert.Equal(t, pmChoiceID, *task.AssigneeAgentID, "assignee_mode=none leaves the requested assignee alone")
}

func TestCreateTaskLeavesAssigneeAloneWhenRoleResolverIsUnwired(t *testing.T) {
	f := newAssigneeFixture()
	f.svc.roles = nil
	pmChoiceID := uuid.New()

	task := f.create(t, domain.CreateBoardTaskRequest{
		TaskType:        "task",
		AssigneeAgentID: &pmChoiceID,
	})

	require.NotNil(t, task.AssigneeAgentID)
	assert.Equal(t, pmChoiceID, *task.AssigneeAgentID)
}
