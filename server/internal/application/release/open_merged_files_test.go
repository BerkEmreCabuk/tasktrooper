package release

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeMergedFiles struct {
	files map[uuid.UUID][]string
	err   error
	calls int
}

func (f *fakeMergedFiles) MergedTaskFiles(_ context.Context, _ uuid.UUID, task domain.BoardTask) ([]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.files[task.ID], nil
}

type mergedFilesFixture struct {
	*openFixture
	merged       *fakeMergedFiles
	repositoryID uuid.UUID
	api          domain.Component
	web          domain.Component
}

// Two active components, so neither "the only component" nor a root
// component can place a task: only the merged files can.
func newMergedFilesFixture(t *testing.T, apiMode domain.DeliveryMode, tasks ...domain.BoardTask) *mergedFilesFixture {
	t.Helper()
	repositoryID := uuid.New()
	for i := range tasks {
		tasks[i].RepositoryID = repositoryID
	}
	f := newOpenFixture(tasks...)
	api := confirmedComponent(apiMode, domain.ExecutorGitHubActions)
	api = f.components.add(repositoryID, api)
	web := domain.Component{ID: uuid.New(), Path: "web", Status: domain.ComponentStatusActive}
	web = f.components.add(repositoryID, web)
	merged := &fakeMergedFiles{files: map[uuid.UUID][]string{}}
	f.svc.SetMergedFiles(merged)
	return &mergedFilesFixture{openFixture: f, merged: merged, repositoryID: repositoryID, api: api, web: web}
}

func doneMergedTask(key string) domain.BoardTask {
	return domain.BoardTask{ID: uuid.New(), Key: key, Column: domain.TaskColumnDone, MergeCommitSHA: mergeSHA}
}

func TestAMergeThatTouchedNoComponentIsTheRelease(t *testing.T) {
	task := doneMergedTask("T-1")
	f := newMergedFilesFixture(t, domain.DeliveryOnMerge, task)
	f.merged.files[task.ID] = []string{"docs/tasks.md", "docs/deploy.md"}

	opening := f.svc.OpenForMerge(context.Background(), f.repositoryID, f.tasks.tasks[task.ID], mergeSHA)

	assert.True(t, opening.Released)
	assert.Equal(t, prompt.Text(mergeOutsideComponentsKey), opening.Next)
	require.Len(t, f.tasks.updates, 1)
	assert.Equal(t, domain.TaskColumnReleased, *f.tasks.updates[0].Column)
	assert.Equal(t, domain.MoveReasonMergeReleasedNoDelivery, f.tasks.updates[0].SystemReason)
	require.Len(t, f.tasks.comments, 1)
	assert.Equal(t, prompt.Text(mergeOutsideComponentsKey), f.tasks.comments[0].Content)
	assert.Empty(t, f.store.releases)
}

func TestAMergeOwnedByOneComponentShipsThroughItAndRecordsIt(t *testing.T) {
	task := doneMergedTask("T-2")
	f := newMergedFilesFixture(t, domain.DeliveryOnMerge, task)
	f.merged.files[task.ID] = []string{"api/handler.go", "docs/api.md"}

	opening := f.svc.OpenForMerge(context.Background(), f.repositoryID, f.tasks.tasks[task.ID], mergeSHA)

	require.NotNil(t, opening.ReleaseID, "files outside every component ride along with the one component that owns the rest")
	release := f.store.releases[*opening.ReleaseID]
	require.NotNil(t, release.ComponentID)
	assert.Equal(t, f.api.ID, *release.ComponentID)
	recorded := f.tasks.tasks[task.ID].ComponentID
	require.NotNil(t, recorded, "the component found from the files is recorded so the delivery catch-up can find the task later")
	assert.Equal(t, f.api.ID, *recorded)
}

func TestAComponentFoundFromTheFilesStillNeedsItsDeliveryConfirmed(t *testing.T) {
	task := doneMergedTask("T-3")
	f := newMergedFilesFixture(t, domain.DeliveryOnMerge, task)
	f.merged.files[task.ID] = []string{"web/app.tsx"}

	opening := f.svc.OpenForMerge(context.Background(), f.repositoryID, f.tasks.tasks[task.ID], mergeSHA)

	assert.True(t, opening.Unconfirmed)
	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "not confirmed")
	recorded := f.tasks.tasks[task.ID].ComponentID
	require.NotNil(t, recorded)
	assert.Equal(t, f.web.ID, *recorded)
	assert.Empty(t, f.store.releases)
}

func TestAMergeThatCannotBePlacedWaitsForAHuman(t *testing.T) {
	cases := map[string]func(f *mergedFilesFixture, task domain.BoardTask){
		"files across two components": func(f *mergedFilesFixture, task domain.BoardTask) {
			f.merged.files[task.ID] = []string{"api/handler.go", "web/app.tsx"}
		},
		"no files read": func(f *mergedFilesFixture, task domain.BoardTask) {},
		"files unreadable": func(f *mergedFilesFixture, task domain.BoardTask) {
			f.merged.err = errors.New("GitHub is not connected")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			task := doneMergedTask("T-4")
			f := newMergedFilesFixture(t, domain.DeliveryOnMerge, task)
			setup(f, task)

			opening := f.svc.OpenForMerge(context.Background(), f.repositoryID, f.tasks.tasks[task.ID], mergeSHA)

			assert.True(t, opening.Unconfirmed)
			assert.Equal(t, prompt.Text(noComponentKey), opening.Next)
			assert.Empty(t, f.tasks.updates)
			assert.Empty(t, f.store.releases)
		})
	}
}

func TestARepositoryWithNoComponentsNeverCallsAMergeOutsideThem(t *testing.T) {
	task := doneMergedTask("T-5")
	task.RepositoryID = uuid.New()
	f := newOpenFixture(task)
	merged := &fakeMergedFiles{files: map[uuid.UUID][]string{task.ID: {"server/main.go"}}}
	f.svc.SetMergedFiles(merged)

	opening := f.svc.OpenForMerge(context.Background(), task.RepositoryID, task, mergeSHA)

	assert.True(t, opening.Unconfirmed, "with no project model, files outside every component say nothing about what ships")
	assert.False(t, opening.Released)
	assert.Empty(t, f.tasks.updates)
}

func TestARecordedComponentIsUsedWithoutReadingTheFiles(t *testing.T) {
	task := doneMergedTask("T-6")
	f := newMergedFilesFixture(t, domain.DeliveryNone, task)
	task = f.tasks.tasks[task.ID]
	task.ComponentID = &f.api.ID
	f.tasks.tasks[task.ID] = task

	opening := f.svc.OpenForMerge(context.Background(), f.repositoryID, task, mergeSHA)

	assert.True(t, opening.Released)
	assert.Zero(t, f.merged.calls)
}

func TestRetryUnresolvedMergesPlacesOnlyWhatTheFilesNowResolve(t *testing.T) {
	docs := doneMergedTask("T-1")
	mixed := doneMergedTask("T-2")
	shipping := doneMergedTask("T-3")
	unmerged := domain.BoardTask{ID: uuid.New(), Key: "T-4", Column: domain.TaskColumnDone}
	f := newMergedFilesFixture(t, domain.DeliveryOnMerge, docs, mixed, shipping, unmerged)
	f.merged.files[docs.ID] = []string{"docs/tasks.md"}
	f.merged.files[mixed.ID] = []string{"api/a.go", "web/b.tsx"}
	f.merged.files[unmerged.ID] = []string{"docs/x.md"}
	f.store.releases[uuid.New()] = domain.Release{RepositoryID: f.repositoryID, Status: domain.ReleaseDeploying,
		Tasks: []domain.ReleaseTaskRef{{ID: shipping.ID}}}
	f.merged.files[shipping.ID] = []string{"docs/y.md"}

	retried := f.svc.RetryUnresolvedMerges(context.Background(), f.repositoryID)

	assert.Equal(t, 1, retried)
	assert.Equal(t, domain.TaskColumnReleased, f.tasks.tasks[docs.ID].Column)
	assert.Equal(t, domain.TaskColumnDone, f.tasks.tasks[mixed.ID].Column)
	assert.Equal(t, domain.TaskColumnDone, f.tasks.tasks[shipping.ID].Column, "a task already carried by a release is not opened again")
	assert.Equal(t, domain.TaskColumnDone, f.tasks.tasks[unmerged.ID].Column)
	require.Len(t, f.tasks.comments, 1, "a task the files still cannot place gets no second comment")
	assert.Equal(t, prompt.Text(mergeOutsideComponentsKey), f.tasks.comments[0].Content)
}

func TestGolden_MergeComponentWording(t *testing.T) {
	assert.Equal(t,
		"None of the files this merge changed belongs to a component, so nothing deploys them; the merge itself was the release.",
		prompt.Text(mergeOutsideComponentsKey))
	assert.Equal(t,
		"This task has no component recorded, and the files its merge changed do not point to exactly one, so nothing knows how it ships. The task stays in done until a human sets the Component field on the task and that component's delivery profile is confirmed on the Deploy tab.",
		prompt.Text(noComponentKey))
}
