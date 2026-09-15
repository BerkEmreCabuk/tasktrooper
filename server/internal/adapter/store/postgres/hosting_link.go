package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// HostingLinkStore persists repository_hosting_links (migration 112).
type HostingLinkStore struct {
	pool *DB
}

func NewHostingLinkStore(pool *DB) *HostingLinkStore {
	return &HostingLinkStore{pool: pool}
}

const hostingLinkCols = `id, repository_id, area, provider, external_id, external_name, scope_id, scope_slug, root_directory, production_url, source, evidence, created_at, updated_at`

func scanHostingLink(row pgx.Row) (domain.HostingLink, error) {
	var l domain.HostingLink
	err := row.Scan(
		&l.ID, &l.RepositoryID, &l.Area, &l.Provider, &l.ExternalID, &l.ExternalName,
		&l.ScopeID, &l.ScopeSlug, &l.RootDirectory, &l.ProductionURL, &l.Source, &l.Evidence,
		&l.CreatedAt, &l.UpdatedAt,
	)
	return l, err
}

func (s *HostingLinkStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.HostingLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+hostingLinkCols+`
		FROM repository_hosting_links WHERE repository_id = $1 ORDER BY area`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list hosting links: %w", err)
	}
	defer rows.Close()
	var out []domain.HostingLink
	for rows.Next() {
		l, err := scanHostingLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan hosting link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *HostingLinkStore) Get(ctx context.Context, repositoryID uuid.UUID, area string) (domain.HostingLink, error) {
	l, err := scanHostingLink(s.pool.QueryRow(ctx, `SELECT `+hostingLinkCols+`
		FROM repository_hosting_links WHERE repository_id = $1 AND area = $2`, repositoryID, area))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HostingLink{}, fmt.Errorf("get hosting link: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.HostingLink{}, fmt.Errorf("get hosting link: %w", err)
	}
	return l, nil
}

func (s *HostingLinkStore) Save(ctx context.Context, in domain.HostingLink) (domain.HostingLink, error) {
	l, err := scanHostingLink(s.pool.QueryRow(ctx, `
		INSERT INTO repository_hosting_links
			(repository_id, area, provider, external_id, external_name, scope_id, scope_slug, root_directory, production_url, source, evidence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (repository_id, area) DO UPDATE SET
			provider = EXCLUDED.provider,
			external_id = EXCLUDED.external_id,
			external_name = EXCLUDED.external_name,
			scope_id = EXCLUDED.scope_id,
			scope_slug = EXCLUDED.scope_slug,
			root_directory = EXCLUDED.root_directory,
			production_url = EXCLUDED.production_url,
			source = EXCLUDED.source,
			evidence = EXCLUDED.evidence,
			updated_at = now()
		RETURNING `+hostingLinkCols,
		in.RepositoryID, in.Area, in.Provider, in.ExternalID, in.ExternalName, in.ScopeID, in.ScopeSlug,
		in.RootDirectory, in.ProductionURL, in.Source, in.Evidence))
	if err != nil {
		return domain.HostingLink{}, fmt.Errorf("save hosting link: %w", err)
	}
	return l, nil
}

// Delete removes the link for one area; a missing row is not an error.
func (s *HostingLinkStore) Delete(ctx context.Context, repositoryID uuid.UUID, area string) error {
	const q = `DELETE FROM repository_hosting_links WHERE repository_id = $1 AND area = $2`
	if _, err := s.pool.Exec(ctx, q, repositoryID, area); err != nil {
		return fmt.Errorf("delete hosting link: %w", err)
	}
	return nil
}
