package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	KPIPeriodDaily   = "daily"
	KPIPeriodWeekly  = "weekly"
	KPIPeriodMonthly = "monthly"

	KPIDirectionLowerBetter  = "lower_better"
	KPIDirectionHigherBetter = "higher_better"
)

func ValidKPIPeriod(p string) bool {
	switch p {
	case KPIPeriodDaily, KPIPeriodWeekly, KPIPeriodMonthly:
		return true
	}
	return false
}

type AgentKPI struct {
	ID          uuid.UUID `json:"id"`
	AgentID     uuid.UUID `json:"agent_id"`
	MetricKey   string    `json:"metric_key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Period      string    `json:"period"`
	TargetFull  float64   `json:"target_full"`
	TargetHalf  float64   `json:"target_half"`
	Weight      float64   `json:"weight"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AgentKPIResult struct {
	ID            uuid.UUID `json:"id"`
	KPIID         uuid.UUID `json:"kpi_id"`
	AgentID       uuid.UUID `json:"agent_id"`
	PeriodStart   time.Time `json:"period_start"`
	PeriodEnd     time.Time `json:"period_end"`
	MeasuredValue float64   `json:"measured_value"`
	Attainment    float64   `json:"attainment"`
	ComputedAt    time.Time `json:"computed_at"`
}

type KPIMetricInfo struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	Direction   string `json:"direction"`
}

type CreateKPIRequest struct {
	MetricKey   string  `json:"metric_key"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Period      string  `json:"period"`
	TargetFull  float64 `json:"target_full"`
	TargetHalf  float64 `json:"target_half"`
	Weight      float64 `json:"weight"`
	Enabled     bool    `json:"enabled"`
}

type UpdateKPIRequest struct {
	MetricKey   string  `json:"metric_key"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Period      string  `json:"period"`
	TargetFull  float64 `json:"target_full"`
	TargetHalf  float64 `json:"target_half"`
	Weight      float64 `json:"weight"`
	Enabled     bool    `json:"enabled"`
}

// KPIAttainment scores a measured value against full/half thresholds.
// lower_better: value <= full -> 1.0, value <= half -> 0.5, else 0.
// higher_better: value >= full -> 1.0, value >= half -> 0.5, else 0.
func KPIAttainment(direction string, value, targetFull, targetHalf float64) float64 {
	if direction == KPIDirectionHigherBetter {
		switch {
		case value >= targetFull:
			return 1.0
		case value >= targetHalf:
			return 0.5
		default:
			return 0
		}
	}
	switch {
	case value <= targetFull:
		return 1.0
	case value <= targetHalf:
		return 0.5
	default:
		return 0
	}
}
