package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// RepositoryPipelineJobStore persists the per-(sub-project, sub-repo-kind,
// category) mapping of pipeline categories to GitHub Actions jobs/workflows.
type RepositoryPipelineJobStore struct {
	pool *DB
}

func NewRepositoryPipelineJobStore(pool *DB) *RepositoryPipelineJobStore {
	return &RepositoryPipelineJobStore{pool: pool}
}

const repoPipelineJobCols = `id, repository_id, sub_project_path, sub_repo_kind, category, target_kind, target_ref, auto_detected`

func scanRepositoryPipelineJob(row interface{ Scan(dest ...any) error }) (domain.RepositoryPipelineJob, error) {
	var j domain.RepositoryPipelineJob
	err := row.Scan(&j.ID, &j.RepositoryID, &j.SubProjectPath, &j.SubRepoKind, &j.Category, &j.TargetKind, &j.TargetRef, &j.AutoDetected)
	return j, err
}

func (s *RepositoryPipelineJobStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+repoPipelineJobCols+` FROM repository_pipeline_jobs
		WHERE repository_id = $1
		ORDER BY sub_project_path, sub_repo_kind, category
	`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository pipeline jobs: %w", err)
	}
	defer rows.Close()
	var out []domain.RepositoryPipelineJob
	for rows.Next() {
		j, err := scanRepositoryPipelineJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ListAll feeds the operations matrix, which needs every repository's
// mapping in one query rather than one query per repository.
func (s *RepositoryPipelineJobStore) ListAll(ctx context.Context) ([]domain.RepositoryPipelineJob, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+repoPipelineJobCols+` FROM repository_pipeline_jobs
		ORDER BY repository_id, sub_project_path, sub_repo_kind, category
	`)
	if err != nil {
		return nil, fmt.Errorf("list all repository pipeline jobs: %w", err)
	}
	defer rows.Close()
	var out []domain.RepositoryPipelineJob
	for rows.Next() {
		j, err := scanRepositoryPipelineJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ReplaceForRepository atomically replaces the full mapping set for a repo —
// every sub-project's slots are submitted together in one save, so this
// still deletes and reinserts the whole repository's rows, not just one
// sub-project's. Rows with an empty TargetRef are dropped (an unset/ambiguous
// slot is simply "no mapping", which the pipeline treats as skip).
func (s *RepositoryPipelineJobStore) ReplaceForRepository(ctx context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM repository_pipeline_jobs WHERE repository_id = $1`, repositoryID); err != nil {
		return nil, err
	}
	var out []domain.RepositoryPipelineJob
	for _, j := range jobs {
		if j.TargetRef == "" {
			continue
		}
		targetKind := j.TargetKind
		if targetKind == "" {
			targetKind = domain.PipelineTargetJob
		}
		saved, err := scanRepositoryPipelineJob(tx.QueryRow(ctx, `
			INSERT INTO repository_pipeline_jobs (repository_id, sub_project_path, sub_repo_kind, category, target_kind, target_ref, auto_detected)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING `+repoPipelineJobCols,
			repositoryID, j.SubProjectPath, j.SubRepoKind, j.Category, targetKind, j.TargetRef, j.AutoDetected,
		))
		if err != nil {
			return nil, fmt.Errorf("insert repository pipeline job: %w", err)
		}
		out = append(out, saved)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
