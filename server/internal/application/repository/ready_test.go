package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type readyTaskStore struct {
	*fakePackageTaskStore
}

func (r *readyTaskStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	out := make([]domain.BoardTask, 0)
	for _, task := range r.tasks {
		if task.RepositoryID == repositoryID {
			out = append(out, task)
		}
	}
	return out, nil
}

type readyRelationStore struct {
	*graphRelationStore
	unfinished []domain.TaskRelation
}

func (r *readyRelationStore) ListUnfinishedBlockers(context.Context) ([]domain.TaskRelation, error) {
	return r.unfinished, nil
}

func newReadyFixture(tasks ...domain.BoardTask) (*Service, map[string]uuid.UUID) {
	byKey := make(map[string]uuid.UUID, len(tasks))
	taskMap := make(map[uuid.UUID]domain.BoardTask, len(tasks))
	for _, task := range tasks {
		taskMap[task.ID] = task
		byKey[task.Key] = task.ID
	}
	svc := &Service{
		tasks:     &readyTaskStore{fakePackageTaskStore: &fakePackageTaskStore{tasks: taskMap}},
		relations: &readyRelationStore{graphRelationStore: newGraphRelations()},
	}
	return svc, byKey
}

func blockEdge(source, target uuid.UUID) domain.TaskRelation {
	return domain.TaskRelation{ID: uuid.New(), SourceTaskID: source, TargetTaskID: target, RelationType: domain.TaskRelationBlocks}
}

func TestListReadyTasksExcludesATaskWithAnUnfinishedBlocker(t *testing.T) {
	repoID := uuid.New()
	blocker := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-1", Column: domain.TaskColumnInProgress}
	blocked := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-2", Column: domain.TaskColumnTodo}
	svc, _ := newReadyFixture(blocker, blocked)
	svc.relations.(*readyRelationStore).unfinished = []domain.TaskRelation{blockEdge(blocker.ID, blocked.ID)}

	ready, err := svc.ListReadyTasks(context.Background(), repoID)

	require.NoError(t, err)
	for _, task := range ready {
		assert.NotEqual(t, "T-2", task.Key, "a task with an unfinished blocker must not be in the ready queue")
	}
}

func TestListReadyTasksIncludesATaskWhoseBlockerIsDone(t *testing.T) {
	repoID := uuid.New()
	blocker := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-1", Column: domain.TaskColumnDone}
	unblocked := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-2", Column: domain.TaskColumnTodo}
	svc, _ := newReadyFixture(blocker, unblocked)

	ready, err := svc.ListReadyTasks(context.Background(), repoID)

	require.NoError(t, err)
	keys := make([]string, len(ready))
	for i, task := range ready {
		keys[i] = task.Key
	}
	assert.Contains(t, keys, "T-2")
}

func TestListReadyTasksExcludesBlockedAndInProgressColumns(t *testing.T) {
	repoID := uuid.New()
	backlog := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-1", Column: domain.TaskColumnBacklog}
	blockedCol := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-2", Column: domain.TaskColumnBlocked}
	inProgress := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-3", Column: domain.TaskColumnInProgress}
	svc, _ := newReadyFixture(backlog, blockedCol, inProgress)

	ready, err := svc.ListReadyTasks(context.Background(), repoID)

	require.NoError(t, err)
	require.Len(t, ready, 1)
	assert.Equal(t, "T-1", ready[0].Key)
}

func TestListReadyTasksSortsByPriorityThenTaskNumber(t *testing.T) {
	repoID := uuid.New()
	low := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-1", TaskNumber: 1, Column: domain.TaskColumnTodo, Priority: domain.TaskPriorityLow}
	criticalLate := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-4", TaskNumber: 4, Column: domain.TaskColumnTodo, Priority: domain.TaskPriorityCritical}
	criticalEarly := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-2", TaskNumber: 2, Column: domain.TaskColumnTodo, Priority: domain.TaskPriorityCritical}
	medium := domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Key: "T-3", TaskNumber: 3, Column: domain.TaskColumnBacklog, Priority: domain.TaskPriorityMedium}
	svc, _ := newReadyFixture(low, criticalLate, criticalEarly, medium)

	ready, err := svc.ListReadyTasks(context.Background(), repoID)

	require.NoError(t, err)
	require.Len(t, ready, 4)
	keys := make([]string, len(ready))
	for i, task := range ready {
		keys[i] = task.Key
	}
	assert.Equal(t, []string{"T-2", "T-4", "T-3", "T-1"}, keys)
}

func TestListReadyTasksNilRepositoryIDListsAllRepositories(t *testing.T) {
	repoA, repoB := uuid.New(), uuid.New()
	a := domain.BoardTask{ID: uuid.New(), RepositoryID: repoA, Key: "A-1", Column: domain.TaskColumnTodo}
	b := domain.BoardTask{ID: uuid.New(), RepositoryID: repoB, Key: "B-1", Column: domain.TaskColumnTodo}
	svc, _ := newReadyFixture(a, b)

	ready, err := svc.ListReadyTasks(context.Background(), uuid.Nil)

	require.NoError(t, err)
	keys := make([]string, len(ready))
	for i, task := range ready {
		keys[i] = task.Key
	}
	assert.ElementsMatch(t, []string{"A-1", "B-1"}, keys)
}
