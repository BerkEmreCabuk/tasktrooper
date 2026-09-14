package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// CatalogVersionStore is the append-only history of skill and rule content.
type CatalogVersionStore interface {
	// AppendVersion stores the snapshot and assigns the next version number
	// for its target.
	AppendVersion(ctx context.Context, v domain.CatalogVersion) (domain.CatalogVersion, error)
	ListVersions(ctx context.Context, targetKind string, targetID uuid.UUID, limit int) ([]domain.CatalogVersion, error)
	GetVersion(ctx context.Context, targetKind string, targetID uuid.UUID, version int) (domain.CatalogVersion, error)
}
