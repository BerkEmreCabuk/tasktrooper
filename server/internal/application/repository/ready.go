package repository

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var readyPriorityRank = map[domain.TaskPriority]int{
	domain.TaskPriorityCritical: 0,
	domain.TaskPriorityHigh:     1,
	domain.TaskPriorityMedium:   2,
	domain.TaskPriorityLow:      3,
}

func (s *Service) ListReadyTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	var tasks []domain.BoardTask
	var err error
	if repositoryID == uuid.Nil {
		tasks, err = s.tasks.ListAll(ctx)
	} else {
		tasks, err = s.tasks.ListByRepository(ctx, repositoryID)
	}
	if err != nil {
		return nil, err
	}

	blocked := map[uuid.UUID]bool{}
	if s.relations != nil {
		edges, err := s.relations.ListUnfinishedBlockers(ctx)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			blocked[edge.TargetTaskID] = true
		}
	}

	ready := make([]domain.BoardTask, 0, len(tasks))
	for _, task := range tasks {
		if task.Column != domain.TaskColumnBacklog && task.Column != domain.TaskColumnTodo {
			continue
		}
		if blocked[task.ID] {
			continue
		}
		ready = append(ready, task)
	}

	sort.SliceStable(ready, func(i, j int) bool {
		ri, rj := readyPriorityRankOf(ready[i].Priority), readyPriorityRankOf(ready[j].Priority)
		if ri != rj {
			return ri < rj
		}
		return ready[i].TaskNumber < ready[j].TaskNumber
	})
	return ready, nil
}

func readyPriorityRankOf(p domain.TaskPriority) int {
	if rank, ok := readyPriorityRank[p]; ok {
		return rank
	}
	return len(readyPriorityRank)
}
