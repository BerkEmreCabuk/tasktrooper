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

// DeployPackageStore persists release trains (deploy_packages) and their
// ordered membership (deploy_package_tasks).
type DeployPackageStore struct {
	pool *DB
}

func NewDeployPackageStore(pool *DB) *DeployPackageStore {
	return &DeployPackageStore{pool: pool}
}

const deployPackageCols = `id, repository_id, name, COALESCE(description, ''), status, COALESCE(note, ''), created_at, updated_at`

func scanDeployPackage(row pgx.Row) (domain.DeployPackage, error) {
	var p domain.DeployPackage
	if err := row.Scan(
		&p.ID, &p.RepositoryID, &p.Name, &p.Description, &p.Status, &p.Note,
		&p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return domain.DeployPackage{}, err
	}
	return p, nil
}

func (s *DeployPackageStore) Create(ctx context.Context, pkg domain.DeployPackage) (domain.DeployPackage, error) {
	status := pkg.Status
	if status == "" {
		status = domain.DeployPackageStatusDraft
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO deploy_packages (repository_id, name, description, status, note)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+deployPackageCols,
		pkg.RepositoryID, pkg.Name, pkg.Description, status, pkg.Note)
	out, err := scanDeployPackage(row)
	if err != nil {
		return domain.DeployPackage{}, fmt.Errorf("create deploy package: %w", err)
	}
	return out, nil
}

func (s *DeployPackageStore) Get(ctx context.Context, repositoryID, packageID uuid.UUID) (domain.DeployPackage, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+deployPackageCols+`
		FROM deploy_packages WHERE id = $1 AND repository_id = $2`, packageID, repositoryID)
	out, err := scanDeployPackage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeployPackage{}, fmt.Errorf("deploy package %s: %w", packageID, port.ErrNotFound)
	}
	if err != nil {
		return domain.DeployPackage{}, fmt.Errorf("get deploy package: %w", err)
	}
	return out, nil
}

func (s *DeployPackageStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployPackage, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deployPackageCols+`
		FROM deploy_packages WHERE repository_id = $1 ORDER BY created_at DESC`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list deploy packages: %w", err)
	}
	defer rows.Close()
	var out []domain.DeployPackage
	for rows.Next() {
		p, scanErr := scanDeployPackage(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan deploy package: %w", scanErr)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Update applies only the non-nil fields. COALESCE on the parameter rather than
// building the SQL string keeps this one statement: the release path moves
// status and note while a human may be renaming the same row.
func (s *DeployPackageStore) Update(ctx context.Context, repositoryID, packageID uuid.UUID, name, description, status, note *string) (domain.DeployPackage, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE deploy_packages SET
			name        = COALESCE($3, name),
			description = COALESCE($4, description),
			status      = COALESCE($5, status),
			note        = COALESCE($6, note),
			updated_at  = now()
		WHERE id = $1 AND repository_id = $2
		RETURNING `+deployPackageCols,
		packageID, repositoryID, name, description, status, note)
	out, err := scanDeployPackage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeployPackage{}, fmt.Errorf("deploy package %s: %w", packageID, port.ErrNotFound)
	}
	if err != nil {
		return domain.DeployPackage{}, fmt.Errorf("update deploy package: %w", err)
	}
	return out, nil
}

func (s *DeployPackageStore) Delete(ctx context.Context, repositoryID, packageID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM deploy_packages WHERE id = $1 AND repository_id = $2`, packageID, repositoryID)
	if err != nil {
		return fmt.Errorf("delete deploy package: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("deploy package %s: %w", packageID, port.ErrNotFound)
	}
	return nil
}

// ReplaceTasks sets the full membership in one transaction. position is the
// index in taskIDs, so the caller's array order IS the declared order.
func (s *DeployPackageStore) ReplaceTasks(ctx context.Context, packageID uuid.UUID, taskIDs []uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM deploy_package_tasks WHERE package_id = $1`, packageID); err != nil {
		return fmt.Errorf("clear deploy package tasks: %w", err)
	}
	seen := make(map[uuid.UUID]bool, len(taskIDs))
	for i, taskID := range taskIDs {
		if taskID == uuid.Nil || seen[taskID] {
			continue
		}
		seen[taskID] = true
		if _, err := tx.Exec(ctx, `
			INSERT INTO deploy_package_tasks (package_id, task_id, position)
			VALUES ($1, $2, $3)
		`, packageID, taskID, i); err != nil {
			return fmt.Errorf("insert deploy package task: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// ListTasks returns membership with each task's board identity joined in, so a
// caller rendering a package does not issue one task query per member.
// Released is left false here: production evidence lives in task_pipelines and
// is the service's call, not the store's.
func (s *DeployPackageStore) ListTasks(ctx context.Context, packageID uuid.UUID) ([]domain.DeployPackageTask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT dpt.task_id, dpt.position,
			`+taskKeySQL+`,
			bt.title, bt.board_column
		FROM deploy_package_tasks dpt
		JOIN board_tasks bt ON bt.id = dpt.task_id
		WHERE dpt.package_id = $1
		ORDER BY dpt.position ASC
	`, packageID)
	if err != nil {
		return nil, fmt.Errorf("list deploy package tasks: %w", err)
	}
	defer rows.Close()
	var out []domain.DeployPackageTask
	for rows.Next() {
		var t domain.DeployPackageTask
		var col string
		if err := rows.Scan(&t.TaskID, &t.Position, &t.Key, &t.Title, &col); err != nil {
			return nil, fmt.Errorf("scan deploy package task: %w", err)
		}
		t.Column = domain.TaskColumn(col)
		out = append(out, t)
	}
	return out, rows.Err()
}
