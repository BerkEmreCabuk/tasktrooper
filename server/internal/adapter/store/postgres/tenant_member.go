package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/application/tenantboot"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// TenantMemberStore is the local mirror of the control plane's roster, plus the
// per-tenant board seed. Both go through the ordinary tenant-scoped handle, so
// they are policy-protected like everything else: a tenant can only mirror its
// own members and only seed its own board.
type TenantMemberStore struct {
	pool *DB
}

func NewTenantMemberStore(pool *DB) *TenantMemberStore { return &TenantMemberStore{pool: pool} }

// UpsertMember refreshes one member from the signed headers of the request
// being served.
//
// created_at is left alone on conflict: it is when this database first saw the
// person, which is worth keeping, and the control plane owns the real join
// date anyway.
func (s *TenantMemberStore) UpsertMember(ctx context.Context, m tenantboot.Member) error {
	role := string(tenant.ParseRole(string(m.Role)))
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenant_members (user_id, email, display_name, role)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET
			role = EXCLUDED.role,
			-- An empty email or name is "the header did not carry one", not
			-- "the person no longer has one", so a blank never overwrites a
			-- value the roster already holds.
			email = CASE WHEN EXCLUDED.email = '' THEN tenant_members.email ELSE EXCLUDED.email END,
			display_name = CASE WHEN EXCLUDED.display_name = '' THEN tenant_members.display_name ELSE EXCLUDED.display_name END,
			updated_at = now()
	`, m.UserID, m.Email, m.DisplayName, role)
	if err != nil {
		return fmt.Errorf("upsert tenant member: %w", err)
	}
	return nil
}

func (s *TenantMemberStore) ListMembers(ctx context.Context) ([]tenantboot.Member, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, email, display_name, role
		FROM tenant_members ORDER BY display_name, user_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list tenant members: %w", err)
	}
	defer rows.Close()
	var out []tenantboot.Member
	for rows.Next() {
		var m tenantboot.Member
		var role string
		if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &role); err != nil {
			return nil, err
		}
		m.Role = tenant.ParseRole(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Seed runs the per-tenant board seed in ONE tenant-scoped transaction, so a
// tenant that half-seeds does not exist: either it has a board or it has
// nothing and the next request tries again.
func (s *TenantMemberStore) Seed(ctx context.Context, sql string) error {
	return s.pool.InTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
}
