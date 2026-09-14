package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGoalIntake_Valid(t *testing.T) {
	raw := `{"ready":true,"purpose":"Learn Go","goal":"Explain interfaces","constraints":["concise"],"questions":[]}`
	intake, err := orchestrator.ParseGoalIntakeForTest(raw)
	require.NoError(t, err)
	assert.True(t, intake.Ready)
	assert.Equal(t, "Learn Go", intake.Purpose)
	assert.Equal(t, "Explain interfaces", intake.Goal)
	assert.Equal(t, []string{"concise"}, intake.Constraints)
}

func TestParseGoalIntake_ClarificationNeeded(t *testing.T) {
	raw := `{"ready":false,"purpose":"","goal":"","constraints":[],"questions":[{"id":"q1","prompt":"Which module?","options":[{"id":"auth","label":"Auth"},{"id":"other","label":"Other"}]}]}`
	intake, err := orchestrator.ParseGoalIntakeForTest(raw)
	require.NoError(t, err)
	assert.False(t, intake.Ready)
	assert.Len(t, intake.Questions, 1)
}

func TestParseGoalIntake_MissingPurpose(t *testing.T) {
	_, err := orchestrator.ParseGoalIntakeForTest(`{"ready":true,"purpose":"","goal":"x","constraints":[],"questions":[]}`)
	assert.Error(t, err)
}

func TestParseGoalIntake_MissingConstraintsField(t *testing.T) {
	_, err := orchestrator.ParseGoalIntakeForTest(`{"ready":true,"purpose":"p","goal":"g","questions":[]}`)
	assert.Error(t, err)
}

func TestBuildIntakeSystemPrompt_ProductManagerPolicy(t *testing.T) {
	prompt := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{
		SoloAgentName:        "product-manager",
		SoloAgentDescription: "PM agent",
		Lang:                 "tr",
	})
	assert.Contains(t, prompt, "product-manager")
	assert.Contains(t, prompt, "PM agent")
	assert.Contains(t, prompt, "Turkish")
	assert.Contains(t, prompt, "full conversation thread")
}

func TestBuildIntakeSystemPrompt_EnglishLocale(t *testing.T) {
	prompt := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{Lang: "en"})
	assert.Contains(t, prompt, "English")
}
