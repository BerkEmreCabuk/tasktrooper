package claudecode

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestQuotaBlockFrom(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	// The epoch the CLI's historical format carries, well after `now`.
	resetEpoch := now.Add(3 * time.Hour).Unix()

	tests := []struct {
		name     string
		text     string
		want     time.Time
		wantNone bool
	}{
		{
			name: "the epoch the CLI appends is the reset time",
			text: "Claude AI usage limit reached|" + strconv.FormatInt(resetEpoch, 10),
			want: time.Unix(resetEpoch, 0),
		},
		{
			name: "no reset time falls back to the default window",
			text: "Claude AI usage limit reached",
			want: now.Add(domain.DefaultQuotaParkWindow),
		},
		{
			name: "an epoch already in the past falls back too, so the sweeper cannot spin",
			text: "Claude AI usage limit reached|1000000000",
			want: now.Add(domain.DefaultQuotaParkWindow),
		},
		{
			name:     "a plain rate-limit message is not a usage limit",
			text:     "API Error: 429 Too Many Requests, please slow down",
			wantNone: true,
		},
		{
			name:     "an unrelated failure is not a usage limit",
			text:     "Error: ENOENT: no such file or directory, open 'go.mod'",
			wantNone: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block := quotaBlockFrom(outcome{Text: tc.text}, "", "sess-1", now)
			if tc.wantNone {
				assert.Nil(t, block, "only a usage limit may park a task")
				return
			}
			require.NotNil(t, block)
			assert.True(t, tc.want.Equal(block.ResumeAt), "resume at = %s, want %s", block.ResumeAt, tc.want)
			assert.Equal(t, "sess-1", block.CLISessionID, "the parked session must travel with the block")
			assert.NotEmpty(t, block.Detail)
			assert.True(t, block.ResumeAt.After(now), "a park must always be in the future or the sweeper spins")
		})
	}
}

// Milliseconds and seconds are both plausible epochs; the digit width is what
// tells them apart. Reading a millisecond epoch as seconds would park the task
// until the year 57000.
func TestParseResetEpochUnits(t *testing.T) {
	seconds, ok := parseResetEpoch("usage limit reached|1755400000")
	require.True(t, ok)
	assert.Equal(t, int64(1755400000), seconds.Unix())

	millis, ok := parseResetEpoch("usage limit reached|1755400000000")
	require.True(t, ok)
	assert.Equal(t, int64(1755400000), millis.Unix())

	_, ok = parseResetEpoch("usage limit reached")
	assert.False(t, ok)
}

// The detail on the board card must be the line that mentions the limit, not
// whatever else was in the buffer the message was found in.
func TestQuotaDetailPicksTheLineThatSaysWhy(t *testing.T) {
	text := "go: downloading example.com/foo\nsome build noise\nClaude AI usage limit reached|1755400000\nmore noise"
	assert.Equal(t, "Claude AI usage limit reached|1755400000", quotaDetail(text))
}
