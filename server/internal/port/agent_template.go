package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AgentTemplateStore interface {
	List(ctx context.Context) ([]domain.AgentTemplate, error)
	Get(ctx context.Context, id uuid.UUID) (domain.AgentTemplate, error)
	UpsertByName(ctx context.Context, tpl domain.AgentTemplate) (domain.AgentTemplate, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
