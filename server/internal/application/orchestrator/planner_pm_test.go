package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/stretchr/testify/assert"
)

// Solo mode plans only the single agent's own tasks and never wakes other team agents.
func TestBuildPlannerPMSoloPrompt_ConstrainedToSingleAgent(t *testing.T) {
	prompt := orchestrator.BuildPlannerPMSoloPromptForTest("tr")
	assert.Contains(t, prompt, "Solo mode: all tasks must use agent_id=")
	assert.Contains(t, prompt, "only")
	assert.Contains(t, prompt, "Decompose according to that agent's rules and skills")
}
