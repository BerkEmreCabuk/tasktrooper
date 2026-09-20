package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type DeployTargetStore struct {
	pool *DB
}

func NewDeployTargetStore(pool *DB) *DeployTargetStore {
	return &DeployTargetStore{pool: pool}
}

const deployTargetCols = `id, repository_id, sub_project_path, env, provider, template_id, vars, health_url, logs_url, base_url, app_package, app_url, auto_rollback, created_at, updated_at`

func scanDeployTarget(row pgx.Row) (domain.DeployTarget, error) {
	var t domain.DeployTarget
	var varsJSON []byte
	if err := row.Scan(
		&t.ID, &t.RepositoryID, &t.SubProjectPath, &t.Env, &t.Provider, &t.TemplateID, &varsJSON,
		&t.HealthURL, &t.LogsURL, &t.BaseURL, &t.AppPackage, &t.AppURL, &t.AutoRollback, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return domain.DeployTarget{}, err
	}
	t.Vars = map[string]string{}
	_ = json.Unmarshal(varsJSON, &t.Vars)
	return t, nil
}

func (s *DeployTargetStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deployTargetCols+`
		FROM repository_deploy_targets WHERE repository_id = $1 ORDER BY sub_project_path, env`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list deploy targets: %w", err)
	}
	defer rows.Close()
	return collectDeployTargets(rows)
}

func (s *DeployTargetStore) ListAll(ctx context.Context) ([]domain.DeployTarget, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deployTargetCols+`
		FROM repository_deploy_targets ORDER BY repository_id, sub_project_path, env`)
	if err != nil {
		return nil, fmt.Errorf("list all deploy targets: %w", err)
	}
	defer rows.Close()
	return collectDeployTargets(rows)
}

func collectDeployTargets(rows pgx.Rows) ([]domain.DeployTarget, error) {
	var out []domain.DeployTarget
	for rows.Next() {
		t, err := scanDeployTarget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan deploy target: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *DeployTargetStore) Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error) {
	t, err := scanDeployTarget(s.pool.QueryRow(ctx, `SELECT `+deployTargetCols+`
		FROM repository_deploy_targets WHERE repository_id = $1 AND sub_project_path = $2 AND env = $3`,
		repositoryID, subProjectPath, env))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeployTarget{}, fmt.Errorf("get deploy target: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.DeployTarget{}, fmt.Errorf("get deploy target: %w", err)
	}
	return t, nil
}

func (s *DeployTargetStore) Save(ctx context.Context, in domain.DeployTarget) (domain.DeployTarget, error) {
	vars := in.Vars
	if vars == nil {
		vars = map[string]string{}
	}
	varsJSON, err := json.Marshal(vars)
	if err != nil {
		return domain.DeployTarget{}, fmt.Errorf("marshal deploy vars: %w", err)
	}
	t, err := scanDeployTarget(s.pool.QueryRow(ctx, `
		INSERT INTO repository_deploy_targets (repository_id, sub_project_path, env, provider, template_id, vars, health_url, logs_url, base_url, app_package, app_url, auto_rollback)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (repository_id, sub_project_path, env) DO UPDATE SET
			provider = EXCLUDED.provider,
			template_id = EXCLUDED.template_id,
			vars = EXCLUDED.vars,
			health_url = EXCLUDED.health_url,
			logs_url = EXCLUDED.logs_url,
			base_url = EXCLUDED.base_url,
			app_package = EXCLUDED.app_package,
			app_url = EXCLUDED.app_url,
			auto_rollback = EXCLUDED.auto_rollback,
			updated_at = now()
		RETURNING `+deployTargetCols,
		in.RepositoryID, in.SubProjectPath, in.Env, in.Provider, in.TemplateID, varsJSON, in.HealthURL, in.LogsURL, in.BaseURL,
		in.AppPackage, in.AppURL, in.AutoRollback))
	if err != nil {
		return domain.DeployTarget{}, fmt.Errorf("save deploy target: %w", err)
	}
	return t, nil
}

func (s *DeployTargetStore) Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM repository_deploy_targets WHERE repository_id = $1 AND sub_project_path = $2 AND env = $3`,
		repositoryID, subProjectPath, env); err != nil {
		return fmt.Errorf("delete deploy target: %w", err)
	}
	return nil
}
