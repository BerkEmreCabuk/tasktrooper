package kpi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestKPIAttainmentLowerBetter(t *testing.T) {
	cases := []struct {
		value float64
		want  float64
	}{
		{2, 1.0}, // bugs < 3 → tam puan
		{4, 0.5}, // <= 5 → yarım puan
		{5, 0.5}, // user example: 5 bug → yarım
		{6, 0},   // üstü → sıfır
		{0, 1.0},
	}
	for _, c := range cases {
		got := domain.KPIAttainment(domain.KPIDirectionLowerBetter, c.value, 2, 5)
		if got != c.want {
			t.Errorf("lower_better value=%v: got %v want %v", c.value, got, c.want)
		}
	}
}

func TestKPIAttainmentHigherBetter(t *testing.T) {
	cases := []struct {
		value float64
		want  float64
	}{
		{3, 1.0},
		{5, 1.0},
		{1, 0.5},
		{2, 0.5},
		{0, 0},
	}
	for _, c := range cases {
		got := domain.KPIAttainment(domain.KPIDirectionHigherBetter, c.value, 3, 1)
		if got != c.want {
			t.Errorf("higher_better value=%v: got %v want %v", c.value, got, c.want)
		}
	}
}

func TestPeriodBoundsWeekly(t *testing.T) {
	// Wednesday 2026-07-08 → ISO week starts Monday 2026-07-06.
	now := time.Date(2026, 7, 8, 15, 30, 0, 0, time.UTC)
	from, to := PeriodBounds(domain.KPIPeriodWeekly, now)
	if from != time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC) {
		t.Errorf("weekly from = %v", from)
	}
	if to != time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC) {
		t.Errorf("weekly to = %v", to)
	}
	// Sunday belongs to the week that started the previous Monday.
	sunday := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	from, _ = PeriodBounds(domain.KPIPeriodWeekly, sunday)
	if from != time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC) {
		t.Errorf("sunday weekly from = %v", from)
	}
}

func TestPeriodBoundsDailyMonthly(t *testing.T) {
	now := time.Date(2026, 7, 8, 15, 30, 0, 0, time.UTC)
	from, to := PeriodBounds(domain.KPIPeriodDaily, now)
	if from.Day() != 8 || to.Sub(from) != 24*time.Hour {
		t.Errorf("daily bounds = %v..%v", from, to)
	}
	from, to = PeriodBounds(domain.KPIPeriodMonthly, now)
	if from != time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) || to != time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("monthly bounds = %v..%v", from, to)
	}
}

func TestMetricByKeyRejectsUnknown(t *testing.T) {
	if _, err := MetricByKey("coffee_consumed"); err == nil {
		t.Fatal("expected error for untrackable metric")
	}
	if _, err := MetricByKey("tasks_completed"); err != nil {
		t.Fatalf("expected tasks_completed to be trackable: %v", err)
	}
	// ListMetrics drives the metric picker in the UI, so a metric added to the
	// registry and left out of the ordered list would be untrackable in
	// practice while looking fine here.
	if len(ListMetrics()) != len(metricRegistry) {
		t.Errorf("ListMetrics covers %d of %d registry metrics", len(ListMetrics()), len(metricRegistry))
	}
}

func TestCompositeScore(t *testing.T) {
	k1 := domain.AgentKPI{ID: uuid.New(), Weight: 1, Enabled: true}
	k2 := domain.AgentKPI{ID: uuid.New(), Weight: 3, Enabled: true}
	disabled := domain.AgentKPI{ID: uuid.New(), Weight: 10, Enabled: false}
	results := []domain.AgentKPIResult{
		{KPIID: k1.ID, Attainment: 1.0},
		{KPIID: k2.ID, Attainment: 0.5},
		{KPIID: disabled.ID, Attainment: 0},
	}
	got := CompositeScore([]domain.AgentKPI{k1, k2, disabled}, results)
	want := (1.0*1 + 3*0.5) / 4 * 100 // 62.5
	if got != want {
		t.Errorf("composite = %v want %v", got, want)
	}
	if CompositeScore(nil, nil) != 0 {
		t.Error("empty composite should be 0")
	}
}
