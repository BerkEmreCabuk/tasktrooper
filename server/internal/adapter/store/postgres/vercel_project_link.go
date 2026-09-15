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

// VercelProjectLinkStore persists repository_vercel_projects (migration 129).
type VercelProjectLinkStore struct {
	pool *DB
}

func NewVercelProjectLinkStore(pool *DB) *VercelProjectLinkStore {
	return &VercelProjectLinkStore{pool: pool}
}

var _ port.VercelProjectLinkStore = (*VercelProjectLinkStore)(nil)

const vercelProjectLinkCols = `id, repository_id, sub_project_path, project_id, project_name, ` +
	`team_id, team_slug, framework, root_directory, production_url, created_at, updated_at`

func scanVercelProjectLink(row pgx.Row) (domain.VercelProjectLink, error) {
	var l domain.VercelProjectLink
	err := row.Scan(
		&l.ID, &l.RepositoryID, &l.SubProjectPath, &l.ProjectID, &l.ProjectName,
		&l.TeamID, &l.TeamSlug, &l.Framework, &l.RootDirectory, &l.ProductionURL,
		&l.CreatedAt, &l.UpdatedAt,
	)
	return l, err
}

func (s *VercelProjectLinkStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.VercelProjectLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+vercelProjectLinkCols+`
		FROM repository_vercel_projects WHERE repository_id = $1 ORDER BY sub_project_path`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list vercel project links: %w", err)
	}
	defer rows.Close()
	var out []domain.VercelProjectLink
	for rows.Next() {
		l, err := scanVercelProjectLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan vercel project link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *VercelProjectLinkStore) Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (domain.VercelProjectLink, error) {
	l, err := scanVercelProjectLink(s.pool.QueryRow(ctx, `SELECT `+vercelProjectLinkCols+`
		FROM repository_vercel_projects WHERE repository_id = $1 AND sub_project_path = $2`,
		repositoryID, subProjectPath))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.VercelProjectLink{}, fmt.Errorf("get vercel project link: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.VercelProjectLink{}, fmt.Errorf("get vercel project link: %w", err)
	}
	return l, nil
}

// Save upserts on the tenant-leading key migration 128 declares. Re-linking a
// path to a different project is an UPDATE of the same row rather than a
// second row, which is what keeps the binding single-valued: the details view
// asks "which project is web/" and there has to be one answer.
func (s *VercelProjectLinkStore) Save(ctx context.Context, in domain.VercelProjectLink) (domain.VercelProjectLink, error) {
	l, err := scanVercelProjectLink(s.pool.QueryRow(ctx, `
		INSERT INTO repository_vercel_projects
			(repository_id, sub_project_path, project_id, project_name, team_id, team_slug,
			 framework, root_directory, production_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (repository_id, sub_project_path) DO UPDATE SET
			project_id = EXCLUDED.project_id,
			project_name = EXCLUDED.project_name,
			team_id = EXCLUDED.team_id,
			team_slug = EXCLUDED.team_slug,
			framework = EXCLUDED.framework,
			root_directory = EXCLUDED.root_directory,
			production_url = EXCLUDED.production_url,
			updated_at = now()
		RETURNING `+vercelProjectLinkCols,
		in.RepositoryID, in.SubProjectPath, in.ProjectID, in.ProjectName, in.TeamID, in.TeamSlug,
		in.Framework, in.RootDirectory, in.ProductionURL))
	if err != nil {
		return domain.VercelProjectLink{}, fmt.Errorf("save vercel project link: %w", err)
	}
	return l, nil
}

// Delete removes one binding; a missing row is not an error.
func (s *VercelProjectLinkStore) Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) error {
	const q = `DELETE FROM repository_vercel_projects WHERE repository_id = $1 AND sub_project_path = $2`
	if _, err := s.pool.Exec(ctx, q, repositoryID, subProjectPath); err != nil {
		return fmt.Errorf("delete vercel project link: %w", err)
	}
	return nil
}
