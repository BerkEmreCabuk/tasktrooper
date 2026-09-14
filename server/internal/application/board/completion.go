package board

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

type CompletionMarker interface {
	MarkCompleted(ctx context.Context, taskID uuid.UUID, clean bool, at time.Time) error
}

type RevisionLookup interface {
	HasVisited(ctx context.Context, taskID uuid.UUID, column string) (bool, error)
}

type CompletionStamper struct {
	tasks CompletionMarker
	spans RevisionLookup
}

func NewCompletionStamper(tasks CompletionMarker, spans RevisionLookup) *CompletionStamper {
	return &CompletionStamper{tasks: tasks, spans: spans}
}

func (c *CompletionStamper) OnColumnTransition(ctx context.Context, task domain.BoardTask, _, to domain.TaskColumn) {
	if c == nil || c.tasks == nil {
		return
	}
	if to != domain.TaskColumnDone && to != domain.TaskColumnReleased {
		return
	}
	clean := true
	if c.spans != nil {
		revised, err := c.spans.HasVisited(ctx, task.ID, string(domain.TaskColumnNeedRevision))
		if err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("revision lookup failed")
			clean = false
		} else {
			clean = !revised
		}
	}
	if err := c.tasks.MarkCompleted(ctx, task.ID, clean, time.Now().UTC()); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("mark completed failed")
	}
}
