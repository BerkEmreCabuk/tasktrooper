package orchestrator

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TopologicalWaves orders a plan into dependency waves for a run that starts
// from nothing: every depends_on must name another task in the same slice.
func TopologicalWaves(tasks []domain.PlannerTask) ([][]string, error) {
	return TopologicalWavesWithCompleted(tasks, nil)
}

// TopologicalWavesWithCompleted orders tasks into waves for a run that already
// has finished work behind it, which is exactly what a repair plan is.
//
// completed holds the ids of tasks that ran in an earlier round of this same run
// and are NOT being scheduled again — their results are already in hand. A
// depends_on naming one of those is satisfied, not pending: the repair task goes
// into the first wave and the executor hands it that task's stored result like
// any other dependency output. Any other reading stalls the DAG — counting a
// finished task as an in-degree leaves a task nothing will ever decrement, and
// its whole dependency subtree never runs.
//
// Treating those ids as unknown is what made the replanner's own instruction
// impossible to obey. Its prompt says "depends_on may reference existing task
// ids from the prior plan" (buildReplannerSystemPrompt), the model followed it,
// and both this function and validatePlannerOutput knew only the tasks of the
// current planning turn — so the repair plan was rejected, retried against a
// rejection its prompt contradicts, and finally abandoned with the verification
// issue unrepaired.
//
// An id in neither set is still an error: that is a hallucinated dependency, and
// running the task anyway would run it without the input it says it needs.
func TopologicalWavesWithCompleted(tasks []domain.PlannerTask, completed map[string]bool) ([][]string, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	ids := make(map[string]domain.PlannerTask)
	inDegree := make(map[string]int)
	dependents := make(map[string][]string)

	for _, t := range tasks {
		ids[t.ID] = t
		inDegree[t.ID] = 0
	}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := ids[dep]; !ok {
				// Scheduled this round wins over completed: a task being re-run
				// has to finish again before its dependents start.
				if completed[dep] {
					continue
				}
				return nil, fmt.Errorf("unknown dependency %s for task %s", dep, t.ID)
			}
			inDegree[t.ID]++
			dependents[dep] = append(dependents[dep], t.ID)
		}
	}

	var waves [][]string
	remaining := len(tasks)
	ready := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}

	for len(ready) > 0 {
		wave := make([]string, len(ready))
		copy(wave, ready)
		waves = append(waves, wave)
		remaining -= len(ready)

		nextReady := make([]string, 0)
		for _, id := range ready {
			for _, dep := range dependents[id] {
				inDegree[dep]--
				if inDegree[dep] == 0 {
					nextReady = append(nextReady, dep)
				}
			}
		}
		ready = nextReady
	}

	if remaining > 0 {
		return nil, fmt.Errorf("cycle detected in task dependencies")
	}
	return waves, nil
}
