package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type CatalogVersionStore interface {
	AppendVersion(ctx context.Context, v domain.CatalogVersion) (domain.CatalogVersion, error)
	ListVersions(ctx context.Context, targetKind string, targetID uuid.UUID, limit int) ([]domain.CatalogVersion, error)
	GetVersion(ctx context.Context, targetKind string, targetID uuid.UUID, version int) (domain.CatalogVersion, error)
}
