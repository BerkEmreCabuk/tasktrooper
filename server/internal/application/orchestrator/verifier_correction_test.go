package orchestrator_test

import (
	"context"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unlike intake/planner/replanner, the verifier used to retry a parse
// failure with the exact same messages — the model saw no hint anything was
// wrong and had no better odds on attempt two. This locks the fix: a first
// unparseable response is followed by the shared pipelineCorrection message
// (echoing the bad response, then "Your response could not be parsed"), the
// same mechanism every other pipeline stage already used.
func TestVerifierEvaluate_RetriesWithCorrectionAfterUnparseableResponse(t *testing.T) {
	llm := &attemptLLM{
		replies: []string{
			"not json at all",
			`{"passed":true,"issues":[],"summary":"looks fine"}`,
		},
	}
	v := orchestrator.NewVerifier(llm)

	result, err := v.Evaluate(
		context.Background(),
		domain.GoalIntake{Purpose: "ship the feature", Goal: "close out the task"},
		"please verify",
		map[string]string{"t1": "done"},
		"test-model",
		domain.LLMProviderOpenAI,
	)
	require.NoError(t, err)
	assert.True(t, result.Passed)
	assert.Equal(t, "looks fine", result.Summary)

	require.Len(t, llm.requests, 2, "must retry once after the unparseable first response")

	firstMessages := llm.requests[0].Messages
	secondMessages := llm.requests[1].Messages
	require.Greater(t, len(secondMessages), len(firstMessages),
		"the retry must carry the correction on top of the original messages, not resend them unchanged")

	last := secondMessages[len(secondMessages)-1]
	assert.Equal(t, domain.RoleUser, last.Role)
	assert.Contains(t, last.Content, "Your response could not be parsed",
		"the retry must tell the model its previous output was invalid, the same as intake/planner/replanner")

	// The bad response itself must be echoed back so the retry has it in context.
	foundEcho := false
	for _, m := range secondMessages {
		if m.Role == domain.RoleAssistant && m.Content == "not json at all" {
			foundEcho = true
		}
	}
	assert.True(t, foundEcho, "the unparseable response must be echoed back as an assistant message")
}
