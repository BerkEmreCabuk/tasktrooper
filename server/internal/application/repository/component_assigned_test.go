package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type componentAssignedResolver struct{ byID map[uuid.UUID]domain.Component }

func (r componentAssignedResolver) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	c, ok := r.byID[id]
	if !ok {
		return domain.Component{}, errors.New("not found")
	}
	return c, nil
}

func (r componentAssignedResolver) ComponentByPath(context.Context, uuid.UUID, string) (domain.Component, error) {
	return domain.Component{}, errors.New("not found")
}

type componentAssignedCall struct{ repositoryID, componentID uuid.UUID }

type componentAssignedFixture struct {
	svc       *Service
	repoID    uuid.UUID
	tasks     *fakePackageTaskStore
	api, web  domain.Component
	triggered []componentAssignedCall
}

func newComponentAssignedFixture() *componentAssignedFixture {
	repoID := uuid.New()
	api := domain.Component{ID: uuid.New(), RepositoryID: repoID, Path: "api", Status: domain.ComponentStatusActive}
	web := domain.Component{ID: uuid.New(), RepositoryID: repoID, Path: "web", Status: domain.ComponentStatusActive}
	tasks := &fakePackageTaskStore{tasks: map[uuid.UUID]domain.BoardTask{}}
	fx := workflowtest.Default()
	f := &componentAssignedFixture{repoID: repoID, tasks: tasks, api: api, web: web}
	f.svc = &Service{
		repos:      &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:      tasks,
		workflows:  fx.Reader(),
		roles:      fx.Resolver(),
		components: componentAssignedResolver{byID: map[uuid.UUID]domain.Component{api.ID: api, web.ID: web}},
	}
	f.svc.SetComponentAssignedHook(func(_ context.Context, repositoryID, componentID uuid.UUID) {
		f.triggered = append(f.triggered, componentAssignedCall{repositoryID, componentID})
	})
	return f
}

func (f *componentAssignedFixture) seed(task domain.BoardTask) domain.BoardTask {
	task.ID = uuid.New()
	task.RepositoryID = f.repoID
	task.TaskType = "task"
	f.tasks.tasks[task.ID] = task
	return task
}

func (f *componentAssignedFixture) assign(t *testing.T, task domain.BoardTask, componentID *uuid.UUID) {
	t.Helper()
	req := domain.UpdateBoardTaskRequest{ComponentID: domain.Nullable[uuid.UUID]{Present: true, Value: componentID}}
	_, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, req)
	require.NoError(t, err)
}

func TestNamingAMergedDoneTasksComponentRetriesItsRelease(t *testing.T) {
	f := newComponentAssignedFixture()
	task := f.seed(domain.BoardTask{Column: domain.TaskColumnDone, MergeCommitSHA: "abc123"})

	f.assign(t, task, &f.api.ID)

	require.Len(t, f.triggered, 1)
	assert.Equal(t, componentAssignedCall{f.repoID, f.api.ID}, f.triggered[0])
}

func TestAComponentChangeThatCannotShipAnythingDoesNotRetryARelease(t *testing.T) {
	f := newComponentAssignedFixture()
	cases := map[string]struct {
		task      domain.BoardTask
		component func(f *componentAssignedFixture) *uuid.UUID
	}{
		"not merged yet": {
			task:      domain.BoardTask{Column: domain.TaskColumnDone},
			component: func(f *componentAssignedFixture) *uuid.UUID { return &f.api.ID },
		},
		"still in review": {
			task:      domain.BoardTask{Column: domain.TaskColumnCodeReview, MergeCommitSHA: "abc123"},
			component: func(f *componentAssignedFixture) *uuid.UUID { return &f.api.ID },
		},
		"the same component again": {
			task:      domain.BoardTask{Column: domain.TaskColumnDone, MergeCommitSHA: "abc123", ComponentID: &f.api.ID},
			component: func(f *componentAssignedFixture) *uuid.UUID { return &f.api.ID },
		},
		"cleared": {
			task:      domain.BoardTask{Column: domain.TaskColumnDone, MergeCommitSHA: "abc123", ComponentID: &f.api.ID},
			component: func(*componentAssignedFixture) *uuid.UUID { return nil },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f.triggered = nil
			task := f.seed(tc.task)

			f.assign(t, task, tc.component(f))

			assert.Empty(t, f.triggered)
		})
	}
}

func TestMovingAMergedTasksComponentRetriesForTheNewOne(t *testing.T) {
	f := newComponentAssignedFixture()
	task := f.seed(domain.BoardTask{Column: domain.TaskColumnDone, MergeCommitSHA: "abc123", ComponentID: &f.api.ID})

	f.assign(t, task, &f.web.ID)

	require.Len(t, f.triggered, 1)
	assert.Equal(t, f.web.ID, f.triggered[0].componentID)
}
