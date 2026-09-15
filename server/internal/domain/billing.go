package domain

import (
	"time"

	"github.com/google/uuid"
)

// Billing model.
//
// The real limit is a per-period USD budget (everyone on the same package gets
// the same USD budget). Each model has its own internal credit structure
// (ModelPrice, USD per 1M tokens), so token usage converts to a USD cost. The
// user, however, is shown TOKENS, not dollars: BillingStatus renders the USD
// budget/spend as a token figure via DisplayTokenRate (USD per token).
//
// Package definitions are global and admin-editable (their authoritative home is
// the control plane); BillingPlan below is the effective snapshot the tenant pod
// enforces against and can also be edited via the tenant admin API.

// BillingPlan is the tenant's effective plan. UsdBudget == 0 means unlimited
// (the safe default, so billing is inert until a real plan is assigned).
type BillingPlan struct {
	Name        string    `json:"name"`
	UsdBudget   float64   `json:"usd_budget"`
	PeriodDays  int       `json:"period_days"`
	PeriodStart time.Time `json:"period_start"`
	// DisplayTokenRate is USD per single token, used ONLY to render the USD
	// budget/spend as a token count for the user. Models differ in real price;
	// this is a stable presentation rate, not a billing rate.
	DisplayTokenRate float64   `json:"display_token_rate"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Unlimited reports whether the plan imposes no USD budget.
func (p BillingPlan) Unlimited() bool { return p.UsdBudget <= 0 }

// ResetAt is when the current period ends and the budget renews.
func (p BillingPlan) ResetAt() time.Time {
	days := p.PeriodDays
	if days <= 0 {
		days = 30
	}
	return p.PeriodStart.AddDate(0, 0, days)
}

// ModelPrice is our internal per-model credit structure: USD per 1M tokens,
// split prompt/completion.
type ModelPrice struct {
	Model              string  `json:"model"`
	UsdPer1MPrompt     float64 `json:"usd_per_1m_prompt"`
	UsdPer1MCompletion float64 `json:"usd_per_1m_completion"`
	// Cache rates are pointers because absent and zero mean opposite things:
	// nil is "this model has no separate cache rate" and bills those tokens at
	// the full prompt price, while a literal 0 would make them free. An
	// operator (or the control plane) that omits the field must not get a
	// silent discount.
	UsdPer1MCacheRead  *float64  `json:"usd_per_1m_cache_read,omitempty"`
	UsdPer1MCacheWrite *float64  `json:"usd_per_1m_cache_write,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

// BillingStatus is the /v1/billing payload. USD figures are internal detail;
// the token_* fields are what the UI shows to the user.
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

// QuotaPausedTask records a task halted because the budget was exhausted, so the
// resumer can re-dispatch it when the period rolls over.
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
