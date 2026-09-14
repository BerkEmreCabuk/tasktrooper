package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type GoldenTaskStore interface {
	CreateTask(ctx context.Context, t domain.GoldenTask) (domain.GoldenTask, error)
	UpdateTask(ctx context.Context, t domain.GoldenTask) (domain.GoldenTask, error)
	DeleteTask(ctx context.Context, id uuid.UUID) error
	ListByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.GoldenTask, error)
	SaveResult(ctx context.Context, r domain.GoldenResult) (domain.GoldenResult, error)
	ListResults(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.GoldenResult, error)
}
