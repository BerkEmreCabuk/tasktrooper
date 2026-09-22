package domain

import (
	"time"

	"github.com/google/uuid"
)

// Billing model. The real limit is a per-period USD budget; each model has its
// own ModelPrice (USD per 1M tokens), so token usage converts to a USD cost.
// The user is shown TOKENS, not dollars, via DisplayTokenRate (USD per token).
// BillingPlan is the single row this install enforces against.

// BillingPlan is the effective plan. UsdBudget == 0 means unlimited (the safe
// default, so billing is inert until a real plan is assigned).
type BillingPlan struct {
	Name        string    `json:"name"`
	UsdBudget   float64   `json:"usd_budget"`
	PeriodDays  int       `json:"period_days"`
	PeriodStart time.Time `json:"period_start"`
	// USD per single token, used ONLY to render budget/spend as a token count;
	// a stable presentation rate, not a billing rate.
	DisplayTokenRate float64   `json:"display_token_rate"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (p BillingPlan) Unlimited() bool { return p.UsdBudget <= 0 }

func (p BillingPlan) ResetAt() time.Time {
	days := p.PeriodDays
	if days <= 0 {
		days = 30
	}
	return p.PeriodStart.AddDate(0, 0, days)
}

// ModelPrice is the internal per-model credit structure: USD per 1M tokens,
// split prompt/completion.
type ModelPrice struct {
	Model              string  `json:"model"`
	UsdPer1MPrompt     float64 `json:"usd_per_1m_prompt"`
	UsdPer1MCompletion float64 `json:"usd_per_1m_completion"`
	// Pointers because absent and zero mean opposite things: nil bills cache
	// tokens at the full prompt price, a literal 0 makes them free.
	UsdPer1MCacheRead  *float64  `json:"usd_per_1m_cache_read,omitempty"`
	UsdPer1MCacheWrite *float64  `json:"usd_per_1m_cache_write,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

// BillingStatus is the /v1/billing payload; the token_* fields are what the UI
// shows, the USD figures are internal detail.
type BillingStatus struct {
	PlanName        string    `json:"plan_name"`
	Unlimited       bool      `json:"unlimited"`
	UsdBudget       float64   `json:"usd_budget"`
	UsdSpent        float64   `json:"usd_spent"`
	TokenBudget     int64     `json:"token_budget"`
	TokenBudgetUsed int64     `json:"token_budget_used"`
	TokenRemaining  int64     `json:"token_remaining"`
	RawTokensUsed   int64     `json:"raw_tokens_used"`
	Exhausted       bool      `json:"exhausted"`
	PeriodStart     time.Time `json:"period_start"`
	ResetAt         time.Time `json:"reset_at"`
}

// QuotaPausedTask records a task halted because the budget was exhausted, so
// the resumer can re-dispatch it when the period rolls over.
type QuotaPausedTask struct {
	TaskID       uuid.UUID `json:"task_id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	PausedAt     time.Time `json:"paused_at"`
}

type UpdateBillingPlanRequest struct {
	Name             *string  `json:"name,omitempty"`
	UsdBudget        *float64 `json:"usd_budget,omitempty"`
	PeriodDays       *int     `json:"period_days,omitempty"`
	DisplayTokenRate *float64 `json:"display_token_rate,omitempty"`
}
