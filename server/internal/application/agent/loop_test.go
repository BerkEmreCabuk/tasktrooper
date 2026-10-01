package agent_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type AgentLoopSuite struct {
	suite.Suite
	llm      *mocks.LLMClient
	registry *mocks.ToolRegistry
	loop     *agent.Loop
}

func (s *AgentLoopSuite) SetupTest() {
	s.llm = new(mocks.LLMClient)
	s.registry = new(mocks.ToolRegistry)
	s.loop = agent.NewLoop(s.llm, s.registry, 5, 5, 16000)
}

func (s *AgentLoopSuite) TearDownTest() {
	s.llm.AssertExpectations(s.T())
	s.registry.AssertExpectations(s.T())
}

func (s *AgentLoopSuite) TestDirectResponse() {
	messages := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}
	expected := domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: "hi there"},
	}
	policy := domain.ToolPolicy{}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})

	s.llm.On("Chat", context.Background(), domain.AgentRequest{
		Messages: messages, Tools: []domain.ToolDefinition{}, Model: "", ToolPolicy: policy,
		CacheAnchorIndex: len(messages),
	}).Return(expected, nil)

	resp, err := s.loop.Run(context.Background(), messages, "", "", policy)
	s.NoError(err)
	s.Equal("hi there", resp.Message.Content)
}

func (s *AgentLoopSuite) TestToolCallThenResponse() {
	messages := []domain.Message{{Role: domain.RoleUser, Content: "list files"}}
	policy := domain.ToolPolicy{}
	toolDefs := []domain.ToolDefinition{{Type: "function", Function: domain.FunctionDefinition{Name: "run_terminal"}}}

	toolCallResp := domain.AgentResponse{
		Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{{
				ID: "tc1", Type: "function",
				Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"ls"}`},
			}},
		},
	}
	finalResp := domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: "files: a.txt b.txt"},
	}
	toolResult := domain.ToolResult{ToolCallID: "tc1", Name: "run_terminal", Content: "a.txt\nb.txt"}

	s.registry.On("DefinitionsForPolicy", policy).Return(toolDefs)
	s.llm.On("Chat", context.Background(), domain.AgentRequest{
		Messages: messages, Tools: toolDefs, Model: "", ToolPolicy: policy,
		CacheAnchorIndex: len(messages),
	}).Return(toolCallResp, nil).Once()

	expectedTC := domain.ToolCall{ID: "tc1", Type: "function", Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"ls"}`}}
	s.registry.On("ExecuteWithPolicy", context.Background(), expectedTC, policy).Return(toolResult)

	messagesAfterToolCall := append(messages,
		toolCallResp.Message,
		domain.Message{Role: domain.RoleTool, Content: "a.txt\nb.txt", ToolCallID: "tc1", Name: "run_terminal"},
	)

	s.llm.On("Chat", context.Background(), domain.AgentRequest{
		Messages: messagesAfterToolCall, Tools: toolDefs, Model: "", ToolPolicy: policy,
		CacheAnchorIndex: len(messages),
	}).Return(finalResp, nil).Once()

	resp, err := s.loop.Run(context.Background(), messages, "", "", policy)
	s.NoError(err)
	s.Equal("files: a.txt b.txt", resp.Message.Content)
}

func (s *AgentLoopSuite) TestLLMErrorRetry() {
	messages := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}
	policy := domain.ToolPolicy{}
	llmErr := errors.New("connection refused")

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("Chat", context.Background(), domain.AgentRequest{
		Messages: messages, Tools: []domain.ToolDefinition{}, Model: "", ToolPolicy: policy,
		CacheAnchorIndex: len(messages),
	}).Return(domain.AgentResponse{}, llmErr).Times(3)

	_, err := s.loop.Run(context.Background(), messages, "", "", policy)
	s.Error(err)
	s.Contains(err.Error(), "llm failed after")
}

func (s *AgentLoopSuite) TestMaxIterationsExceeded() {
	loop := agent.NewLoop(s.llm, s.registry, 2, 2, 100)
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
	toolResp := domain.AgentResponse{Message: toolCallMsg}
	toolResult := domain.ToolResult{ToolCallID: "tc1", Name: "run_terminal", Content: "ok"}
	tc := domain.ToolCall{ID: "tc1", Type: "function", Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"true"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return(toolDefs)
	s.llm.On("Chat", context.Background(), mock.Anything).Return(toolResp, nil)
	s.registry.On("ExecuteWithPolicy", context.Background(), tc, policy).Return(toolResult)

	_, err := loop.Run(context.Background(), messages, "", "", policy)
	s.Error(err)
	s.Contains(err.Error(), "maximum iterations")

	var budgetErr *agent.BudgetExhaustedError
	s.Require().True(errors.As(err, &budgetErr))
	s.Equal(2, budgetErr.Budget)
	s.Equal(2, budgetErr.Stats.Iterations)
	s.Equal(2, budgetErr.Stats.ToolCalls)
	s.False(budgetErr.Stuck)
}

func (s *AgentLoopSuite) TestBudgetWarningReachesTheModel() {
	loop := agent.NewLoop(s.llm, s.registry, 1, 1, 100)
	messages := []domain.Message{{Role: domain.RoleUser, Content: "do work"}}
	policy := domain.ToolPolicy{}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})

	var seen []domain.Message
	s.llm.On("Chat", context.Background(), mock.Anything).
		Run(func(args mock.Arguments) {
			req := args.Get(1).(domain.AgentRequest)
			seen = req.Messages
		}).
		Return(domain.AgentResponse{Message: domain.Message{
			Role:      domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{{ID: "tc1", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a"}`}}},
		}}, nil)
	s.registry.On("ExecuteWithPolicy", context.Background(), mock.Anything, policy).
		Return(domain.ToolResult{ToolCallID: "tc1", Name: "read_file", Content: "x"})

	_, err := loop.Run(context.Background(), messages, "", "", policy)
	s.Error(err)

	var warned bool
	for _, m := range seen {
		if m.Role == domain.RoleSystem && strings.Contains(m.Content, "[budget]") {
			warned = true
		}
	}
	s.True(warned, "expected a budget warning message in the request history")
}

func (s *AgentLoopSuite) TestStuckOnRepeatedIdenticalCalls() {
	loop := agent.NewLoop(s.llm, s.registry, 30, 30, 100)
	messages := []domain.Message{{Role: domain.RoleUser, Content: "spin"}}
	policy := domain.ToolPolicy{}

	call := domain.ToolCall{ID: "tc1", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a.txt"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("Chat", context.Background(), mock.Anything).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call}}}, nil)
	s.registry.On("ExecuteWithPolicy", context.Background(), call, policy).
		Return(domain.ToolResult{ToolCallID: "tc1", Name: "read_file", Content: "same bytes"})

	_, err := loop.Run(context.Background(), messages, "", "", policy)
	s.Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().True(errors.As(err, &budgetErr))
	s.True(budgetErr.Stuck)
	s.Less(budgetErr.Stats.Iterations, 30, "should stop well before the budget is spent")
	s.Contains(err.Error(), "repeated the same call")
}

func (s *AgentLoopSuite) TestRepeatedCallWithChangingResultIsNotStuck() {
	loop := agent.NewLoop(s.llm, s.registry, 4, 4, 100)
	messages := []domain.Message{{Role: domain.RoleUser, Content: "build until green"}}
	policy := domain.ToolPolicy{}

	call := domain.ToolCall{ID: "tc1", Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"npm run build"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("Chat", context.Background(), mock.Anything).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call}}}, nil)

	for attempt := 1; attempt <= 4; attempt++ {
		s.registry.On("ExecuteWithPolicy", context.Background(), call, policy).
			Return(domain.ToolResult{ToolCallID: "tc1", Name: "run_terminal", Content: fmt.Sprintf("errors: %d", attempt)}).Once()
	}

	_, err := loop.Run(context.Background(), messages, "", "", policy)
	s.Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().True(errors.As(err, &budgetErr))
	s.False(budgetErr.Stuck)
	s.Zero(budgetErr.Stats.RepeatedNoProgress)
}

func (s *AgentLoopSuite) TestRunTaskUsesTaskBudget() {
	loop := agent.NewLoop(s.llm, s.registry, 1, 3, 100)
	messages := []domain.Message{{Role: domain.RoleUser, Content: "implement"}}
	policy := domain.ToolPolicy{}

	call := domain.ToolCall{ID: "tc1", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("Chat", context.Background(), mock.Anything).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call}}}, nil)

	for attempt := 1; attempt <= 3; attempt++ {
		s.registry.On("ExecuteWithPolicy", context.Background(), call, policy).
			Return(domain.ToolResult{ToolCallID: "tc1", Name: "read_file", Content: fmt.Sprintf("v%d", attempt)}).Once()
	}

	_, err := loop.RunTask(context.Background(), messages, "", "", policy)
	s.Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().True(errors.As(err, &budgetErr))
	s.Equal(3, budgetErr.Budget)
}

// stubArchiver is agent.ScreenshotArchiver: it fabricates a deterministic
// attachment id per upload instead of writing anywhere real.
type stubArchiver struct{ next int }

func (a *stubArchiver) Upload(_ context.Context, _, _ string, _ []byte, _ *uuid.UUID, _, _ string) (domain.AttachmentMeta, error) {
	a.next++
	return domain.AttachmentMeta{ID: uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", a.next))}, nil
}

func (s *AgentLoopSuite) TestToolResultImageGetsAnAttachmentIDTheModelCanCite() {
	messages := []domain.Message{{Role: domain.RoleUser, Content: "look at the screen"}}
	policy := domain.ToolPolicy{}
	toolDefs := []domain.ToolDefinition{{Type: "function", Function: domain.FunctionDefinition{Name: "browser_screenshot"}}}

	s.loop.SetScreenshotArchiver(&stubArchiver{})

	toolCallResp := domain.AgentResponse{
		Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{{
				ID: "tc1", Type: "function",
				Function: domain.FunctionCall{Name: "browser_screenshot", Arguments: `{}`},
			}},
		},
	}
	finalResp := domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}}
	toolResult := domain.ToolResult{
		ToolCallID: "tc1", Name: "browser_screenshot", Content: "HTTP 200 url=http://127.0.0.1:3000",
		Images: []domain.ToolResultImage{{MediaType: "image/png", Data: "aGVsbG8="}},
	}

	s.registry.On("DefinitionsForPolicy", policy).Return(toolDefs)
	s.llm.On("Chat", context.Background(), domain.AgentRequest{
		Messages: messages, Tools: toolDefs, Model: "", ToolPolicy: policy,
		CacheAnchorIndex: len(messages),
	}).Return(toolCallResp, nil).Once()

	expectedTC := domain.ToolCall{ID: "tc1", Type: "function", Function: domain.FunctionCall{Name: "browser_screenshot", Arguments: `{}`}}
	s.registry.On("ExecuteWithPolicy", context.Background(), expectedTC, policy).Return(toolResult)

	var sentToolMessage domain.Message
	s.llm.On("Chat", context.Background(), mock.MatchedBy(func(req domain.AgentRequest) bool {
		for _, m := range req.Messages {
			if m.Role == domain.RoleTool && m.ToolCallID == "tc1" {
				sentToolMessage = m
				return true
			}
		}
		return false
	})).Return(finalResp, nil).Once()

	_, err := s.loop.Run(context.Background(), messages, "", "", policy)
	s.NoError(err)
	s.Contains(sentToolMessage.Content, "HTTP 200 url=http://127.0.0.1:3000")
	s.Contains(sentToolMessage.Content, "attachment: 00000000-0000-0000-0000-000000000001")
}

func TestAgentLoopSuite(t *testing.T) {
	suite.Run(t, new(AgentLoopSuite))
}
