package agent_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type LoopGuardSuite struct {
	suite.Suite
	llm      *mocks.LLMClient
	registry *mocks.ToolRegistry
}

func (s *LoopGuardSuite) SetupTest() {
	s.llm = new(mocks.LLMClient)
	s.registry = new(mocks.ToolRegistry)
}

func (s *LoopGuardSuite) alwaysCalls(call domain.ToolCall, seen *[]domain.Message) {
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			if seen != nil {
				*seen = args.Get(1).(domain.AgentRequest).Messages
			}
		}).
		Return(domain.AgentResponse{Message: domain.Message{
			Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call},
		}}, nil)
}

func (s *LoopGuardSuite) respondWith(calls []domain.ToolCall, seen *[]domain.Message) {
	capture := func(args mock.Arguments) {
		if seen != nil {
			*seen = args.Get(1).(domain.AgentRequest).Messages
		}
	}
	for _, call := range calls {
		s.llm.On("Chat", mock.Anything, mock.Anything).
			Run(capture).
			Return(domain.AgentResponse{Message: domain.Message{
				Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call},
			}}, nil).Once()
	}
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}}, nil)
}

func failingCalls(tool string, n int) []domain.ToolCall {
	calls := make([]domain.ToolCall, 0, n)
	for i := range n {
		calls = append(calls, domain.ToolCall{
			ID:       "tc",
			Function: domain.FunctionCall{Name: tool, Arguments: `{"path":"f` + string(rune('a'+i)) + `.txt"}`},
		})
	}
	return calls
}

func (s *LoopGuardSuite) TestConsecutiveFailuresProduceAPenalty() {
	loop := agent.NewLoop(s.llm, s.registry, 6, 6, 4000)
	policy := domain.ToolPolicy{}
	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})

	var lastSeen []domain.Message
	s.respondWith(failingCalls("run_terminal", 6), &lastSeen)
	s.registry.On("ExecuteWithPolicy", mock.Anything, mock.Anything, policy).
		Return(domain.ToolResult{ToolCallID: "tc", Name: "run_terminal", Content: "sed: no such file", IsError: true})

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "fix"}}, "", "", policy)
	s.Error(err)

	var penalised bool
	for _, m := range lastSeen {
		if m.Role == domain.RoleTool && strings.Contains(m.Content, "tool calls in a row all failed") {
			penalised = true
		}
	}
	s.True(penalised, "expected a failure-streak penalty in the tool results the model reads")

	var keptTheError bool
	for _, m := range lastSeen {
		if m.Role == domain.RoleTool && strings.Contains(m.Content, "sed: no such file") {
			keptTheError = true
		}
	}
	s.True(keptTheError, "the penalty must frame the tool's own output, not replace it")
}

func (s *LoopGuardSuite) TestRunStopsWhenEveryCallFails() {
	loop := agent.NewLoop(s.llm, s.registry, 40, 40, 4000)
	policy := domain.ToolPolicy{}
	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})

	s.respondWith(failingCalls("edit_file", 20), nil)
	s.registry.On("ExecuteWithPolicy", mock.Anything, mock.Anything, policy).
		Return(domain.ToolResult{ToolCallID: "tc", Name: "edit_file", Content: "permission denied", IsError: true})

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "edit"}}, "", "", policy)
	s.Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().True(errors.As(err, &budgetErr))
	s.True(budgetErr.DeadEnd)
	s.Less(budgetErr.Stats.Iterations, 40, "should stop well before the budget is spent")
	s.Contains(err.Error(), "in a row failed")
	s.Contains(err.Error(), "edit_file failed")
}

func (s *LoopGuardSuite) TestProvenRepeatIsNotExecutedAgain() {
	loop := agent.NewLoop(s.llm, s.registry, 12, 12, 4000)
	policy := domain.ToolPolicy{}
	call := domain.ToolCall{ID: "tc", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a.txt"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.alwaysCalls(call, nil)
	s.registry.On("ExecuteWithPolicy", mock.Anything, call, policy).
		Return(domain.ToolResult{ToolCallID: "tc", Name: "read_file", Content: "same bytes"}).Twice()

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "spin"}}, "", "", policy)
	s.Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().True(errors.As(err, &budgetErr))
	s.True(budgetErr.Stuck)
	s.Positive(budgetErr.Stats.SkippedRepeats, "repeats past the second must not reach the registry")
	s.registry.AssertNumberOfCalls(s.T(), "ExecuteWithPolicy", 2)
}

func (s *LoopGuardSuite) TestRepeatWithSomethingInBetweenStillRuns() {
	loop := agent.NewLoop(s.llm, s.registry, 3, 3, 4000)
	policy := domain.ToolPolicy{}
	read := domain.ToolCall{ID: "tc1", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a.txt"}`}}
	write := domain.ToolCall{ID: "tc2", Function: domain.FunctionCall{Name: "write_file", Arguments: `{"path":"a.txt"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.respondWith([]domain.ToolCall{read, write, read}, nil)

	s.registry.On("ExecuteWithPolicy", mock.Anything, read, policy).
		Return(domain.ToolResult{ToolCallID: "tc1", Name: "read_file", Content: "contents"}).Twice()
	s.registry.On("ExecuteWithPolicy", mock.Anything, write, policy).
		Return(domain.ToolResult{ToolCallID: "tc2", Name: "write_file", Content: ""}).Once()

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "edit"}}, "", "", policy)
	s.Error(err)
	s.registry.AssertNumberOfCalls(s.T(), "ExecuteWithPolicy", 3)
}

func (s *LoopGuardSuite) TestHistoryIsTrimmedWhileTheLoopRuns() {
	loop := agent.NewLoop(s.llm, s.registry, 6, 6, 100_000)
	loop.SetHistoryBudget(appcontext.Budget{MaxTokens: 1200, ReserveOutput: 100, KeepRecentMessages: 4})
	policy := domain.ToolPolicy{}

	call := domain.ToolCall{ID: "tc", Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"build"}`}}
	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})

	var sizes []int
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			sizes = append(sizes, appcontext.CountTokens(args.Get(1).(domain.AgentRequest).Messages))
		}).
		Return(domain.AgentResponse{Message: domain.Message{
			Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call},
		}}, nil)

	for i := range 6 {
		s.registry.On("ExecuteWithPolicy", mock.Anything, call, policy).
			Return(domain.ToolResult{
				ToolCallID: "tc", Name: "run_terminal",
				Content: strings.Repeat("output ", 150) + string(rune('a'+i)),
			}).Once()
	}

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "build"}}, "", "", policy)
	s.Error(err)

	s.Require().NotEmpty(sizes)

	for turn, size := range sizes {
		s.LessOrEqual(size, 1200, "turn %d sent a request past the budget", turn+1)
	}
}

func (s *LoopGuardSuite) TestProviderFailureCarriesTheRunStats() {
	loop := agent.NewLoop(s.llm, s.registry, 5, 5, 4000)
	policy := domain.ToolPolicy{}
	call := domain.ToolCall{ID: "tc", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(domain.AgentResponse{Message: domain.Message{
			Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call},
		}}, nil).Once()
	s.registry.On("ExecuteWithPolicy", mock.Anything, call, policy).
		Return(domain.ToolResult{ToolCallID: "tc", Name: "read_file", Content: "x"}).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(domain.AgentResponse{}, domain.NewLLMHTTPError(401, []byte("invalid api key"))).Once()

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "go"}}, "", "", policy)
	s.Error(err)

	stats, ok := agent.StatsFromError(err)
	s.Require().True(ok, "a provider failure must carry the run's stats")
	s.Equal(1, stats.ToolCalls)
	s.Equal(1, stats.ByTool["read_file"])
}

func (s *LoopGuardSuite) TestFatalProviderErrorIsNotRetried() {
	loop := agent.NewLoop(s.llm, s.registry, 3, 3, 4000)
	policy := domain.ToolPolicy{}
	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(domain.AgentResponse{}, domain.NewLLMHTTPError(400, []byte("Unexpected tool call id None"))).Once()

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "go"}}, "", "", policy)
	s.Error(err)
	s.Contains(err.Error(), "llm failed after 1 attempt")
	s.llm.AssertNumberOfCalls(s.T(), "Chat", 1)
}

func (s *LoopGuardSuite) TestOverlongRequestIsRetriedSmaller() {
	loop := agent.NewLoop(s.llm, s.registry, 3, 3, 4000)
	loop.SetHistoryBudget(appcontext.Budget{MaxTokens: 100_000, KeepRecentMessages: 2})
	policy := domain.ToolPolicy{}
	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})

	history := []domain.Message{{Role: domain.RoleSystem, Content: "be helpful"}}
	for i := range 30 {
		history = append(history,
			domain.Message{Role: domain.RoleUser, Content: strings.Repeat("question ", 100) + string(rune('a'+i))},
			domain.Message{Role: domain.RoleAssistant, Content: strings.Repeat("answer ", 100)},
		)
	}

	var sizes []int
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			sizes = append(sizes, len(args.Get(1).(domain.AgentRequest).Messages))
		}).
		Return(domain.AgentResponse{}, domain.NewLLMHTTPError(400,
			[]byte("This model's maximum context length is 8192 tokens"))).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			sizes = append(sizes, len(args.Get(1).(domain.AgentRequest).Messages))
		}).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "ok"}}, nil).Once()

	resp, err := loop.Run(context.Background(), history, "", "", policy)
	s.NoError(err)
	s.Equal("ok", resp.Message.Content)
	s.Require().Len(sizes, 2)
	s.Less(sizes[1], sizes[0], "the retry must send fewer messages than the request that was refused")
}

func (s *LoopGuardSuite) TestBuildTimingJitterStillCountsAsARepeat() {
	loop := agent.NewLoop(s.llm, s.registry, 12, 12, 4000)
	policy := domain.ToolPolicy{}
	build := domain.ToolCall{ID: "tc", Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"npm run build"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.alwaysCalls(build, nil)
	for i := range 10 {
		s.registry.On("ExecuteWithPolicy", mock.Anything, build, policy).
			Return(domain.ToolResult{
				ToolCallID: "tc", Name: "run_terminal",
				Content: fmt.Sprintf("▲ Next.js 15.5.20\n✓ Compiled successfully in 11.%ds\nGenerating static pages (9/9)", i),
			}).Once()
	}

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "ship it"}}, "", "", policy)
	s.Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().True(errors.As(err, &budgetErr))
	s.True(budgetErr.Stuck, "a build repeated with only its duration changing is not progress")
	s.LessOrEqual(len(s.registry.Calls), 4, "the guard must stop paying for the rebuild, not just note it")
}

func (s *LoopGuardSuite) TestSameCallWithAChangingResultIsWarnedNotStopped() {
	loop := agent.NewLoop(s.llm, s.registry, 4, 4, 4000)
	policy := domain.ToolPolicy{}
	build := domain.ToolCall{ID: "tc", Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{"command":"npm run build"}`}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	var lastSeen []domain.Message
	capture := func(args mock.Arguments) { lastSeen = args.Get(1).(domain.AgentRequest).Messages }
	for range 3 {
		s.llm.On("Chat", mock.Anything, mock.Anything).Run(capture).
			Return(domain.AgentResponse{Message: domain.Message{
				Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{build},
			}}, nil).Once()
	}
	s.llm.On("Chat", mock.Anything, mock.Anything).Run(capture).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}}, nil)
	for i := range 3 {
		s.registry.On("ExecuteWithPolicy", mock.Anything, build, policy).
			Return(domain.ToolResult{
				ToolCallID: "tc", Name: "run_terminal",
				Content: fmt.Sprintf("Type error in src/page-%d.tsx", i),
			}).Once()
	}

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "fix the types"}}, "", "", policy)
	s.NoError(err, "a changing result is progress; the run must be allowed to continue")

	var warned, keptTheOutput bool
	for _, m := range lastSeen {
		if m.Role != domain.RoleTool {
			continue
		}
		if strings.Contains(m.Content, "run this exact run_terminal call 3 times") {
			warned = true
		}
		if strings.Contains(m.Content, "Type error in src/page-2.tsx") {
			keptTheOutput = true
		}
	}
	s.True(warned, "the third execution of one command must be named")
	s.True(keptTheOutput, "the warning is appended to the output, never in place of it")
	s.registry.AssertNumberOfCalls(s.T(), "ExecuteWithPolicy", 3)
}

func (s *LoopGuardSuite) TestSameCallWithAChangingResultIsEventuallyStopped() {
	loop := agent.NewLoop(s.llm, s.registry, 40, 40, 4000)
	policy := domain.ToolPolicy{}
	read := domain.ToolCall{ID: "tc", Function: domain.FunctionCall{
		Name: "browser_read_dom", Arguments: `{"selector":"body"}`,
	}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})
	s.alwaysCalls(read, nil)
	for i := range 40 {
		s.registry.On("ExecuteWithPolicy", mock.Anything, read, policy).
			Return(domain.ToolResult{
				ToolCallID: "tc", Name: "browser_read_dom",
				Content: fmt.Sprintf("url: http://localhost:3000/\nno Android button\nclock: %d", i),
			}).Once()
	}

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "check the page"}}, "", "", policy)
	s.Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().True(errors.As(err, &budgetErr))
	s.True(budgetErr.Stuck)
	s.Less(budgetErr.Stats.Iterations, 40, "the run must stop long before its budget is spent")
	s.LessOrEqual(budgetErr.Stats.ByTool["browser_read_dom"], 8)
}

func (s *LoopGuardSuite) TestEmptyToolResultIsSaidOutLoud() {
	loop := agent.NewLoop(s.llm, s.registry, 3, 3, 4000)
	policy := domain.ToolPolicy{}
	read := domain.ToolCall{ID: "tc", Function: domain.FunctionCall{
		Name: "browser_read_dom", Arguments: `{"selector":"#root"}`,
	}}

	s.registry.On("DefinitionsForPolicy", policy).Return([]domain.ToolDefinition{})

	var lastSeen []domain.Message
	capture := func(args mock.Arguments) { lastSeen = args.Get(1).(domain.AgentRequest).Messages }
	s.llm.On("Chat", mock.Anything, mock.Anything).Run(capture).
		Return(domain.AgentResponse{Message: domain.Message{
			Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{read},
		}}, nil).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).Run(capture).
		Return(domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}}, nil)
	s.registry.On("ExecuteWithPolicy", mock.Anything, read, policy).
		Return(domain.ToolResult{ToolCallID: "tc", Name: "browser_read_dom", Content: ""})

	_, err := loop.Run(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "read it"}}, "", "", policy)
	s.NoError(err)

	var explained bool
	for _, m := range lastSeen {
		if m.Role == domain.RoleTool {
			s.NotEmpty(strings.TrimSpace(m.Content), "a tool message must never be blank")
			if strings.Contains(m.Content, "[no output]") {
				explained = true
			}
		}
	}
	s.True(explained, "an empty result must be named as an answer, not left blank")
}

func TestLoopGuardSuite(t *testing.T) {
	suite.Run(t, new(LoopGuardSuite))
}
