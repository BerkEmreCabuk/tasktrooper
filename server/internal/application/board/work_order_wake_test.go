package board_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeWakeResourceLister struct {
	byID    map[uuid.UUID]domain.BoardTask
	cleared []uuid.UUID
}

func (f *fakeWakeResourceLister) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *fakeWakeResourceLister) ClearWorkOrderWaiting(_ context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	task, ok := f.byID[taskID]
	if !ok {
		return domain.BoardTask{}, false, nil
	}
	f.cleared = append(f.cleared, taskID)
	return task, true, nil
}

type fakeWakeBlockerReader struct {
	openByTask map[uuid.UUID][]domain.BoardTask
}

func (f *fakeWakeBlockerReader) ListBlockingSources(_ context.Context, targetTaskID uuid.UUID) ([]domain.BoardTask, error) {
	return f.openByTask[targetTaskID], nil
}

type fakeWakeDependents struct {
	bySource map[uuid.UUID][]domain.TaskRelation
}

func (f *fakeWakeDependents) ListBySource(_ context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error) {
	return f.bySource[sourceTaskID], nil
}

func TestWakeDependentsOfDispatchesTheClearDependentAndLeavesTheStillBlockedOneParked(t *testing.T) {
	blockerID := uuid.New()
	agentID := uuid.New()
	clearDependent := domain.BoardTask{
		ID:              uuid.New(),
		RepositoryID:    uuid.New(),
		Key:             "T-A",
		Column:          domain.TaskColumnInProgress,
		TaskType:        "task",
		AssigneeAgentID: &agentID,
	}
	stillBlockedDependent := domain.BoardTask{
		ID:       uuid.New(),
		Key:      "T-D",
		Column:   domain.TaskColumnInProgress,
		TaskType: "task",
	}
	otherBlockerID := uuid.New()

	dependents := &fakeWakeDependents{bySource: map[uuid.UUID][]domain.TaskRelation{
		blockerID: {
			{RelationType: domain.TaskRelationBlocks, SourceTaskID: blockerID, TargetTaskID: clearDependent.ID},
			{RelationType: domain.TaskRelationBlocks, SourceTaskID: blockerID, TargetTaskID: stillBlockedDependent.ID},
			{RelationType: domain.TaskRelationDeployDependsOn, SourceTaskID: blockerID, TargetTaskID: clearDependent.ID},
		},
	}}
	blockers := &fakeWakeBlockerReader{openByTask: map[uuid.UUID][]domain.BoardTask{
		stillBlockedDependent.ID: {{ID: otherBlockerID, Key: "T-C", Column: domain.TaskColumnInProgress}},
	}}
	resources := &fakeWakeResourceLister{byID: map[uuid.UUID]domain.BoardTask{
		clearDependent.ID:        clearDependent,
		stillBlockedDependent.ID: stillBlockedDependent,
	}}

	boardCfg := &fakeBoardConfigStore{}
	events := &fakeEventStore{}
	runs := &fakeRunStore{}
	runner := &fakeRunner{}
	disp := board.NewDispatcher(boardCfg, events, runs, runner, true)
	disp.SetWorkflows(workflowtest.Default().Reader())

	sweeper := board.NewWorkOrderSweeper(resources, blockers, disp)
	sweeper.SetDependents(dependents)

	sweeper.WakeDependentsOf(context.Background(), blockerID)

	require.Len(t, resources.cleared, 1)
	assert.Equal(t, clearDependent.ID, resources.cleared[0], "only the fully-clear dependent is unparked")

	require.Len(t, runner.jobs, 1, "the clear dependent must be dispatched in the same call, not on the next poll")
	assert.Equal(t, clearDependent.ID, runner.jobs[0].Task.ID)
	assert.Equal(t, agentID, runner.jobs[0].Run.AgentID)
}

func TestWakeDependentsOfIsANoOpWithoutDependentsWired(t *testing.T) {
	boardCfg := &fakeBoardConfigStore{}
	events := &fakeEventStore{}
	runs := &fakeRunStore{}
	runner := &fakeRunner{}
	disp := board.NewDispatcher(boardCfg, events, runs, runner, true)
	disp.SetWorkflows(workflowtest.Default().Reader())
	sweeper := board.NewWorkOrderSweeper(&fakeWakeResourceLister{}, &fakeWakeBlockerReader{}, disp)

	sweeper.WakeDependentsOf(context.Background(), uuid.New())

	assert.Empty(t, runner.jobs)
}
