package antigravity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestQuotaBlockFrom(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		text     string
		want     time.Time
		wantNone bool
	}{
		{
			name: "the reported agy sentence with its own countdown is a quota block",
			text: "⚠ Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 143h57m55s.",
			want: now.Add(143*time.Hour + 57*time.Minute + 55*time.Second),
		},
		{
			name: "the reported RESOURCE_EXHAUSTED relay is a quota block too",
			text: "RESOURCE_EXHAUSTED (code 429): Individual quota reached. Contact your administrator to enable overages. Resets in 167h39m40s.",
			want: now.Add(167*time.Hour + 39*time.Minute + 40*time.Second),
		},
		{
			name: "no countdown falls back to the default window",
			text: "Individual quota reached.",
			want: now.Add(domain.DefaultQuotaParkWindow),
		},
		{
			name:     "an unrelated failure is not a quota block",
			text:     "Error: ENOENT: no such file or directory, open 'go.mod'",
			wantNone: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block := quotaBlockFrom(outcome{Text: tc.text}, "", "sess-1", now)
			if tc.wantNone {
				assert.Nil(t, block, "only a quota message may park a task")
				return
			}
			require.NotNil(t, block)
			assert.True(t, tc.want.Equal(block.ResumeAt), "resume at = %s, want %s", block.ResumeAt, tc.want)
			assert.Equal(t, "sess-1", block.CLISessionID)
			assert.Equal(t, domain.LLMProviderAntigravity, block.Provider)
			assert.NotEmpty(t, block.Detail)
		})
	}
}

// The duration format AGY reports is already Go's own syntax; a partial
// countdown (no hours, say) must still parse.
func TestParseResetsIn(t *testing.T) {
	d, ok := parseResetsIn("Resets in 143h57m55s.")
	require.True(t, ok)
	assert.Equal(t, 143*time.Hour+57*time.Minute+55*time.Second, d)

	d, ok = parseResetsIn("Resets in 45m.")
	require.True(t, ok)
	assert.Equal(t, 45*time.Minute, d)

	_, ok = parseResetsIn("no countdown here")
	assert.False(t, ok)
}
