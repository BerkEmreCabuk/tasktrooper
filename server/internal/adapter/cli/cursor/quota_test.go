package cursor

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
			name: "the reported cursor-agent sentence is a usage limit",
			text: "Error: You've hit your usage limit",
			want: now.Add(domain.DefaultQuotaParkWindow),
		},
		{
			name: "a future reset date in the reported M/D/YYYY format is honoured",
			text: "Error: You've hit your usage limit. Your usage limits will reset when your monthly cycle ends on 12/25/2026.",
			want: time.Date(2026, 12, 25, 0, 0, 0, 0, time.Local),
		},
		{
			name: "a reset date already in the past falls back to the default window",
			text: "Error: You've hit your usage limit. Your usage limits will reset when your monthly cycle ends on 1/1/2020.",
			want: now.Add(domain.DefaultQuotaParkWindow),
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
			assert.Equal(t, "sess-1", block.CLISessionID)
			assert.Equal(t, domain.LLMProviderCursorAgent, block.Provider)
			assert.NotEmpty(t, block.Detail)
			assert.True(t, block.ResumeAt.After(now), "a park must always be in the future or the sweeper spins")
		})
	}
}

// The stderr tail is checked too: a session that dies before its "result"
// event still carries the limit text on stderr.
func TestQuotaBlockFromChecksStderr(t *testing.T) {
	block := quotaBlockFrom(outcome{}, "Error: You've hit your usage limit", "sess-2", time.Now())
	require.NotNil(t, block)
	assert.Equal(t, "sess-2", block.CLISessionID)
}

func TestQuotaDetailPicksTheLineThatSaysWhy(t *testing.T) {
	text := "some build noise\nError: You've hit your usage limit\nmore noise"
	assert.Equal(t, "Error: You've hit your usage limit", quotaDetail(text))
}
