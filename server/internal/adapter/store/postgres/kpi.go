package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type KPIStore struct {
	pool *DB
}

func NewKPIStore(pool *DB) *KPIStore {
	return &KPIStore{pool: pool}
}

const kpiColumns = "id, agent_id, metric_key, name, description, period, target_full, target_half, weight, enabled, created_at, updated_at"

func (s *KPIStore) CreateKPI(ctx context.Context, k domain.AgentKPI) (domain.AgentKPI, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_kpis (agent_id, metric_key, name, description, period, target_full, target_half, weight, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+kpiColumns,
		k.AgentID, k.MetricKey, k.Name, k.Description, k.Period, k.TargetFull, k.TargetHalf, k.Weight, k.Enabled)
	out, err := scanKPI(row)
	if err != nil {
		return domain.AgentKPI{}, fmt.Errorf("create kpi: %w", err)
	}
	return out, nil
}

func (s *KPIStore) UpdateKPI(ctx context.Context, k domain.AgentKPI) (domain.AgentKPI, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE agent_kpis
		SET metric_key=$2, name=$3, description=$4, period=$5, target_full=$6, target_half=$7, weight=$8, enabled=$9, updated_at=now()
		WHERE id=$1
		RETURNING `+kpiColumns,
		k.ID, k.MetricKey, k.Name, k.Description, k.Period, k.TargetFull, k.TargetHalf, k.Weight, k.Enabled)
	out, err := scanKPI(row)
	if err != nil {
		return domain.AgentKPI{}, fmt.Errorf("update kpi: %w", err)
	}
	return out, nil
}

func (s *KPIStore) DeleteKPI(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM agent_kpis WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete kpi: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("kpi not found")
	}
	return nil
}

func (s *KPIStore) GetKPI(ctx context.Context, id uuid.UUID) (domain.AgentKPI, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+kpiColumns+` FROM agent_kpis WHERE id = $1`, id)
	out, err := scanKPI(row)
	if err != nil {
		return domain.AgentKPI{}, fmt.Errorf("get kpi: %w", err)
	}
	return out, nil
}

func (s *KPIStore) ListByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.AgentKPI, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+kpiColumns+` FROM agent_kpis WHERE agent_id = $1 ORDER BY created_at ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list kpis: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentKPI
	for rows.Next() {
		k, err := scanKPI(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

const kpiResultColumns = "id, kpi_id, agent_id, period_start, period_end, measured_value, attainment, computed_at"

func (s *KPIStore) UpsertResult(ctx context.Context, r domain.AgentKPIResult) (domain.AgentKPIResult, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_kpi_results (kpi_id, agent_id, period_start, period_end, measured_value, attainment)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, kpi_id, period_start) DO UPDATE SET
			measured_value = EXCLUDED.measured_value,
			attainment     = EXCLUDED.attainment,
			period_end     = EXCLUDED.period_end,
			computed_at    = now()
		RETURNING `+kpiResultColumns,
		r.KPIID, r.AgentID, r.PeriodStart, r.PeriodEnd, r.MeasuredValue, r.Attainment)
	out, err := scanKPIResult(row)
	if err != nil {
		return domain.AgentKPIResult{}, fmt.Errorf("upsert kpi result: %w", err)
	}
	return out, nil
}

func (s *KPIStore) ListResults(ctx context.Context, agentID uuid.UUID, from, to time.Time) ([]domain.AgentKPIResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+kpiResultColumns+` FROM agent_kpi_results
		WHERE agent_id = $1 AND period_start >= $2 AND period_start < $3
		ORDER BY period_start DESC
	`, agentID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list kpi results: %w", err)
	}
	defer rows.Close()
	return scanKPIResults(rows)
}

func (s *KPIStore) LatestResults(ctx context.Context, agentID uuid.UUID) ([]domain.AgentKPIResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (kpi_id) `+kpiResultColumns+` FROM agent_kpi_results
		WHERE agent_id = $1
		ORDER BY kpi_id, period_start DESC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("latest kpi results: %w", err)
	}
	defer rows.Close()
	return scanKPIResults(rows)
}

func scanKPI(row pgx.Row) (domain.AgentKPI, error) {
	var k domain.AgentKPI
	if err := row.Scan(&k.ID, &k.AgentID, &k.MetricKey, &k.Name, &k.Description, &k.Period,
		&k.TargetFull, &k.TargetHalf, &k.Weight, &k.Enabled, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return domain.AgentKPI{}, err
	}
	return k, nil
}

func scanKPIResult(row pgx.Row) (domain.AgentKPIResult, error) {
	var r domain.AgentKPIResult
	if err := row.Scan(&r.ID, &r.KPIID, &r.AgentID, &r.PeriodStart, &r.PeriodEnd,
		&r.MeasuredValue, &r.Attainment, &r.ComputedAt); err != nil {
		return domain.AgentKPIResult{}, err
	}
	return r, nil
}

func scanKPIResults(rows pgx.Rows) ([]domain.AgentKPIResult, error) {
	var out []domain.AgentKPIResult
	for rows.Next() {
		r, err := scanKPIResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
