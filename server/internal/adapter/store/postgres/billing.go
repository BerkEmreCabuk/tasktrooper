package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type BillingStore struct {
	pool *DB
}

func NewBillingStore(pool *DB) *BillingStore {
	return &BillingStore{pool: pool}
}

func (s *BillingStore) GetPlan(ctx context.Context) (domain.BillingPlan, error) {
	var p domain.BillingPlan
	err := s.pool.QueryRow(ctx, `
		SELECT name, usd_budget, period_days, period_start, display_token_rate, updated_at
		FROM billing_plan WHERE id = 1
	`).Scan(&p.Name, &p.UsdBudget, &p.PeriodDays, &p.PeriodStart, &p.DisplayTokenRate, &p.UpdatedAt)
	if err != nil {
		return domain.BillingPlan{}, fmt.Errorf("get billing plan: %w", err)
	}
	return p, nil
}

func (s *BillingStore) UpdatePlan(ctx context.Context, req domain.UpdateBillingPlanRequest) (domain.BillingPlan, error) {
	var p domain.BillingPlan
	err := s.pool.QueryRow(ctx, `
		UPDATE billing_plan SET
			name = COALESCE($1, name),
			usd_budget = COALESCE($2, usd_budget),
			period_days = COALESCE($3, period_days),
			display_token_rate = COALESCE($4, display_token_rate),
			updated_at = now()
		WHERE id = 1
		RETURNING name, usd_budget, period_days, period_start, display_token_rate, updated_at
	`, req.Name, req.UsdBudget, req.PeriodDays, req.DisplayTokenRate).
		Scan(&p.Name, &p.UsdBudget, &p.PeriodDays, &p.PeriodStart, &p.DisplayTokenRate, &p.UpdatedAt)
	if err != nil {
		return domain.BillingPlan{}, fmt.Errorf("update billing plan: %w", err)
	}
	return p, nil
}

func (s *BillingStore) SetPeriodStart(ctx context.Context, start time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE billing_plan SET period_start = $1, updated_at = now() WHERE id = 1`, start)
	if err != nil {
		return fmt.Errorf("set period start: %w", err)
	}
	return nil
}

func (s *BillingStore) ListModelPrices(ctx context.Context) ([]domain.ModelPrice, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT model, usd_per_1m_prompt, usd_per_1m_completion,
		       usd_per_1m_cache_read, usd_per_1m_cache_write, updated_at
		FROM model_prices ORDER BY model ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list model prices: %w", err)
	}
	defer rows.Close()
	var out []domain.ModelPrice
	for rows.Next() {
		var m domain.ModelPrice
		if err := rows.Scan(&m.Model, &m.UsdPer1MPrompt, &m.UsdPer1MCompletion,
			&m.UsdPer1MCacheRead, &m.UsdPer1MCacheWrite, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *BillingStore) UpsertModelPrice(ctx context.Context, price domain.ModelPrice) (domain.ModelPrice, error) {
	var m domain.ModelPrice
	err := s.pool.QueryRow(ctx, `
		INSERT INTO model_prices (model, usd_per_1m_prompt, usd_per_1m_completion,
			usd_per_1m_cache_read, usd_per_1m_cache_write, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (model) DO UPDATE SET
			usd_per_1m_prompt = EXCLUDED.usd_per_1m_prompt,
			usd_per_1m_completion = EXCLUDED.usd_per_1m_completion,
			usd_per_1m_cache_read = EXCLUDED.usd_per_1m_cache_read,
			usd_per_1m_cache_write = EXCLUDED.usd_per_1m_cache_write,
			updated_at = now()
		RETURNING model, usd_per_1m_prompt, usd_per_1m_completion,
		          usd_per_1m_cache_read, usd_per_1m_cache_write, updated_at
	`, price.Model, price.UsdPer1MPrompt, price.UsdPer1MCompletion,
		price.UsdPer1MCacheRead, price.UsdPer1MCacheWrite).
		Scan(&m.Model, &m.UsdPer1MPrompt, &m.UsdPer1MCompletion,
			&m.UsdPer1MCacheRead, &m.UsdPer1MCacheWrite, &m.UpdatedAt)
	if err != nil {
		return domain.ModelPrice{}, fmt.Errorf("upsert model price: %w", err)
	}
	return m, nil
}

func (s *BillingStore) DeleteModelPrice(ctx context.Context, model string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM model_prices WHERE model = $1`, model)
	return err
}

// usdSpentSinceSQL prices the period's usage. A model with no price row costs 0,
// which silently disables the budget gate (agents with no explicit model record
// under "(default)", which can never be priced by name). Fall back to the '*'
// catch-all row when there is no exact match, so an operator can price the long
// tail in one place.
//
// Cached tokens are priced separately because they cost differently: a cache
// read is a fraction of the input rate, a cache write a premium over it. Both
// columns are SUBSETS of prompt_tokens (see domain.Usage), so the base rate
// applies only to what is left after taking them out — adding them on top
// would bill the same tokens twice.
//
// A NULL cache rate resolves to the FULL prompt price, not to zero and not to
// some other row's discount. Unpriced must mean "charged normally": the failure
// mode of guessing low is a tenant who blows through their budget for free,
// which the gate can never detect afterwards.
const usdSpentSinceSQL = `
	SELECT COALESCE(SUM(
		-- GREATEST guards the subtraction: a provider that ever reports more
		-- cached tokens than prompt tokens must not mint negative spend.
		GREATEST(u.prompt_tokens - u.cache_read_tokens - u.cache_write_tokens, 0)::double precision / 1000000.0
			* rate.prompt
		+ u.cache_read_tokens::double precision / 1000000.0 * cache_rate.read
		+ u.cache_write_tokens::double precision / 1000000.0 * cache_rate.write
		+ u.completion_tokens::double precision / 1000000.0 * rate.completion
	), 0)
	FROM llm_usage u
	LEFT JOIN model_prices p ON p.model = u.model
	LEFT JOIN model_prices fallback ON fallback.model = '*'
	CROSS JOIN LATERAL (
		SELECT
			COALESCE(p.usd_per_1m_prompt, fallback.usd_per_1m_prompt, 0) AS prompt,
			COALESCE(p.usd_per_1m_completion, fallback.usd_per_1m_completion, 0) AS completion,
			-- Cache rates come from whichever row supplied the base rate. Letting
			-- an exact row with no cache price borrow the '*' row's would price
			-- one model's cache off another model's discount.
			CASE WHEN p.model IS NOT NULL
				THEN p.usd_per_1m_cache_read
				ELSE fallback.usd_per_1m_cache_read
			END AS raw_read,
			CASE WHEN p.model IS NOT NULL
				THEN p.usd_per_1m_cache_write
				ELSE fallback.usd_per_1m_cache_write
			END AS raw_write
	) rate
	CROSS JOIN LATERAL (
		SELECT
			COALESCE(rate.raw_read::double precision, rate.prompt) AS read,
			COALESCE(rate.raw_write::double precision, rate.prompt) AS write
	) cache_rate
	WHERE u.created_at >= $1
`

func (s *BillingStore) UsdSpentSince(ctx context.Context, since time.Time) (float64, error) {
	var usd float64
	if err := s.pool.QueryRow(ctx, usdSpentSinceSQL, since).Scan(&usd); err != nil {
		return 0, fmt.Errorf("usd spent since: %w", err)
	}
	return usd, nil
}

// The budget gate reaches for LockedUsdSpentSince through an optional interface
// (it is not part of port.BillingStore), so renaming it would silently drop the
// gate back to the unlocked read instead of breaking the build.
var _ interface {
	LockedUsdSpentSince(ctx context.Context, since time.Time) (float64, error)
} = (*BillingStore)(nil)

// LockedUsdSpentSince is UsdSpentSince evaluated while holding the plan row's
// write lock, which is what the budget gate calls. The gate is a read-then-
// compare with nothing in between to serialise it, so concurrent runs all read
// the same pre-spend total and all passed; taking billing_plan's row lock makes
// the gate one-at-a-time per tenant, so each run sees the usage every earlier
// run has already committed. Nothing else writes billing_plan on the request
// path (only the period roll and the tenant-manager push), so the lock costs a
// round trip, not contention.
func (s *BillingStore) LockedUsdSpentSince(ctx context.Context, since time.Time) (float64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("locked usd spent since: %w", err)
	}
	defer tx.Rollback(ctx)
	var planID int
	if err := tx.QueryRow(ctx, `SELECT id FROM billing_plan WHERE id = 1 FOR UPDATE`).Scan(&planID); err != nil {
		return 0, fmt.Errorf("lock billing plan: %w", err)
	}
	var usd float64
	err = tx.QueryRow(ctx, usdSpentSinceSQL, since).Scan(&usd)
	if err != nil {
		return 0, fmt.Errorf("locked usd spent since: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("locked usd spent since: %w", err)
	}
	return usd, nil
}

func (s *BillingStore) TokensSince(ctx context.Context, since time.Time) (int64, error) {
	var tokens int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(prompt_tokens + completion_tokens), 0)
		FROM llm_usage WHERE created_at >= $1
	`, since).Scan(&tokens)
	if err != nil {
		return 0, fmt.Errorf("tokens since: %w", err)
	}
	return tokens, nil
}

func (s *BillingStore) AddPausedTask(ctx context.Context, task domain.QuotaPausedTask) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO quota_paused_tasks (task_id, repository_id) VALUES ($1, $2)
		ON CONFLICT (task_id) DO NOTHING
	`, task.TaskID, task.RepositoryID)
	return err
}

func (s *BillingStore) ListPausedTasks(ctx context.Context) ([]domain.QuotaPausedTask, error) {
	rows, err := s.pool.Query(ctx, `SELECT task_id, repository_id, paused_at FROM quota_paused_tasks ORDER BY paused_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list paused tasks: %w", err)
	}
	defer rows.Close()
	var out []domain.QuotaPausedTask
	for rows.Next() {
		var t domain.QuotaPausedTask
		if err := rows.Scan(&t.TaskID, &t.RepositoryID, &t.PausedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *BillingStore) ClearPausedTasks(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM quota_paused_tasks`)
	return err
}

func (s *BillingStore) RemovePausedTask(ctx context.Context, taskID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM quota_paused_tasks WHERE task_id = $1`, taskID)
	return err
}
