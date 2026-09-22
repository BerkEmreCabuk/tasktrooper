package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type MaxTokensSuite struct {
	suite.Suite
	llm      *mocks.LLMClient
	registry *mocks.ToolRegistry
}

func TestMaxTokensSuite(t *testing.T) {
	suite.Run(t, new(MaxTokensSuite))
}

func (s *MaxTokensSuite) SetupTest() {
	s.llm = new(mocks.LLMClient)
	s.registry = new(mocks.ToolRegistry)
}

func (s *MaxTokensSuite) TearDownTest() {
	s.llm.AssertExpectations(s.T())
	s.registry.AssertExpectations(s.T())
}

func hasMaxTokens(want int) func(domain.AgentRequest) bool {
	return func(req domain.AgentRequest) bool { return req.MaxTokens == want }
}

func (s *MaxTokensSuite) TestRunWithNoHistoryBudgetLeavesMaxTokensZero() {
	loop := agent.NewLoop(s.llm, s.registry, 5, 5, 16000)
	messages := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}
	policy := domain.ToolPolicy{}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("Chat", context.Background(), mock.MatchedBy(hasMaxTokens(0))).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "hi"}}, nil)

	_, err := loop.Run(context.Background(), messages, "", "", policy)
	s.NoError(err)
}

func (s *MaxTokensSuite) TestRunCarriesReserveOutputAsMaxTokens() {
	loop := agent.NewLoop(s.llm, s.registry, 5, 5, 16000)
	loop.SetHistoryBudget(appcontext.Budget{MaxTokens: 100_000, ReserveOutput: 3000, KeepRecentMessages: 4})
	messages := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}
	policy := domain.ToolPolicy{}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("Chat", context.Background(), mock.MatchedBy(hasMaxTokens(3000))).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "hi"}}, nil)

	_, err := loop.Run(context.Background(), messages, "", "", policy)
	s.NoError(err)
}

func (s *MaxTokensSuite) TestRunStreamCarriesReserveOutputAsMaxTokens() {
	loop := agent.NewLoop(s.llm, s.registry, 5, 5, 16000)
	loop.SetHistoryBudget(appcontext.Budget{MaxTokens: 100_000, ReserveOutput: 1500, KeepRecentMessages: 4})
	messages := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}
	policy := domain.ToolPolicy{}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("ChatStream", context.Background(), mock.MatchedBy(hasMaxTokens(1500)), mock.Anything).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "hi"}}, nil)

	_, err := loop.RunStream(context.Background(), messages, "", "", policy, func(string) {})
	s.NoError(err)
}

func (s *MaxTokensSuite) TestWrapUpRequestCarriesReserveOutputAsMaxTokens() {
	loop := agent.NewLoop(s.llm, s.registry, 2, 2, 100)
	loop.SetHistoryBudget(appcontext.Budget{MaxTokens: 100_000, ReserveOutput: 777, KeepRecentMessages: 4})
	messages := []domain.Message{{Role: domain.RoleUser, Content: "loop forever"}}
	policy := domain.ToolPolicy{}
	toolDefs := []domain.ToolDefinition{{Type: "function", Function: domain.FunctionDefinition{Name: "run_terminal"}}}

	toolCallMsg := domain.Message{
		Role: domain.RoleAssistant,
		ToolCalls: []domain.ToolCall{{
			ID: "tc1", Type: "function",
			Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"true"}`},
		}},
	}
	toolResult := domain.ToolResult{ToolCallID: "tc1", Name: "run_terminal", Content: "ok"}
	tc := domain.ToolCall{ID: "tc1", Type: "function", Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"true"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return(toolDefs)
	s.llm.On("Chat", context.Background(), mock.MatchedBy(func(req domain.AgentRequest) bool {
		return len(req.Tools) > 0 && req.MaxTokens == 777
	})).Return(domain.AgentResponse{Message: toolCallMsg}, nil)
	s.registry.On("ExecuteWithPolicy", context.Background(), tc, policy).Return(toolResult)
	s.llm.On("Chat", context.Background(), mock.MatchedBy(func(req domain.AgentRequest) bool {
		return len(req.Tools) == 0 && req.MaxTokens == 777
	})).Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "wrapped up"}}, nil)

	_, err := loop.Run(context.Background(), messages, "", "", policy)
	s.Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().ErrorAs(err, &budgetErr)
	s.Equal("wrapped up", budgetErr.Partial)
}
