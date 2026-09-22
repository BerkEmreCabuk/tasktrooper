package orchestrator

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TopologicalWaves(tasks []domain.PlannerTask) ([][]string, error) {
	return TopologicalWavesWithCompleted(tasks, nil)
}

// completed ids are pre-satisfied dependencies kept out of the DAG; an id in neither set is still an error.
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
				// A re-run is scheduled this round and must finish again before its dependents start.
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
