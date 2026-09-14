package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A repair task is allowed to depend on a task of the original plan — the
// replanner prompt says so in as many words. That task has already run, so the
// dependency is satisfied before the repair plan is even scheduled: r1 belongs
// in the first wave. Counting it as pending instead left an in-degree nothing
// could ever decrement, and the whole repair round errored out.
func TestTopologicalWavesWithCompleted_PriorPlanDependencyRunsImmediately(t *testing.T) {
	waves, err := orchestrator.TopologicalWavesWithCompleted(
		[]domain.PlannerTask{{ID: "r1", DependsOn: []string{"t1"}}},
		map[string]bool{"t1": true},
	)

	require.NoError(t, err, "an already-completed task is a known id, not an unknown one")
	require.Len(t, waves, 1, "nothing is left to wait for")
	assert.Equal(t, []string{"r1"}, waves[0])
}

// Dependencies inside the repair plan still order it; only the ones pointing
// backwards at finished work are free.
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

// A task being re-run this round has to finish again before its dependents
// start, even though an earlier round also produced a result for that id.
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

// An id in neither set is a hallucinated dependency: running the task anyway
// would run it without the input it says it needs.
func TestTopologicalWavesWithCompleted_StillRejectsUnknownDependencies(t *testing.T) {
	_, err := orchestrator.TopologicalWavesWithCompleted(
		[]domain.PlannerTask{{ID: "r1", DependsOn: []string{"nope"}}},
		map[string]bool{"t1": true},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown dependency nope")
}

// A first plan has no round behind it, so a backwards reference there is still
// exactly what it always was.
func TestTopologicalWaves_FirstPlanHasNoCompletedWork(t *testing.T) {
	_, err := orchestrator.TopologicalWaves([]domain.PlannerTask{
		{ID: "r1", DependsOn: []string{"t1"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown dependency t1")
}
