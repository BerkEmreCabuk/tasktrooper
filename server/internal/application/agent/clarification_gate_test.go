package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func explorationTools() []domain.ToolDefinition {
	return []domain.ToolDefinition{
		{Type: "function", Function: domain.FunctionDefinition{Name: "ask_user"}},
		{Type: "function", Function: domain.FunctionDefinition{Name: "grep_code"}},
	}
}

func askUserCall(id string) domain.ToolCall {
	return domain.ToolCall{
		ID: id, Type: "function",
		Function: domain.FunctionCall{Name: "ask_user", Arguments: `{"context":"where?","questions":[]}`},
	}
}

func askUserResult(id string) domain.ToolResult {
	return domain.ToolResult{
		ToolCallID: id, Name: "ask_user", Content: "awaiting user clarification",
		Clarification: &domain.ClarificationRequest{
			Context: "I need the repository structure",
			Questions: []domain.ClarificationQuestion{{
				ID: "q1", Prompt: "Where is the homepage?",
				Options: []domain.ClarificationOption{{ID: "free_text", Label: "..."}, {ID: "skip", Label: "skip"}},
			}},
		},
	}
}

func TestClarificationBeforeReadingCodeIsRefused(t *testing.T) {
	llm := new(mocks.LLMClient)
	reg := new(mocks.ToolRegistry)
	loop := agent.NewLoop(llm, reg, 2, 2, 16000)
	policy := domain.ToolPolicy{}
	ctx, _ := registry.ContextWithToolUsage(context.Background())

	var second []domain.Message
	reg.On("DefinitionsForPolicy", policy).Return(explorationTools())
	llm.On("Chat", ctx, mock.Anything).
		Return(domain.AgentResponse{Message: domain.Message{
			Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{askUserCall("tc1")},
		}}, nil).Once()
	llm.On("Chat", ctx, mock.Anything).
		Run(func(args mock.Arguments) { second = args.Get(1).(domain.AgentRequest).Messages }).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "found it myself"}}, nil).Once()
	reg.On("ExecuteWithPolicy", ctx, askUserCall("tc1"), policy).Return(askUserResult("tc1"))

	resp, err := loop.RunTask(ctx, []domain.Message{{Role: domain.RoleUser, Content: "add a link"}}, "", "", policy)

	require.NoError(t, err)
	require.Nil(t, resp.Clarification, "the human must not be asked before the repo is read")
	require.Equal(t, "found it myself", resp.Message.Content)

	var refusal domain.Message
	for _, m := range second {
		if m.Role == domain.RoleTool && m.ToolCallID == "tc1" {
			refusal = m
		}
	}
	require.Contains(t, refusal.Content, "ask_user rejected")
	require.Contains(t, refusal.Content, "grep_code", "the refusal must name the tools that answer it")
}

func TestClarificationAfterReadingCodeReachesTheHuman(t *testing.T) {
	llm := new(mocks.LLMClient)
	reg := new(mocks.ToolRegistry)
	loop := agent.NewLoop(llm, reg, 3, 3, 16000)
	policy := domain.ToolPolicy{}
	ctx, usage := registry.ContextWithToolUsage(context.Background())
	usage.Record("grep_code")

	reg.On("DefinitionsForPolicy", policy).Return(explorationTools())
	llm.On("Chat", ctx, mock.Anything).Return(domain.AgentResponse{Message: domain.Message{
		Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{askUserCall("tc1")},
	}}, nil).Once()
	reg.On("ExecuteWithPolicy", ctx, askUserCall("tc1"), policy).Return(askUserResult("tc1"))

	resp, err := loop.RunTask(ctx, []domain.Message{{Role: domain.RoleUser, Content: "add a link"}}, "", "", policy)

	require.NoError(t, err)
	require.NotNil(t, resp.Clarification)
	require.Equal(t, "I need the repository structure", resp.Clarification.Context)
}

func TestClarificationRefusedOnlyOnce(t *testing.T) {
	llm := new(mocks.LLMClient)
	reg := new(mocks.ToolRegistry)
	loop := agent.NewLoop(llm, reg, 3, 3, 16000)
	policy := domain.ToolPolicy{}
	ctx, _ := registry.ContextWithToolUsage(context.Background())

	reg.On("DefinitionsForPolicy", policy).Return(explorationTools())
	llm.On("Chat", ctx, mock.Anything).Return(domain.AgentResponse{Message: domain.Message{
		Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{askUserCall("tc1")},
	}}, nil).Once()
	llm.On("Chat", ctx, mock.Anything).Return(domain.AgentResponse{Message: domain.Message{
		Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{askUserCall("tc2")},
	}}, nil).Once()
	reg.On("ExecuteWithPolicy", ctx, askUserCall("tc1"), policy).Return(askUserResult("tc1"))
	reg.On("ExecuteWithPolicy", ctx, askUserCall("tc2"), policy).Return(askUserResult("tc2"))

	resp, err := loop.RunTask(ctx, []domain.Message{{Role: domain.RoleUser, Content: "add a link"}}, "", "", policy)

	require.NoError(t, err)
	require.NotNil(t, resp.Clarification, "the second question must reach the human")
}

func TestClarificationNotRefusedWithoutTrackerOrReadTools(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tracked bool
		tools   []domain.ToolDefinition
	}{
		{"chat run, no tracker", false, explorationTools()},
		{"no read tools in policy", true, []domain.ToolDefinition{
			{Type: "function", Function: domain.FunctionDefinition{Name: "ask_user"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			llm := new(mocks.LLMClient)
			reg := new(mocks.ToolRegistry)
			loop := agent.NewLoop(llm, reg, 2, 2, 16000)
			policy := domain.ToolPolicy{}
			ctx := context.Background()
			if tc.tracked {
				ctx, _ = registry.ContextWithToolUsage(ctx)
			}

			reg.On("DefinitionsForPolicy", policy).Return(tc.tools)
			llm.On("Chat", ctx, mock.Anything).Return(domain.AgentResponse{Message: domain.Message{
				Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{askUserCall("tc1")},
			}}, nil).Once()
			reg.On("ExecuteWithPolicy", ctx, askUserCall("tc1"), policy).Return(askUserResult("tc1"))

			resp, err := loop.RunTask(ctx, []domain.Message{{Role: domain.RoleUser, Content: "add a link"}}, "", "", policy)

			require.NoError(t, err)
			require.NotNil(t, resp.Clarification, strings.ToLower(tc.name)+" must not be blocked")
		})
	}
}

func TestClarificationNotRefusedAfterTerminalUse(t *testing.T) {
	llm := new(mocks.LLMClient)
	reg := new(mocks.ToolRegistry)
	loop := agent.NewLoop(llm, reg, 2, 2, 16000)
	policy := domain.ToolPolicy{}
	ctx, usage := registry.ContextWithToolUsage(context.Background())
	usage.Record("run_terminal")

	reg.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{
		{Type: "function", Function: domain.FunctionDefinition{Name: "ask_user"}},
		{Type: "function", Function: domain.FunctionDefinition{Name: "run_terminal"}},
	})
	llm.On("Chat", ctx, mock.Anything).Return(domain.AgentResponse{Message: domain.Message{
		Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{askUserCall("tc1")},
	}}, nil).Once()
	reg.On("ExecuteWithPolicy", ctx, askUserCall("tc1"), policy).Return(askUserResult("tc1"))

	resp, err := loop.RunTask(ctx, []domain.Message{{Role: domain.RoleUser, Content: "add a link"}}, "", "", policy)

	require.NoError(t, err)
	require.NotNil(t, resp.Clarification)
}
