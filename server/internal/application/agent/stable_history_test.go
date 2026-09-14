package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type StableHistorySuite struct {
	suite.Suite
}

func TestStableHistorySuite(t *testing.T) {
	suite.Run(t, new(StableHistorySuite))
}

type recordingLLM struct {
	requests [][]domain.Message
	models   []string
	turn     int
}

func (f *recordingLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	f.requests = append(f.requests, append([]domain.Message(nil), req.Messages...))
	f.models = append(f.models, req.Model)
	f.turn++
	if len(req.Tools) == 0 {
		return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "wrapped up"}}, nil
	}
	id := fmt.Sprintf("tc%d", f.turn)
	return domain.AgentResponse{Message: domain.Message{
		Role:    domain.RoleAssistant,
		Content: fmt.Sprintf("reading file %d", f.turn),
		ToolCalls: []domain.ToolCall{{
			ID:       id,
			Type:     "function",
			Function: domain.FunctionCall{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.go"}`, f.turn)},
		}},
	}}, nil
}

func (f *recordingLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return f.Chat(ctx, req)
}

func (f *recordingLLM) Models(context.Context) ([]string, error)                 { return nil, nil }
func (f *recordingLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

type echoRegistry struct {
	payload string
}

func (r *echoRegistry) Register(port.ToolExecutor) {}

func (r *echoRegistry) Definitions() []domain.ToolDefinition {
	return r.DefinitionsForPolicy(domain.ToolPolicy{})
}

func (r *echoRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition {
	return []domain.ToolDefinition{{Type: "function", Function: domain.FunctionDefinition{Name: "read_file"}}}
}

func (r *echoRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return r.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (r *echoRegistry) ExecuteWithPolicy(_ context.Context, call domain.ToolCall, _ domain.ToolPolicy) domain.ToolResult {
	return domain.ToolResult{
		ToolCallID: call.ID,
		Name:       call.Function.Name,
		Content:    call.Function.Arguments + " => " + r.payload,
	}
}

func (r *echoRegistry) AllToolNames() []string { return []string{"read_file"} }

type countingSummarizer struct {
	inputs [][]domain.Message
	models []string
}

func (c *countingSummarizer) Summarize(_ context.Context, messages []domain.Message, model string) (string, error) {
	c.inputs = append(c.inputs, append([]domain.Message(nil), messages...))
	c.models = append(c.models, model)
	return fmt.Sprintf("condensed span %d", len(c.inputs)), nil
}

func runHead() []domain.Message {
	return []domain.Message{
		{Role: domain.RoleSystem, Content: "PERSONA " + strings.Repeat("p", 8000)},
		{Role: domain.RoleSystem, Content: "PROJECT " + strings.Repeat("j", 8000)},
		{Role: domain.RoleUser, Content: "TRIGGER: implement the thing"},
	}
}

func mustJSON(s *StableHistorySuite, messages []domain.Message) string {
	s.T().Helper()
	raw, err := json.Marshal(messages)
	s.Require().NoError(err)
	return string(raw)
}

func (s *StableHistorySuite) TestRequestPrefixSurvivesALongRun() {
	llm := &recordingLLM{}
	reg := &echoRegistry{payload: strings.Repeat("x", 1000)}
	sum := &countingSummarizer{}

	loop := agent.NewLoop(llm, reg, 45, 45, 16000)
	loop.SetHistoryBudget(appcontext.Budget{
		MaxTokens:          12000,
		ReserveOutput:      2000,
		SummarizeThreshold: 7000,
		KeepRecentMessages: 4,
	})
	loop.SetSummarizer(sum)

	head := runHead()
	headJSON := mustJSON(s, head)

	_, err := loop.RunTask(context.Background(), head, "test-model", domain.LLMProviderAnthropic, domain.ToolPolicy{})
	s.Require().Error(err)

	requests := llm.requests
	s.Require().Greater(len(requests), 30, "the run must be long enough to trim more than once")
	s.Require().GreaterOrEqual(len(sum.inputs), 2, "the run must have trimmed at least twice")

	for i, req := range requests {
		s.Require().GreaterOrEqual(len(req), len(head), "request %d lost part of its head", i)
		s.Equal(headJSON, mustJSON(s, req[:len(head)]), "request %d rewrote the head", i)

		blocks := 0
		for _, m := range req {
			if strings.HasPrefix(m.Content, appcontext.SummaryMarker) {
				blocks++
			}
		}
		s.LessOrEqual(blocks, 1, "request %d carries more than one summary block", i)
		if idx := appcontext.SummaryBlockIndex(req); idx >= 0 {
			s.Equal(len(head), idx, "request %d moved the summary block", i)
		}
		s.assertToolPairing(req, i)
	}

	breaks := 0
	for i := 1; i < len(requests); i++ {
		prev, cur := requests[i-1], requests[i]
		if len(cur) >= len(prev) && mustJSON(s, cur[:len(prev)]) == mustJSON(s, prev) {
			continue
		}
		breaks++
	}
	s.Equal(len(sum.inputs), breaks,
		"the prefix may only change when the history is summarised: %d trims, %d prefix breaks", len(sum.inputs), breaks)
	s.Less(breaks*4, len(requests),
		"hysteresis failed: trimming on %d of %d turns is the per-turn churn this replaced", breaks, len(requests))
}

func (s *StableHistorySuite) TestEachTrimCarriesThePreviousSummary() {
	llm := &recordingLLM{}
	reg := &echoRegistry{payload: strings.Repeat("x", 1000)}
	sum := &countingSummarizer{}

	loop := agent.NewLoop(llm, reg, 45, 45, 16000)
	loop.SetHistoryBudget(appcontext.Budget{
		MaxTokens:          12000,
		ReserveOutput:      2000,
		SummarizeThreshold: 7000,
		KeepRecentMessages: 4,
	})
	loop.SetSummarizer(sum)

	_, err := loop.RunTask(context.Background(), runHead(), "test-model", domain.LLMProviderAnthropic, domain.ToolPolicy{})
	s.Require().Error(err)
	s.Require().GreaterOrEqual(len(sum.inputs), 2)

	for i := 1; i < len(sum.inputs); i++ {
		first := sum.inputs[i][0]
		s.True(strings.HasPrefix(first.Content, appcontext.SummaryMarker),
			"summarize call %d was not given the block it is replacing: %q", i, first.Content)
		s.Contains(first.Content, fmt.Sprintf("condensed span %d", i))
	}
	for i, input := range sum.inputs {
		s.NotContains(mustJSON(s, input), "PERSONA", "summarize call %d re-sent the head", i)
	}
}

func (s *StableHistorySuite) TestNoSummarizerFallsBackToDropping() {
	llm := &recordingLLM{}
	reg := &echoRegistry{payload: strings.Repeat("x", 1000)}

	loop := agent.NewLoop(llm, reg, 45, 45, 16000)
	budget := appcontext.Budget{
		MaxTokens:          12000,
		ReserveOutput:      2000,
		SummarizeThreshold: 7000,
		KeepRecentMessages: 4,
	}
	loop.SetHistoryBudget(budget)

	head := runHead()
	_, err := loop.RunTask(context.Background(), head, "test-model", domain.LLMProviderAnthropic, domain.ToolPolicy{})
	s.Require().Error(err)
	s.Require().NotEmpty(llm.requests)

	trimmed := false
	for i, req := range llm.requests {
		s.LessOrEqual(appcontext.CountTokens(req), budget.TokenLimit(), "request %d does not fit the budget", i)
		s.Equal(-1, appcontext.SummaryBlockIndex(req), "request %d has a summary block with no summarizer", i)
		if i > 0 {
			prev := llm.requests[i-1]
			if len(req) < len(prev) || mustJSON(s, req[:len(prev)]) != mustJSON(s, prev) {
				trimmed = true
			}
		}
		s.assertToolPairing(req, i)
	}
	s.True(trimmed, "the run must have grown past the budget and been cut")
}

func (s *StableHistorySuite) TestLightModelAppliesToUtilityCallsNotMainIterations() {
	llm := &recordingLLM{}
	reg := &echoRegistry{payload: strings.Repeat("x", 1000)}
	sum := &countingSummarizer{}

	loop := agent.NewLoop(llm, reg, 45, 45, 16000)
	loop.SetHistoryBudget(appcontext.Budget{
		MaxTokens:          12000,
		ReserveOutput:      2000,
		SummarizeThreshold: 7000,
		KeepRecentMessages: 4,
	})
	loop.SetSummarizer(sum)

	const runModel = "run-model"
	const lightModel = "light-model"

	_, err := loop.RunTask(context.Background(), runHead(), runModel, domain.LLMProviderAnthropic, domain.ToolPolicy{},
		agent.WithLightModel(lightModel))

	s.Require().Error(err)

	s.Require().GreaterOrEqual(len(sum.models), 2, "the run must have trimmed at least twice")
	for i, m := range sum.models {
		s.Equal(lightModel, m, "summarize call %d ran on %q, want the light model", i, m)
	}

	s.Require().NotEmpty(llm.models)
	wrapUpModel := llm.models[len(llm.models)-1]
	s.Equal(lightModel, wrapUpModel, "the wrap-up turn ran on %q, want the light model", wrapUpModel)
	for i, m := range llm.models[:len(llm.models)-1] {
		s.Equal(runModel, m, "iteration %d ran on %q, want the run model", i, m)
	}
}

func (s *StableHistorySuite) TestNoLightModelOptionKeepsTheRunModelEverywhere() {
	llm := &recordingLLM{}
	reg := &echoRegistry{payload: strings.Repeat("x", 1000)}
	sum := &countingSummarizer{}

	loop := agent.NewLoop(llm, reg, 45, 45, 16000)
	loop.SetHistoryBudget(appcontext.Budget{
		MaxTokens:          12000,
		ReserveOutput:      2000,
		SummarizeThreshold: 7000,
		KeepRecentMessages: 4,
	})
	loop.SetSummarizer(sum)

	const runModel = "run-model"

	_, err := loop.RunTask(context.Background(), runHead(), runModel, domain.LLMProviderAnthropic, domain.ToolPolicy{})
	s.Require().Error(err)

	s.Require().NotEmpty(sum.models)
	for i, m := range sum.models {
		s.Equal(runModel, m, "summarize call %d ran on %q, want the run model", i, m)
	}
	s.Require().NotEmpty(llm.models)
	for i, m := range llm.models {
		s.Equal(runModel, m, "call %d ran on %q, want the run model", i, m)
	}
}

func (s *StableHistorySuite) assertToolPairing(messages []domain.Message, req int) {
	s.T().Helper()
	for i, m := range messages {
		if m.Role != domain.RoleTool {
			continue
		}
		j := i - 1
		for j >= 0 && messages[j].Role == domain.RoleTool {
			j--
		}
		s.Require().GreaterOrEqual(j, 0, "request %d: tool result at %d opens the conversation", req, i)
		s.Require().Equal(domain.RoleAssistant, messages[j].Role,
			"request %d: tool result at %d follows a %s", req, i, messages[j].Role)
		found := false
		for _, tc := range messages[j].ToolCalls {
			if tc.ID == m.ToolCallID {
				found = true
			}
		}
		s.True(found, "request %d: tool result %q at %d is orphaned", req, m.ToolCallID, i)
	}
}
