package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func TestEmptyTurnIsRetriedOnce(t *testing.T) {
	llm := new(mocks.LLMClient)
	registry := new(mocks.ToolRegistry)
	loop := agent.NewLoop(llm, registry, 5, 5, 16000)
	policy := domain.ToolPolicy{}

	registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	empty := domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "  "}}
	answered := domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "DE-1 deleted."}}
	llm.On("Chat", mock.Anything, mock.Anything).Return(empty, nil).Once()
	llm.On("Chat", mock.Anything, mock.Anything).Return(answered, nil).Once()

	resp, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "delete DE-1"}}, "", "", policy)

	require.NoError(t, err)
	require.Equal(t, "DE-1 deleted.", resp.Message.Content)
	llm.AssertExpectations(t)
}

func TestEmptyTurnTwiceFallsBackToAnExplanation(t *testing.T) {
	llm := new(mocks.LLMClient)
	registry := new(mocks.ToolRegistry)
	loop := agent.NewLoop(llm, registry, 5, 5, 16000)
	policy := domain.ToolPolicy{}

	registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	empty := domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant}}
	llm.On("Chat", mock.Anything, mock.Anything).Return(empty, nil).Twice()

	resp, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "hi"}}, "", "", policy)

	require.NoError(t, err)
	require.NotEmpty(t, resp.Message.Content)
	require.Contains(t, resp.Message.Content, "empty answer")
	llm.AssertNumberOfCalls(t, "Chat", 2)
}

func TestEmptyTurnPromptAsksForTheAnswer(t *testing.T) {
	msg := agent.EmptyTurnPromptForTest()

	require.Contains(t, msg, "empty")
	require.Contains(t, msg, "Reply now")
}
