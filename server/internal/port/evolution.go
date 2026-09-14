package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AgentEvolutionStore interface {
	CreateReflection(ctx context.Context, r domain.AgentReflection) (domain.AgentReflection, error)
	CompleteReflection(ctx context.Context, r domain.AgentReflection) error
	GetReflection(ctx context.Context, id uuid.UUID) (domain.AgentReflection, error)
	ListReflections(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.AgentReflection, error)
	LatestCompletedReflection(ctx context.Context, agentID uuid.UUID) (*domain.AgentReflection, error)
	LatestReflectionByTrigger(ctx context.Context, agentID uuid.UUID, trigger string) (*domain.AgentReflection, error)
	CreateEvent(ctx context.Context, e domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error)
	GetEvent(ctx context.Context, id uuid.UUID) (domain.AgentEvolutionEvent, error)
	ListEvents(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.AgentEvolutionEvent, error)
	ListEventsByImpact(ctx context.Context, impact string, createdBefore time.Time, limit int) ([]domain.AgentEvolutionEvent, error)
	UpdateEventImpact(ctx context.Context, id uuid.UUID, impact string) error
}
