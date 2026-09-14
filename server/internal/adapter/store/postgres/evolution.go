package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type EvolutionStore struct {
	pool *DB
}

func NewEvolutionStore(pool *DB) *EvolutionStore {
	return &EvolutionStore{pool: pool}
}

const reflectionColumns = "id, agent_id, trigger_kind, status, window_start, window_end, summary, performance_snapshot, raw_output, error, created_at, completed_at"

func (s *EvolutionStore) CreateReflection(ctx context.Context, r domain.AgentReflection) (domain.AgentReflection, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_reflections (agent_id, trigger_kind, status, window_start, window_end)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+reflectionColumns,
		r.AgentID, r.Trigger, r.Status, r.WindowStart, r.WindowEnd)
	out, err := scanReflection(row)
	if err != nil {
		return domain.AgentReflection{}, fmt.Errorf("create reflection: %w", err)
	}
	return out, nil
}

func (s *EvolutionStore) CompleteReflection(ctx context.Context, r domain.AgentReflection) error {
	var snapJSON []byte
	if r.PerformanceSnapshot != nil {
		var err error
		snapJSON, err = json.Marshal(r.PerformanceSnapshot)
		if err != nil {
			return err
		}
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_reflections
		SET status = $2, summary = $3, performance_snapshot = $4, raw_output = $5, error = $6, completed_at = now()
		WHERE id = $1
	`, r.ID, r.Status, r.Summary, snapJSON, nullIfEmpty(r.RawOutput), nullIfEmpty(r.Error))
	if err != nil {
		return fmt.Errorf("complete reflection: %w", err)
	}
	return nil
}

func (s *EvolutionStore) GetReflection(ctx context.Context, id uuid.UUID) (domain.AgentReflection, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+reflectionColumns+` FROM agent_reflections WHERE id = $1`, id)
	out, err := scanReflection(row)
	if err != nil {
		return domain.AgentReflection{}, fmt.Errorf("get reflection: %w", err)
	}
	return out, nil
}

func (s *EvolutionStore) ListReflections(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.AgentReflection, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+reflectionColumns+` FROM agent_reflections
		WHERE agent_id = $1
		ORDER BY created_at DESC LIMIT $2
	`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list reflections: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentReflection
	for rows.Next() {
		r, err := scanReflection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *EvolutionStore) LatestCompletedReflection(ctx context.Context, agentID uuid.UUID) (*domain.AgentReflection, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+reflectionColumns+` FROM agent_reflections
		WHERE agent_id = $1 AND status = $2
		ORDER BY created_at DESC LIMIT 1
	`, agentID, domain.ReflectionStatusCompleted)
	r, err := scanReflection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest completed reflection: %w", err)
	}
	return &r, nil
}

func (s *EvolutionStore) LatestReflectionByTrigger(ctx context.Context, agentID uuid.UUID, trigger string) (*domain.AgentReflection, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+reflectionColumns+` FROM agent_reflections
		WHERE agent_id = $1 AND trigger_kind = $2
		ORDER BY created_at DESC LIMIT 1
	`, agentID, trigger)
	r, err := scanReflection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest reflection by trigger: %w", err)
	}
	return &r, nil
}

const evolutionEventColumns = "id, reflection_id, agent_id, change_type, target_kind, target_id, target_name, before_state, after_state, reverted_event_id, score_at_change, impact, impact_evaluated_at, created_at"

func (s *EvolutionStore) CreateEvent(ctx context.Context, e domain.AgentEvolutionEvent) (domain.AgentEvolutionEvent, error) {
	if e.Impact == "" {
		e.Impact = domain.EvolutionImpactPending
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_evolution_events
			(reflection_id, agent_id, change_type, target_kind, target_id, target_name, before_state, after_state, reverted_event_id, score_at_change, impact)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+evolutionEventColumns,
		e.ReflectionID, e.AgentID, e.ChangeType, e.TargetKind, e.TargetID, e.TargetName,
		rawOrNil(e.Before), rawOrNil(e.After), e.RevertedEventID, e.ScoreAtChange, e.Impact)
	out, err := scanEvolutionEvent(row)
	if err != nil {
		return domain.AgentEvolutionEvent{}, fmt.Errorf("create evolution event: %w", err)
	}
	return out, nil
}

func (s *EvolutionStore) GetEvent(ctx context.Context, id uuid.UUID) (domain.AgentEvolutionEvent, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+evolutionEventColumns+` FROM agent_evolution_events WHERE id = $1`, id)
	out, err := scanEvolutionEvent(row)
	if err != nil {
		return domain.AgentEvolutionEvent{}, fmt.Errorf("get evolution event: %w", err)
	}
	return out, nil
}

func (s *EvolutionStore) ListEvents(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.AgentEvolutionEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+evolutionEventColumns+` FROM agent_evolution_events
		WHERE agent_id = $1
		ORDER BY created_at DESC LIMIT $2
	`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list evolution events: %w", err)
	}
	defer rows.Close()
	return scanEvolutionEvents(rows)
}

func (s *EvolutionStore) ListEventsByImpact(ctx context.Context, impact string, createdBefore time.Time, limit int) ([]domain.AgentEvolutionEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+evolutionEventColumns+` FROM agent_evolution_events
		WHERE impact = $1 AND created_at < $2
		ORDER BY created_at ASC LIMIT $3
	`, impact, createdBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("list events by impact: %w", err)
	}
	defer rows.Close()
	return scanEvolutionEvents(rows)
}

func (s *EvolutionStore) UpdateEventImpact(ctx context.Context, id uuid.UUID, impact string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_evolution_events SET impact = $2, impact_evaluated_at = now() WHERE id = $1
	`, id, impact)
	if err != nil {
		return fmt.Errorf("update event impact: %w", err)
	}
	return nil
}

func scanReflection(row pgx.Row) (domain.AgentReflection, error) {
	var r domain.AgentReflection
	var snapJSON []byte
	var rawOutput, errMsg *string
	if err := row.Scan(&r.ID, &r.AgentID, &r.Trigger, &r.Status, &r.WindowStart, &r.WindowEnd,
		&r.Summary, &snapJSON, &rawOutput, &errMsg, &r.CreatedAt, &r.CompletedAt); err != nil {
		return domain.AgentReflection{}, err
	}
	if len(snapJSON) > 0 {
		var snap domain.PerformanceSnapshot
		if json.Unmarshal(snapJSON, &snap) == nil {
			r.PerformanceSnapshot = &snap
		}
	}
	if rawOutput != nil {
		r.RawOutput = *rawOutput
	}
	if errMsg != nil {
		r.Error = *errMsg
	}
	return r, nil
}

func scanEvolutionEvent(row pgx.Row) (domain.AgentEvolutionEvent, error) {
	var e domain.AgentEvolutionEvent
	var before, after []byte
	if err := row.Scan(&e.ID, &e.ReflectionID, &e.AgentID, &e.ChangeType, &e.TargetKind,
		&e.TargetID, &e.TargetName, &before, &after, &e.RevertedEventID, &e.ScoreAtChange,
		&e.Impact, &e.ImpactEvaluatedAt, &e.CreatedAt); err != nil {
		return domain.AgentEvolutionEvent{}, err
	}
	e.Before = before
	e.After = after
	return e, nil
}

func scanEvolutionEvents(rows pgx.Rows) ([]domain.AgentEvolutionEvent, error) {
	var out []domain.AgentEvolutionEvent
	for rows.Next() {
		e, err := scanEvolutionEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func rawOrNil(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
