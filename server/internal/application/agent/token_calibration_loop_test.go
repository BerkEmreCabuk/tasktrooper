package agent_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func lightHead() []domain.Message {
	return []domain.Message{
		{Role: domain.RoleSystem, Content: "persona"},
		{Role: domain.RoleUser, Content: "TRIGGER: implement the thing"},
	}
}

func minimalToolDefs() []domain.ToolDefinition {
	return []domain.ToolDefinition{{Type: "function", Function: domain.FunctionDefinition{Name: "read_file"}}}
}

func fatToolDefs(n, descLen int) []domain.ToolDefinition {
	defs := make([]domain.ToolDefinition, n)
	for i := range defs {
		defs[i] = domain.ToolDefinition{
			Type: "function",
			Function: domain.FunctionDefinition{
				Name:        fmt.Sprintf("tool_%d", i),
				Description: strings.Repeat("d", descLen),
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"arg": map[string]interface{}{"type": "string"},
					},
				},
			},
		}
	}
	return defs
}

type toolCatalogRegistry struct {
	payload string
	defs    []domain.ToolDefinition
}

func (r *toolCatalogRegistry) Register(port.ToolExecutor) {}

func (r *toolCatalogRegistry) Definitions() []domain.ToolDefinition { return r.defs }

func (r *toolCatalogRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition {
	return r.defs
}

func (r *toolCatalogRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return r.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (r *toolCatalogRegistry) ExecuteWithPolicy(_ context.Context, call domain.ToolCall, _ domain.ToolPolicy) domain.ToolResult {
	return domain.ToolResult{
		ToolCallID: call.ID,
		Name:       call.Function.Name,
		Content:    call.Function.Arguments + " => " + r.payload,
	}
}

func (r *toolCatalogRegistry) AllToolNames() []string { return []string{"read_file"} }

type scaledUsageLLM struct {
	multiplier float64
	turn       int
	requests   [][]domain.Message
}

func (f *scaledUsageLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	f.requests = append(f.requests, append([]domain.Message(nil), req.Messages...))
	f.turn++
	if len(req.Tools) == 0 {
		return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "wrapped up"}}, nil
	}
	estimated := appcontext.CountTokens(req.Messages)
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
		Usage: domain.Usage{PromptTokens: int(float64(estimated) * f.multiplier), CompletionTokens: 50},
	}, nil
}

func (f *scaledUsageLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return f.Chat(ctx, req)
}

func (f *scaledUsageLLM) Models(context.Context) ([]string, error)                 { return nil, nil }
func (f *scaledUsageLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

func firstBreakIndex(s *StableHistorySuite, requests [][]domain.Message) int {
	for i := 1; i < len(requests); i++ {
		prev, cur := requests[i-1], requests[i]
		if len(cur) >= len(prev) && mustJSON(s, cur[:len(prev)]) == mustJSON(s, prev) {
			continue
		}
		return i
	}
	return -1
}

func (s *StableHistorySuite) TestFatToolCatalogTrimsEarlierThanMinimalCatalog() {
	budget := appcontext.Budget{
		MaxTokens:          4000,
		ReserveOutput:      200,
		SummarizeThreshold: 3000,
		KeepRecentMessages: 4,
	}

	run := func(defs []domain.ToolDefinition) int {
		llm := &recordingLLM{}
		reg := &toolCatalogRegistry{payload: strings.Repeat("x", 300), defs: defs}
		sum := &countingSummarizer{}
		loop := agent.NewLoop(llm, reg, 20, 20, 16000)
		loop.SetHistoryBudget(budget)
		loop.SetSummarizer(sum)

		_, err := loop.RunTask(context.Background(), lightHead(), "test-model", domain.LLMProviderAnthropic, domain.ToolPolicy{})
		s.Require().Error(err, "the run is designed to spend its whole iteration budget")

		return firstBreakIndex(s, llm.requests)
	}

	minimalBreak := run(minimalToolDefs())
	fatBreak := run(fatToolDefs(30, 500))

	s.Require().NotEqual(-1, fatBreak, "the fat-catalog run must have trimmed at least once in the iteration budget given")
	if minimalBreak == -1 {
		return
	}
	s.Less(fatBreak, minimalBreak,
		"a run with a large tool schema catalog (%d) must trim earlier than one with a minimal catalog (%d), same messages",
		fatBreak, minimalBreak)
}

func (s *StableHistorySuite) TestCalibratedRatioTrimsEarlierThanAnAccurateEstimate() {
	budget := appcontext.Budget{
		MaxTokens:          4000,
		ReserveOutput:      200,
		SummarizeThreshold: 3000,
		KeepRecentMessages: 4,
	}
	reg := &toolCatalogRegistry{payload: strings.Repeat("x", 300), defs: minimalToolDefs()}

	run := func(multiplier float64) int {
		llm := &scaledUsageLLM{multiplier: multiplier}
		sum := &countingSummarizer{}
		loop := agent.NewLoop(llm, reg, 20, 20, 16000)
		loop.SetHistoryBudget(budget)
		loop.SetSummarizer(sum)

		_, err := loop.RunTask(context.Background(), lightHead(), "test-model", domain.LLMProviderAnthropic, domain.ToolPolicy{})
		s.Require().Error(err, "the run is designed to spend its whole iteration budget")

		return firstBreakIndex(s, llm.requests)
	}

	accurateBreak := run(1.0)
	calibratedBreak := run(2.0)

	s.Require().NotEqual(-1, calibratedBreak, "the 2x-real-usage run must have trimmed at least once in the iteration budget given")
	if accurateBreak == -1 {
		return
	}
	s.Less(calibratedBreak, accurateBreak,
		"a run whose real usage is 2x the estimate (trim at %d) must trim earlier than one where the estimate was accurate (trim at %d)",
		calibratedBreak, accurateBreak)
}
