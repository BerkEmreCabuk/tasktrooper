package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A repair task may depend on an already-run original task; that dependency is satisfied before scheduling.
func TestTopologicalWavesWithCompleted_PriorPlanDependencyRunsImmediately(t *testing.T) {
	waves, err := orchestrator.TopologicalWavesWithCompleted(
		[]domain.PlannerTask{{ID: "r1", DependsOn: []string{"t1"}}},
		map[string]bool{"t1": true},
	)

	require.NoError(t, err, "an already-completed task is a known id, not an unknown one")
	require.Len(t, waves, 1, "nothing is left to wait for")
	assert.Equal(t, []string{"r1"}, waves[0])
}

// Only backwards references to finished work are free; repair tasks still order among themselves.
func TestTopologicalWavesWithCompleted_OrdersRepairTasksAmongThemselves(t *testing.T) {
	waves, err := orchestrator.TopologicalWavesWithCompleted([]domain.PlannerTask{
		{ID: "r1", DependsOn: []string{"t1"}},
		{ID: "r2", DependsOn: []string{"t2", "r1"}},
	}, map[string]bool{"t1": true, "t2": true})

	require.NoError(t, err)
	require.Len(t, waves, 2)
	assert.Equal(t, []string{"r1"}, waves[0])
	assert.Equal(t, []string{"r2"}, waves[1])
}

// A task re-run this round must finish again before its dependents start.
func TestTopologicalWavesWithCompleted_RescheduledDependencyStillOrders(t *testing.T) {
	waves, err := orchestrator.TopologicalWavesWithCompleted([]domain.PlannerTask{
		{ID: "t1"},
		{ID: "r1", DependsOn: []string{"t1"}},
	}, map[string]bool{"t1": true})

	require.NoError(t, err)
	require.Len(t, waves, 2)
	assert.Equal(t, []string{"t1"}, waves[0])
	assert.Equal(t, []string{"r1"}, waves[1])
}

// An id in neither set is a hallucinated dependency.
func TestTopologicalWavesWithCompleted_StillRejectsUnknownDependencies(t *testing.T) {
	_, err := orchestrator.TopologicalWavesWithCompleted(
		[]domain.PlannerTask{{ID: "r1", DependsOn: []string{"nope"}}},
		map[string]bool{"t1": true},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown dependency nope")
}

func TestTopologicalWaves_FirstPlanHasNoCompletedWork(t *testing.T) {
	_, err := orchestrator.TopologicalWaves([]domain.PlannerTask{
		{ID: "r1", DependsOn: []string{"t1"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown dependency t1")
}
