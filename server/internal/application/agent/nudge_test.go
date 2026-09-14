package agent_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/stretchr/testify/assert"
)

func TestRepeatNudgeGivesAnAlternativeAndTheCost(t *testing.T) {
	msg := agent.RepeatNudgeMessageForTest("run_terminal", 2)

	assert.Contains(t, msg, "run_terminal")
	assert.Contains(t, msg, "3 times")
	assert.Contains(t, msg, "Empty output")
	assert.Contains(t, msg, "Read the file")
	assert.Contains(t, msg, "summarise")
	assert.Contains(t, msg, "no longer shown")
}

func TestRepeatNudgeEscalatesAsTheAbortApproaches(t *testing.T) {
	abort := agent.RepeatAbortThresholdForTest()

	early := agent.RepeatNudgeMessageForTest("run_terminal", 2)
	last := agent.RepeatNudgeMessageForTest("run_terminal", abort-1)

	assert.Contains(t, early, "2 more")
	assert.Contains(t, last, "1 more")
	assert.Contains(t, last, "stopped")
	assert.NotEqual(t, early, last)
}
