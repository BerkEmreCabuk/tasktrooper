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

// --- DeploymentRunStore ---

type DeploymentRunStore struct {
	pool *DB
}

func NewDeploymentRunStore(pool *DB) *DeploymentRunStore {
	return &DeploymentRunStore{pool: pool}
}

const deploymentRunCols = `id, repository_id, env, run_id, run_number, workflow_file, head_sha, head_ref,
	status, conclusion, html_url, trigger_source, triggered_by, rollback_of_sha,
	started_at, completed_at, created_at, updated_at`

func scanDeploymentRun(row pgx.Row) (domain.DeploymentRun, error) {
	var r domain.DeploymentRun
	if err := row.Scan(
		&r.ID, &r.RepositoryID, &r.Env, &r.RunID, &r.RunNumber, &r.WorkflowFile, &r.HeadSHA, &r.HeadRef,
		&r.Status, &r.Conclusion, &r.HTMLURL, &r.TriggerSource, &r.TriggeredBy, &r.RollbackOfSHA,
		&r.StartedAt, &r.CompletedAt, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return domain.DeploymentRun{}, err
	}
	return r, nil
}

func collectDeploymentRuns(rows pgx.Rows) ([]domain.DeploymentRun, error) {
	var out []domain.DeploymentRun
	for rows.Next() {
		r, err := scanDeploymentRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan deployment run: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// The attribution-preserving upsert — COALESCE(NULLIF(EXCLUDED.x, ”), table.x)
// is what stops a later poll (which does not know trigger_source/triggered_by/
// rollback_of_sha) from undoing what the dispatch reconciler stamped.
const upsertDeploymentRunSQL = `
INSERT INTO deployment_runs (repository_id, env, run_id, run_number, workflow_file,
    head_sha, head_ref, status, conclusion, html_url, trigger_source, triggered_by,
    rollback_of_sha, started_at, completed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (tenant_id, repository_id, run_id) DO UPDATE SET
    status = EXCLUDED.status,
    conclusion = EXCLUDED.conclusion,
    run_number = EXCLUDED.run_number,
    head_sha = EXCLUDED.head_sha,
    head_ref = EXCLUDED.head_ref,
    html_url = EXCLUDED.html_url,
    started_at = EXCLUDED.started_at,
    completed_at = EXCLUDED.completed_at,
    -- Reconciliation writes these; a later poll must never blank them.
    trigger_source = CASE WHEN EXCLUDED.trigger_source = 'external'
                          THEN deployment_runs.trigger_source ELSE EXCLUDED.trigger_source END,
    triggered_by = COALESCE(NULLIF(EXCLUDED.triggered_by, ''), deployment_runs.triggered_by),
    rollback_of_sha = COALESCE(NULLIF(EXCLUDED.rollback_of_sha, ''), deployment_runs.rollback_of_sha),
    updated_at = now()
RETURNING ` + deploymentRunCols

// LatestAll uses DISTINCT ON — one row per (repository, env), newest first.
const latestAllDeploymentRunsSQL = `
SELECT DISTINCT ON (repository_id, env) ` + deploymentRunCols + `
FROM deployment_runs
ORDER BY repository_id, env, started_at DESC NULLS LAST, created_at DESC`

const lastSuccessfulBeforeSQL = `
SELECT ` + deploymentRunCols + `
FROM deployment_runs
WHERE repository_id = $1 AND env = $2 AND conclusion = 'success' AND head_sha <> '' AND head_sha <> $3
ORDER BY completed_at DESC NULLS LAST, created_at DESC LIMIT 1`

// Upsert inserts or updates on (repository_id, run_id). An empty
// TriggerSource is normalized to "external" before the query runs — that is
// both the domain default for "a run this system did not knowingly cause"
// and the exact sentinel the ON CONFLICT CASE above checks for, so a poller
// that never sets TriggerSource can never clobber a reconciled attribution.
func (s *DeploymentRunStore) Upsert(ctx context.Context, run domain.DeploymentRun) (domain.DeploymentRun, error) {
	triggerSource := run.TriggerSource
	if triggerSource == "" {
		triggerSource = domain.TriggerSourceExternal
	}
	row := s.pool.QueryRow(ctx, upsertDeploymentRunSQL,
		run.RepositoryID, run.Env, run.RunID, run.RunNumber, run.WorkflowFile,
		run.HeadSHA, run.HeadRef, run.Status, run.Conclusion, run.HTMLURL,
		triggerSource, run.TriggeredBy, run.RollbackOfSHA, run.StartedAt, run.CompletedAt)
	out, err := scanDeploymentRun(row)
	if err != nil {
		return domain.DeploymentRun{}, fmt.Errorf("upsert deployment run: %w", err)
	}
	return out, nil
}

func (s *DeploymentRunStore) ByRunID(ctx context.Context, repositoryID uuid.UUID, runID int64) (domain.DeploymentRun, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+deploymentRunCols+`
		FROM deployment_runs WHERE repository_id = $1 AND run_id = $2`, repositoryID, runID)
	run, err := scanDeploymentRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeploymentRun{}, fmt.Errorf("deployment run by run id: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.DeploymentRun{}, fmt.Errorf("deployment run by run id: %w", err)
	}
	return run, nil
}

func (s *DeploymentRunStore) Latest(ctx context.Context, repositoryID uuid.UUID, env string) (domain.DeploymentRun, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+deploymentRunCols+`
		FROM deployment_runs WHERE repository_id = $1 AND env = $2
		ORDER BY started_at DESC NULLS LAST, created_at DESC LIMIT 1`, repositoryID, env)
	run, err := scanDeploymentRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeploymentRun{}, fmt.Errorf("latest deployment run: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.DeploymentRun{}, fmt.Errorf("latest deployment run: %w", err)
	}
	return run, nil
}

func (s *DeploymentRunStore) LatestAll(ctx context.Context) ([]domain.DeploymentRun, error) {
	rows, err := s.pool.Query(ctx, latestAllDeploymentRunsSQL)
	if err != nil {
		return nil, fmt.Errorf("latest all deployment runs: %w", err)
	}
	defer rows.Close()
	return collectDeploymentRuns(rows)
}

func (s *DeploymentRunStore) ListByEnv(ctx context.Context, repositoryID uuid.UUID, env string, limit int) ([]domain.DeploymentRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deploymentRunCols+`
		FROM deployment_runs WHERE repository_id = $1 AND env = $2
		ORDER BY started_at DESC NULLS LAST, created_at DESC LIMIT $3`, repositoryID, env, limit)
	if err != nil {
		return nil, fmt.Errorf("list deployment runs by env: %w", err)
	}
	defer rows.Close()
	return collectDeploymentRuns(rows)
}

func (s *DeploymentRunStore) LastSuccessfulBefore(ctx context.Context, repositoryID uuid.UUID, env, excludeSHA string) (domain.DeploymentRun, error) {
	row := s.pool.QueryRow(ctx, lastSuccessfulBeforeSQL, repositoryID, env, excludeSHA)
	run, err := scanDeploymentRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeploymentRun{}, fmt.Errorf("last successful deployment run: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.DeploymentRun{}, fmt.Errorf("last successful deployment run: %w", err)
	}
	return run, nil
}

func (s *DeploymentRunStore) Stamp(ctx context.Context, repositoryID uuid.UUID, runID int64, triggerSource, triggeredBy, rollbackOfSHA string) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE deployment_runs SET
			trigger_source = $1, triggered_by = $2, rollback_of_sha = $3, updated_at = now()
		WHERE repository_id = $4 AND run_id = $5`,
		triggerSource, triggeredBy, rollbackOfSHA, repositoryID, runID); err != nil {
		return fmt.Errorf("stamp deployment run: %w", err)
	}
	return nil
}

// --- DeployDispatchStore ---

type DeployDispatchStore struct {
	pool *DB
}

func NewDeployDispatchStore(pool *DB) *DeployDispatchStore {
	return &DeployDispatchStore{pool: pool}
}

const deployDispatchCols = `id, repository_id, env, workflow_file, ref, kind, rollback_of_sha, actor, state, matched_run_id, created_at`

func scanDeployDispatch(row pgx.Row) (domain.DeployDispatch, error) {
	var d domain.DeployDispatch
	if err := row.Scan(
		&d.ID, &d.RepositoryID, &d.Env, &d.WorkflowFile, &d.Ref, &d.Kind, &d.RollbackOfSHA,
		&d.Actor, &d.State, &d.MatchedRunID, &d.CreatedAt,
	); err != nil {
		return domain.DeployDispatch{}, err
	}
	return d, nil
}

func (s *DeployDispatchStore) Create(ctx context.Context, d domain.DeployDispatch) (domain.DeployDispatch, error) {
	state := d.State
	if state == "" {
		state = domain.DispatchStatePending
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO deploy_dispatches (repository_id, env, workflow_file, ref, kind, rollback_of_sha, actor, state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+deployDispatchCols,
		d.RepositoryID, d.Env, d.WorkflowFile, d.Ref, d.Kind, d.RollbackOfSHA, d.Actor, state)
	out, err := scanDeployDispatch(row)
	if err != nil {
		return domain.DeployDispatch{}, fmt.Errorf("create deploy dispatch: %w", err)
	}
	return out, nil
}

func (s *DeployDispatchStore) ListPending(ctx context.Context) ([]domain.DeployDispatch, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deployDispatchCols+`
		FROM deploy_dispatches WHERE state = $1 ORDER BY created_at`, domain.DispatchStatePending)
	if err != nil {
		return nil, fmt.Errorf("list pending deploy dispatches: %w", err)
	}
	defer rows.Close()
	var out []domain.DeployDispatch
	for rows.Next() {
		d, err := scanDeployDispatch(rows)
		if err != nil {
			return nil, fmt.Errorf("scan deploy dispatch: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *DeployDispatchStore) Resolve(ctx context.Context, id uuid.UUID, state string, runID *int64) error {
	if _, err := s.pool.Exec(ctx, `UPDATE deploy_dispatches SET state = $1, matched_run_id = $2 WHERE id = $3`,
		state, runID, id); err != nil {
		return fmt.Errorf("resolve deploy dispatch: %w", err)
	}
	return nil
}

// --- OpsAuditStore ---

type OpsAuditStore struct {
	pool *DB
}

func NewOpsAuditStore(pool *DB) *OpsAuditStore {
	return &OpsAuditStore{pool: pool}
}

const opsAuditCols = `id, repository_id, action, target, actor, detail, outcome, error, created_at`

func scanOpsAuditEntry(row pgx.Row) (domain.OpsAuditEntry, error) {
	var e domain.OpsAuditEntry
	var detailJSON []byte
	if err := row.Scan(
		&e.ID, &e.RepositoryID, &e.Action, &e.Target, &e.Actor, &detailJSON, &e.Outcome, &e.Error, &e.CreatedAt,
	); err != nil {
		return domain.OpsAuditEntry{}, err
	}
	e.Detail = map[string]string{}
	_ = json.Unmarshal(detailJSON, &e.Detail)
	return e, nil
}

func (s *OpsAuditStore) Log(ctx context.Context, entry domain.OpsAuditEntry) error {
	detail := entry.Detail
	if detail == nil {
		detail = map[string]string{}
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal ops audit detail: %w", err)
	}
	outcome := entry.Outcome
	if outcome == "" {
		outcome = domain.OpsOutcomeOK
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO ops_audit_log (repository_id, action, target, actor, detail, outcome, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		entry.RepositoryID, entry.Action, entry.Target, entry.Actor, detailJSON, outcome, entry.Error,
	); err != nil {
		return fmt.Errorf("log ops audit entry: %w", err)
	}
	return nil
}

func (s *OpsAuditStore) List(ctx context.Context, repositoryID *uuid.UUID, limit int) ([]domain.OpsAuditEntry, error) {
	var rows pgx.Rows
	var err error
	if repositoryID != nil {
		rows, err = s.pool.Query(ctx, `SELECT `+opsAuditCols+`
			FROM ops_audit_log WHERE repository_id = $1 ORDER BY created_at DESC LIMIT $2`, *repositoryID, limit)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT `+opsAuditCols+`
			FROM ops_audit_log ORDER BY created_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list ops audit entries: %w", err)
	}
	defer rows.Close()
	var out []domain.OpsAuditEntry
	for rows.Next() {
		e, err := scanOpsAuditEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ops audit entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
