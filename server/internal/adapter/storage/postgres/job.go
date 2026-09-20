package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type JobStore struct {
	pool *DB
}

func NewJobStore(pool *DB) *JobStore {
	return &JobStore{pool: pool}
}

func (j *JobStore) Create(ctx context.Context, request []byte, callbackURL string) (domain.Job, error) {
	var job domain.Job
	err := j.pool.QueryRow(ctx, `
		INSERT INTO jobs (request, callback_url, status)
		VALUES ($1, $2, $3)
		RETURNING id, status, request, callback_url, created_at, updated_at
	`, request, callbackURL, domain.JobStatusPending).Scan(
		&job.ID, &job.Status, &job.Request, &job.CallbackURL, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return domain.Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

func (j *JobStore) Get(ctx context.Context, id uuid.UUID) (domain.Job, error) {
	var job domain.Job
	err := j.pool.QueryRow(ctx, `
		SELECT id, status, request, result, error, callback_url, created_at, updated_at, started_at, completed_at
		FROM jobs WHERE id = $1
	`, id).Scan(
		&job.ID, &job.Status, &job.Request, &job.Result, &job.Error, &job.CallbackURL,
		&job.CreatedAt, &job.UpdatedAt, &job.StartedAt, &job.CompletedAt,
	)
	if err != nil {
		return domain.Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (j *JobStore) List(ctx context.Context, status string, limit int) ([]domain.Job, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows pgx.Rows
	var err error
	if status != "" {
		rows, err = j.pool.Query(ctx, `
			SELECT id, status, request, result, error, callback_url, created_at, updated_at, started_at, completed_at
			FROM jobs WHERE status = $1 ORDER BY created_at DESC LIMIT $2
		`, status, limit)
	} else {
		rows, err = j.pool.Query(ctx, `
			SELECT id, status, request, result, error, callback_url, created_at, updated_at, started_at, completed_at
			FROM jobs ORDER BY created_at DESC LIMIT $1
		`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	var jobs []domain.Job
	for rows.Next() {
		var job domain.Job
		if err := rows.Scan(
			&job.ID, &job.Status, &job.Request, &job.Result, &job.Error, &job.CallbackURL,
			&job.CreatedAt, &job.UpdatedAt, &job.StartedAt, &job.CompletedAt,
		); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (j *JobStore) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.JobStatus, result []byte, errMsg string) error {
	now := time.Now()
	var startedAt, completedAt *time.Time
	switch status {
	case domain.JobStatusRunning:
		startedAt = &now
	case domain.JobStatusCompleted, domain.JobStatusFailed, domain.JobStatusCancelled:
		completedAt = &now
	}

	_, err := j.pool.Exec(ctx, `
		UPDATE jobs SET status = $2, result = $3, error = $4, updated_at = $5,
			started_at = COALESCE($6, started_at), completed_at = COALESCE($7, completed_at)
		WHERE id = $1
	`, id, status, result, errMsg, now, startedAt, completedAt)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	return nil
}

func (j *JobStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := j.pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job not found")
	}
	return nil
}

func (j *JobStore) ClaimPending(ctx context.Context) (*domain.Job, error) {
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var job domain.Job
	err = tx.QueryRow(ctx, `
		SELECT id, status, request, callback_url, created_at, updated_at
		FROM jobs WHERE status = $1
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`, domain.JobStatusPending).Scan(
		&job.ID, &job.Status, &job.Request, &job.CallbackURL, &job.CreatedAt, &job.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}

	now := time.Now()
	_, err = tx.Exec(ctx, `
		UPDATE jobs SET status = $2, started_at = $3, updated_at = $3 WHERE id = $1
	`, job.ID, domain.JobStatusRunning, now)
	if err != nil {
		return nil, fmt.Errorf("mark running: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	job.Status = domain.JobStatusRunning
	job.StartedAt = &now
	return &job, nil
}
