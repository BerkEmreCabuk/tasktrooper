package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// TenantSeedStore runs the per-tenant board seed through the ordinary
// tenant-scoped handle, so it is policy-protected like every other write: a
// tenant can only seed its own board.
type TenantSeedStore struct {
	pool *DB
}

func NewTenantSeedStore(pool *DB) *TenantSeedStore { return &TenantSeedStore{pool: pool} }

// Seed runs the per-tenant board seed in ONE tenant-scoped transaction, so a
// tenant that half-seeds does not exist: either it has a board or it has
// nothing and the next request tries again.
func (s *TenantSeedStore) Seed(ctx context.Context, sql string) error {
	return s.pool.InTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
}
