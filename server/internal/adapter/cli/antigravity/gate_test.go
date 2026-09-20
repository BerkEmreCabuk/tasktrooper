package antigravity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestQuotaGateParksWithoutSpawning(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	ex := &Executor{now: func() time.Time { return now }}

	req := domain.TaskExecution{TaskKey: "task-1"}
	assert.Nil(t, ex.gatedQuotaBlock(req), "an unarmed gate must never park")

	block := &domain.QuotaBlock{ResumeAt: now.Add(30 * time.Minute), Detail: "quota reached"}
	ex.armQuotaGate(block)

	until, armed := ex.QuotaGate()
	require.True(t, armed)
	assert.True(t, until.Equal(block.ResumeAt))

	req.ResumeSessionID = "sess-limit-9"
	gated := ex.gatedQuotaBlock(req)
	require.NotNil(t, gated)
	assert.Equal(t, "sess-limit-9", gated.CLISessionID,
		"the gate's own block must carry the caller's resume id through, or a re-parked task loses its session")
	assert.Equal(t, domain.LLMProviderAntigravity, gated.Provider)

	ex.clearQuotaGate()
	assert.Nil(t, ex.gatedQuotaBlock(req), "a cleared gate must stop parking")
}

func TestArmQuotaGateOnlyExtends(t *testing.T) {
	now := time.Now()
	ex := &Executor{now: func() time.Time { return now }}

	ex.armQuotaGate(&domain.QuotaBlock{ResumeAt: now.Add(time.Hour)})
	ex.armQuotaGate(&domain.QuotaBlock{ResumeAt: now.Add(10 * time.Minute)})

	until, armed := ex.QuotaGate()
	require.True(t, armed)
	assert.True(t, until.Equal(now.Add(time.Hour)), "an earlier estimate must never shorten the gate")
}
