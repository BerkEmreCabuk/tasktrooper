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

// GCloudCredentialStore persists the single encrypted Google Cloud
// service-account credential.
type GCloudCredentialStore struct {
	pool *DB
}

func NewGCloudCredentialStore(pool *DB) *GCloudCredentialStore {
	return &GCloudCredentialStore{pool: pool}
}

var _ port.GCloudCredentialStore = (*GCloudCredentialStore)(nil)

// Set writes the whole credential. gcloud_credentials holds one row (id = 1,
// the column default): one Google Cloud connection per install, unlike
// store_credentials which is keyed by provider.
func (s *GCloudCredentialStore) Set(ctx context.Context, projectID, clientEmail string, encrypted []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gcloud_credentials (project_id, client_email, data)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET
			project_id   = EXCLUDED.project_id,
			client_email = EXCLUDED.client_email,
			data         = EXCLUDED.data,
			updated_at   = now()
	`, projectID, clientEmail, encrypted)
	if err != nil {
		return fmt.Errorf("set gcloud credential: %w", err)
	}
	return nil
}

func (s *GCloudCredentialStore) Get(ctx context.Context) (port.GCloudCredentialRow, error) {
	var row port.GCloudCredentialRow
	err := s.pool.QueryRow(ctx, `
		SELECT project_id, client_email, data, updated_at FROM gcloud_credentials
	`).Scan(&row.ProjectID, &row.ClientEmail, &row.Data, &row.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return port.GCloudCredentialRow{}, fmt.Errorf("get gcloud credential: %w", port.ErrNotFound)
	}
	if err != nil {
		return port.GCloudCredentialRow{}, fmt.Errorf("get gcloud credential: %w", err)
	}
	return row, nil
}

func (s *GCloudCredentialStore) Delete(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM gcloud_credentials`); err != nil {
		return fmt.Errorf("delete gcloud credential: %w", err)
	}
	return nil
}

// GCloudResourceStore persists the per-(repository, sub-project) binding to a
// Google Cloud resource.
type GCloudResourceStore struct {
	pool *DB
}

func NewGCloudResourceStore(pool *DB) *GCloudResourceStore {
	return &GCloudResourceStore{pool: pool}
}

var _ port.GCloudResourceStore = (*GCloudResourceStore)(nil)

const gcloudResourceCols = `id, repository_id, sub_project_path, resource_type, resource_name,
	display_name, project_id, location, source, created_at, updated_at`

func scanGCloudResource(row pgx.Row) (domain.GCloudResourceBinding, error) {
	var b domain.GCloudResourceBinding
	if err := row.Scan(
		&b.ID, &b.RepositoryID, &b.SubProjectPath, &b.ResourceType, &b.ResourceName,
		&b.DisplayName, &b.ProjectID, &b.Location, &b.Source, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return domain.GCloudResourceBinding{}, err
	}
	return b, nil
}

// Save upserts on (repository_id, sub_project_path).
func (s *GCloudResourceStore) Save(ctx context.Context, binding domain.GCloudResourceBinding) (domain.GCloudResourceBinding, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO repository_gcloud_resources
			(repository_id, sub_project_path, resource_type, resource_name, display_name, project_id, location, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (repository_id, sub_project_path) DO UPDATE SET
			resource_type = EXCLUDED.resource_type,
			resource_name = EXCLUDED.resource_name,
			display_name  = EXCLUDED.display_name,
			project_id    = EXCLUDED.project_id,
			location      = EXCLUDED.location,
			source        = EXCLUDED.source,
			updated_at    = now()
		RETURNING `+gcloudResourceCols,
		binding.RepositoryID, binding.SubProjectPath, binding.ResourceType, binding.ResourceName,
		binding.DisplayName, binding.ProjectID, binding.Location, binding.Source,
	)
	saved, err := scanGCloudResource(row)
	if err != nil {
		return domain.GCloudResourceBinding{}, fmt.Errorf("save gcloud resource binding: %w", err)
	}
	return saved, nil
}

func (s *GCloudResourceStore) Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (domain.GCloudResourceBinding, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+gcloudResourceCols+`
		FROM repository_gcloud_resources
		WHERE repository_id = $1 AND sub_project_path = $2
	`, repositoryID, subProjectPath)
	binding, err := scanGCloudResource(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GCloudResourceBinding{}, fmt.Errorf("get gcloud resource binding: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.GCloudResourceBinding{}, fmt.Errorf("get gcloud resource binding: %w", err)
	}
	return binding, nil
}

func (s *GCloudResourceStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.GCloudResourceBinding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+gcloudResourceCols+`
		FROM repository_gcloud_resources
		WHERE repository_id = $1
		ORDER BY sub_project_path
	`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list gcloud resource bindings: %w", err)
	}
	defer rows.Close()

	out := []domain.GCloudResourceBinding{}
	for rows.Next() {
		binding, err := scanGCloudResource(rows)
		if err != nil {
			return nil, fmt.Errorf("scan gcloud resource binding: %w", err)
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func (s *GCloudResourceStore) Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM repository_gcloud_resources WHERE repository_id = $1 AND sub_project_path = $2
	`, repositoryID, subProjectPath)
	if err != nil {
		return fmt.Errorf("delete gcloud resource binding: %w", err)
	}
	return nil
}
