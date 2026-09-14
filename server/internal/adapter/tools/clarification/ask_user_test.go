package clarification_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/clarification"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAskUserTool_DefinitionIncludesContract(t *testing.T) {
	tool := clarification.NewAskUserTool()
	def := tool.Definition()
	assert.Equal(t, prompt.AskUserToolDescription, def.Function.Description)
	assert.Contains(t, def.Function.Description, "free_text")
	assert.Contains(t, def.Function.Description, "other")
}

func TestAskUserTool_ValidChoiceMode(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"Need scope","questions":[{"id":"q1","prompt":"Which area?","options":[{"id":"auth","label":"Auth"},{"id":"other","label":"Diğer"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
	require.NotNil(t, result.Clarification)
	assert.Len(t, result.Clarification.Questions, 1)
}

func TestAskUserTool_ValidTextMode(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"Need URL","questions":[{"id":"url","prompt":"Profile URL?","options":[{"id":"free_text","label":"Yanıtımı yazacağım"},{"id":"skip","label":"Sonra"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
}

func TestAskUserTool_RejectsMixedMode(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"?","options":[{"id":"free_text","label":"Text"},{"id":"a","label":"A"}]}]}`
	result := tool.Execute(t.Context(), raw)
	assert.True(t, result.IsError)
}

func TestAskUserTool_RejectsChoiceWithoutOtherLast(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"?","options":[{"id":"a","label":"Auth"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
	require.NotNil(t, result.Clarification)
	assert.Equal(t, "other", result.Clarification.Questions[0].Options[len(result.Clarification.Questions[0].Options)-1].ID)
}

func TestAskUserTool_AcceptsAllowMultiple(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"Sections?","allow_multiple":true,"options":[{"id":"home","label":"Home"},{"id":"other","label":"Other"}]}]}`
	result := tool.Execute(t.Context(), raw)
	require.False(t, result.IsError)
	assert.True(t, result.Clarification.Questions[0].AllowMultiple)
}

func TestAskUserTool_InvalidQuestions(t *testing.T) {
	tool := clarification.NewAskUserTool()
	raw := `{"context":"x","questions":[{"id":"q1","prompt":"?","options":[]}]}`
	result := tool.Execute(t.Context(), raw)
	assert.True(t, result.IsError)
}
