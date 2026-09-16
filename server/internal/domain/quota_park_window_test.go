package domain_test

import (
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A repeated 30m guess on a 5h window re-hits the same limit every half hour;
// the escalation has to double toward that window's own length and then hold,
// never overshoot it.
func TestQuotaParkWindowEscalatesAndCaps(t *testing.T) {
	tests := []struct {
		consecutiveParks int
		want             time.Duration
	}{
		{0, 30 * time.Minute},
		{1, time.Hour},
		{2, 2 * time.Hour},
		{3, 4 * time.Hour},
		{4, 5 * time.Hour},
		{5, 5 * time.Hour},
		{20, 5 * time.Hour},
		{-1, 30 * time.Minute},
	}
	for _, tc := range tests {
		if got := domain.QuotaParkWindow(tc.consecutiveParks); got != tc.want {
			t.Errorf("QuotaParkWindow(%d) = %s, want %s", tc.consecutiveParks, got, tc.want)
		}
	}
}
