package cursor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// One session's usage limit must gate every OTHER session on this executor,
// without spawning a process to find out, and a re-parked task must not lose
// the CLI session it would resume.
func TestQuotaGateParksWithoutSpawning(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	ex := &Executor{now: func() time.Time { return now }}

	req := domain.TaskExecution{TaskKey: "task-1"}
	assert.Nil(t, ex.gatedQuotaBlock(req), "an unarmed gate must never park")

	block := &domain.QuotaBlock{ResumeAt: now.Add(30 * time.Minute), Detail: "usage limit reached"}
	ex.armQuotaGate(block)

	until, armed := ex.QuotaGate()
	require.True(t, armed)
	assert.True(t, until.Equal(block.ResumeAt))

	req.ResumeSessionID = "sess-limit-9"
	gated := ex.gatedQuotaBlock(req)
	require.NotNil(t, gated)
	assert.Equal(t, "sess-limit-9", gated.CLISessionID,
		"the gate's own block must carry the caller's resume id through, or a re-parked task loses its session")
	assert.Equal(t, domain.LLMProviderCursorAgent, gated.Provider)

	ex.clearQuotaGate()
	assert.Nil(t, ex.gatedQuotaBlock(req), "a cleared gate must stop parking")
}

// The gate only ever extends, never shortens: a session that started before
// the first park must not overwrite an already-later estimate with an
// earlier one.
func TestArmQuotaGateOnlyExtends(t *testing.T) {
	now := time.Now()
	ex := &Executor{now: func() time.Time { return now }}

	ex.armQuotaGate(&domain.QuotaBlock{ResumeAt: now.Add(time.Hour)})
	ex.armQuotaGate(&domain.QuotaBlock{ResumeAt: now.Add(10 * time.Minute)})

	until, armed := ex.QuotaGate()
	require.True(t, armed)
	assert.True(t, until.Equal(now.Add(time.Hour)), "an earlier estimate must never shorten the gate")
}
