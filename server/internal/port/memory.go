package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AgentMemoryStore interface {
	Create(ctx context.Context, m domain.AgentMemory) (domain.AgentMemory, error)
	Get(ctx context.Context, id uuid.UUID) (domain.AgentMemory, error)
	// List reads one slice of the owner × repository matrix, newest first.
	List(ctx context.Context, q domain.MemoryQuery) ([]domain.AgentMemory, error)
	Update(ctx context.Context, m domain.AgentMemory) (domain.AgentMemory, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// CountInScope and DeleteOldestInScope bound one exact bucket, so a busy
	// repository cannot evict what the agent learned in another one.
	CountInScope(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID) (int, error)
	DeleteOldestInScope(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, n int) error
}
