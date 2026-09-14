package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AgentKPIStore interface {
	CreateKPI(ctx context.Context, k domain.AgentKPI) (domain.AgentKPI, error)
	UpdateKPI(ctx context.Context, k domain.AgentKPI) (domain.AgentKPI, error)
	DeleteKPI(ctx context.Context, id uuid.UUID) error
	GetKPI(ctx context.Context, id uuid.UUID) (domain.AgentKPI, error)
	ListByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.AgentKPI, error)
	UpsertResult(ctx context.Context, r domain.AgentKPIResult) (domain.AgentKPIResult, error)
	ListResults(ctx context.Context, agentID uuid.UUID, from, to time.Time) ([]domain.AgentKPIResult, error)
	LatestResults(ctx context.Context, agentID uuid.UUID) ([]domain.AgentKPIResult, error)
}
