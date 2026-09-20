package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// CatalogRepoReader turns the external agent catalog (a git clone or a local
// directory under adapter/catalogrepo) into domain definitions. Source lives
// entirely in that one adapter, so the sync logic in application/catalog can be
// exercised against a fake.
type CatalogRepoReader interface {
	ReadCatalog(ctx context.Context) ([]domain.UpstreamAgent, string, error)
}

// CatalogSyncStore is the last-sync state and the pending-changes ledger a
// catalog sync reads and writes. Pending rows are the changes the per-agent
// gates or a failed merge kept the sync from applying, so they survive for the
// user to act on rather than vanishing with the sync run.
type CatalogSyncStore interface {
	GetCatalogSyncState(ctx context.Context) (domain.CatalogSyncState, error)
	SaveCatalogSyncState(ctx context.Context, state domain.CatalogSyncState) error
	AppendCatalogPending(ctx context.Context, pending domain.CatalogPending) error
	ListCatalogPending(ctx context.Context) ([]domain.CatalogPending, error)
	DeleteCatalogPending(ctx context.Context, id uuid.UUID) error
}