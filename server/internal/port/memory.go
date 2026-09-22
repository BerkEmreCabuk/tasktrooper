package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AgentMemoryStore interface {
	Create(ctx context.Context, m domain.AgentMemory) (domain.AgentMemory, error)
	Get(ctx context.Context, id uuid.UUID) (domain.AgentMemory, error)
	List(ctx context.Context, q domain.MemoryQuery) ([]domain.AgentMemory, error)
	Update(ctx context.Context, m domain.AgentMemory) (domain.AgentMemory, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountInScope(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID) (int, error)
	DeleteOldestInScope(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, n int) error
}
