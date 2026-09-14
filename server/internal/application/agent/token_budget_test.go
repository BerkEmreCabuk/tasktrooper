package agent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type TokenBudgetSuite struct {
	suite.Suite
}

func TestTokenBudgetSuite(t *testing.T) {
	suite.Run(t, new(TokenBudgetSuite))
}

type usageLLM struct {
	usage    domain.Usage
	turn     int
	requests []domain.AgentRequest
}

func (f *usageLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	stored := req
	stored.Messages = append([]domain.Message(nil), req.Messages...)
	f.requests = append(f.requests, stored)
	f.turn++
	if len(req.Tools) == 0 {
		return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "wrapped up"}}, nil
	}
	id := fmt.Sprintf("tc%d", f.turn)
	return domain.AgentResponse{
		Message: domain.Message{
			Role: domain.RoleAssistant,
			ToolCalls: []domain.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: domain.FunctionCall{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.go"}`, f.turn)},
			}},
		},
		Usage: f.usage,
	}, nil
}

func (f *usageLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return f.Chat(ctx, req)
}

func (f *usageLLM) Models(context.Context) ([]string, error)                 { return nil, nil }
func (f *usageLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

func requestWithTokenWarning(requests []domain.AgentRequest) bool {
	for _, req := range requests {
		if len(req.Tools) == 0 {
			continue
		}
		for _, m := range req.Messages {
			if m.Role == domain.RoleSystem && strings.Contains(m.Content, "[token budget]") {
				return true
			}
		}
	}
	return false
}

func (s *TokenBudgetSuite) TestRunTaskStopsAtTokenCapWithPriorWarning() {
	llm := &usageLLM{usage: domain.Usage{PromptTokens: 200, CompletionTokens: 100}}
	reg := &echoRegistry{payload: "x"}

	loop := agent.NewLoop(llm, reg, 30, 30, 16000)
	loop.SetRunTokenCap(1000)

	messages := []domain.Message{{Role: domain.RoleUser, Content: "do work"}}
	_, err := loop.RunTask(context.Background(), messages, "test-model", domain.LLMProviderAnthropic, domain.ToolPolicy{})
	s.Require().Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().ErrorAs(err, &budgetErr)
	s.True(budgetErr.TokenExhausted, "expected the token cap, not iterations/stuck/dead-end, to end the run")
	s.False(budgetErr.Stuck)
	s.False(budgetErr.DeadEnd)
	s.Equal(1200, budgetErr.TokensUsed, "300 tokens/turn over 4 iterations before the 1000 cap is crossed")
	s.Contains(err.Error(), "run token budget exhausted after 1200 tokens", "the cause must be visible on the error itself")

	s.Equal("wrapped up", budgetErr.Partial)

	s.True(requestWithTokenWarning(llm.requests),
		"expected a '[token budget]' system nudge in some iteration request before the cut")
}

func (s *TokenBudgetSuite) TestRunStreamStopsAtTokenCapWithPriorWarning() {
	llm := &usageLLM{usage: domain.Usage{PromptTokens: 200, CompletionTokens: 100}}
	reg := &echoRegistry{payload: "x"}

	loop := agent.NewLoop(llm, reg, 30, 30, 16000)
	loop.SetRunTokenCap(1000)

	messages := []domain.Message{{Role: domain.RoleUser, Content: "do work"}}
	_, err := loop.RunStream(context.Background(), messages, "test-model", domain.LLMProviderAnthropic, domain.ToolPolicy{}, func(string) {})
	s.Require().Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().ErrorAs(err, &budgetErr)
	s.True(budgetErr.TokenExhausted)
	s.Equal(1200, budgetErr.TokensUsed)
	s.Contains(err.Error(), "run token budget exhausted after 1200 tokens")

	s.True(requestWithTokenWarning(llm.requests),
		"expected a '[token budget]' system nudge in some iteration request before the cut")
}

func (s *TokenBudgetSuite) TestZeroCapIsUnlimited() {
	llm := &usageLLM{usage: domain.Usage{PromptTokens: 10_000, CompletionTokens: 10_000}}
	reg := &echoRegistry{payload: "x"}

	loop := agent.NewLoop(llm, reg, 3, 3, 16000)

	messages := []domain.Message{{Role: domain.RoleUser, Content: "do work"}}
	_, err := loop.RunTask(context.Background(), messages, "test-model", domain.LLMProviderAnthropic, domain.ToolPolicy{})
	s.Require().Error(err)

	var budgetErr *agent.BudgetExhaustedError
	s.Require().ErrorAs(err, &budgetErr)
	s.False(budgetErr.TokenExhausted, "a disabled cap (0) must never be reported as the cause")
	s.Equal(0, budgetErr.TokensUsed)
	s.Contains(err.Error(), "maximum iterations")

	s.False(requestWithTokenWarning(llm.requests), "a disabled cap must never warn either")
}
