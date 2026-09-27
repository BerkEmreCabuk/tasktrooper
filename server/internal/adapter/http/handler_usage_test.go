package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// days=N must return exactly N local calendar days including today: since is
// midnight N-1 days back, from/to bracket that window in the requested tz.
func TestUsageWindow_DaysIncludesTodayInRequestedTimezone(t *testing.T) {
	// Istanbul is a fixed UTC+3 (no DST since 2016): this UTC instant is still
	// 2026-09-26 in UTC but already 2026-09-27 01:00 in Istanbul, so "today"
	// must come out different depending on which tz is requested.
	now := time.Date(2026, 9, 26, 22, 0, 0, 0, time.UTC)

	days, loc, since, from, to := usageWindow(7, "Europe/Istanbul", now)
	assert.Equal(t, 7, days)
	require.Equal(t, "Europe/Istanbul", loc.String())
	assert.Equal(t, "2026-09-27", to)
	assert.Equal(t, "2026-09-21", from)

	wantSince := time.Date(2026, 9, 21, 0, 0, 0, 0, loc)
	assert.True(t, since.Equal(wantSince), "since = %s, want %s", since, wantSince)
}

func TestUsageWindow_EmptyTzDefaultsToUTC(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	_, loc, _, from, to := usageWindow(1, "", now)
	assert.Equal(t, "UTC", loc.String())
	assert.Equal(t, "2026-01-15", from)
	assert.Equal(t, "2026-01-15", to)
}

func TestUsageWindow_InvalidTzFallsBackToUTC(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	for _, tz := range []string{"Not/A/Zone", "Local", "../../etc/passwd"} {
		_, loc, _, _, _ := usageWindow(1, tz, now)
		assert.Equal(t, "UTC", loc.String(), "tz=%q", tz)
	}
}

func TestUsageWindow_OutOfRangeDaysDefaultsTo30(t *testing.T) {
	now := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)
	_, _, since30, from, _ := usageWindow(30, "", now)

	for _, days := range []int{0, -5, 366} {
		gotDays, _, since, gotFrom, _ := usageWindow(days, "", now)
		assert.Equal(t, 30, gotDays, "days=%d must be reported as the window actually served", days)
		assert.True(t, since.Equal(since30), "days=%d should behave like days=30", days)
		assert.Equal(t, from, gotFrom)
	}
}
