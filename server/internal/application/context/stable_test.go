package context

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type StableTrimSuite struct {
	suite.Suite
}

func TestStableTrimSuite(t *testing.T) {
	suite.Run(t, new(StableTrimSuite))
}

type recordingSummarizer struct {
	calls    [][]domain.Message
	models   []string
	provider []domain.LLMProviderType
	err      error
	reply    func(call int) string
}

func (r *recordingSummarizer) Summarize(_ gocontext.Context, messages []domain.Message, model string) (string, error) {
	r.calls = append(r.calls, append([]domain.Message(nil), messages...))
	r.models = append(r.models, model)
	if r.err != nil {
		return "", r.err
	}
	if r.reply != nil {
		return r.reply(len(r.calls)), nil
	}
	return fmt.Sprintf("summary of span %d", len(r.calls)), nil
}

type providerRecordingSummarizer struct {
	recordingSummarizer
}

func (p *providerRecordingSummarizer) SummarizeFor(
	ctx gocontext.Context,
	messages []domain.Message,
	model string,
	provider domain.LLMProviderType,
) (string, error) {
	p.provider = append(p.provider, provider)
	return p.recordingSummarizer.Summarize(ctx, messages, model)
}

// Ratios mirror the shipped config; the hysteresis properties are about proportion, not absolute numbers.
func stableBudget() Budget {
	return Budget{MaxTokens: 40000, ReserveOutput: 4000, SummarizeThreshold: 24000, KeepRecentMessages: 4}
}

func stableHead() []domain.Message {
	return []domain.Message{
		{Role: domain.RoleSystem, Content: "PERSONA " + repeat("p", 6000)},
		{Role: domain.RoleSystem, Content: "PROJECT " + repeat("j", 6000)},
		{Role: domain.RoleUser, Content: "TRIGGER: implement the thing"},
	}
}

func toolTurns(history []domain.Message, from, n int) []domain.Message {
	for i := from; i < from+n; i++ {
		id := fmt.Sprintf("tc%d", i)
		history = append(history, domain.Message{
			Role:    domain.RoleAssistant,
			Content: fmt.Sprintf("reading file %d", i),
			ToolCalls: []domain.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: domain.FunctionCall{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.go"}`, i)},
			}},
		})
		history = append(history, domain.Message{
			Role:       domain.RoleTool,
			ToolCallID: id,
			Name:       "read_file",
			Content:    fmt.Sprintf("file %d: ", i) + repeat("x", 4000),
		})
	}
	return history
}

func encode(t interface{ Fatal(...any) }, messages []domain.Message) string {
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (s *StableTrimSuite) assertToolPairing(messages []domain.Message) {
	s.T().Helper()
	for i, m := range messages {
		if m.Role != domain.RoleTool {
			continue
		}
		j := i - 1
		for j >= 0 && messages[j].Role == domain.RoleTool {
			j--
		}
		s.Require().GreaterOrEqual(j, 0, "tool result at %d has no assistant turn in front of it", i)
		s.Require().Equal(domain.RoleAssistant, messages[j].Role,
			"tool result at %d follows a %s, not the assistant turn that called it", i, messages[j].Role)
		var found bool
		for _, tc := range messages[j].ToolCalls {
			if tc.ID == m.ToolCallID {
				found = true
			}
		}
		s.True(found, "tool result %q at %d is orphaned: no matching tool call", m.ToolCallID, i)
	}
}

func (s *StableTrimSuite) TestHeadStaysByteIdenticalAcrossTrims() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	head := stableHead()
	headBytes := encode(s.T(), head)

	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)
	s.Greater(CountTokens(history), b.TokenLimit(), "fixture must actually be over budget")

	first, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(headBytes, encode(s.T(), first[:len(head)]))

	second := toolTurns(first, 40, 40)
	second, ok, err = StableTrim(gocontext.Background(), b, sum, second, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(headBytes, encode(s.T(), second[:len(head)]))
}

func (s *StableTrimSuite) TestExactlyOneSummaryBlockAtTheFixedPosition() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	head := stableHead()

	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)
	for pass := range 3 {
		trimmed, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
		s.Require().NoError(err, "pass %d", pass)
		s.Require().True(ok, "pass %d", pass)

		count := 0
		for _, m := range trimmed {
			if strings.HasPrefix(m.Content, SummaryMarker) {
				count++
			}
		}
		s.Equal(1, count, "pass %d: expected exactly one summary block", pass)
		s.Equal(len(head), SummaryBlockIndex(trimmed), "pass %d: summary must sit right after the head", pass)
		s.Equal(domain.RoleSystem, trimmed[len(head)].Role)

		history = toolTurns(trimmed, 40*(pass+1), 40)
	}
	s.Len(sum.calls, 3)
}

func (s *StableTrimSuite) TestRecentTailSurvivesVerbatim() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	head := stableHead()

	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)
	tail := encode(s.T(), history[len(history)-b.KeepRecentMessages:])

	trimmed, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(tail, encode(s.T(), trimmed[len(trimmed)-b.KeepRecentMessages:]))
}

func (s *StableTrimSuite) TestSecondTrimFeedsTheFirstSummaryBackIn() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	head := stableHead()

	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)
	first, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)

	second := toolTurns(first, 40, 40)
	_, ok, err = StableTrim(gocontext.Background(), b, sum, second, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)

	s.Require().Len(sum.calls, 2)
	s.Require().NotEmpty(sum.calls[1])
	s.True(strings.HasPrefix(sum.calls[1][0].Content, SummaryMarker),
		"the second summarize call must be handed the first summary, got %q", sum.calls[1][0].Content)
	s.Contains(sum.calls[1][0].Content, "summary of span 1")
	for _, m := range sum.calls[1] {
		s.NotContains(m.Content, "PERSONA")
	}
}

func (s *StableTrimSuite) TestCutsToTheLowerWatermarkNotTheCeiling() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	head := stableHead()

	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)
	trimmed, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)

	s.LessOrEqual(CountTokens(trimmed), b.SummarizeThreshold,
		"a trim must reach the watermark so the next several turns need no trim at all")
	s.Less(CountTokens(trimmed), b.TokenLimit())
}

func (s *StableTrimSuite) TestDropsNoMoreThanTheWatermarkNeeds() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	head := stableHead()

	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)
	trimmed, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)

	s.Greater(len(trimmed)-len(head)-1, b.KeepRecentMessages,
		"expected turns beyond the protected tail to survive the cut")
}

func (s *StableTrimSuite) TestNeverOrphansAToolResult() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	head := stableHead()

	// Odd keep counts land "last N messages" mid-tool-pair on purpose.
	for _, keep := range []int{1, 3, 5, 7} {
		b.KeepRecentMessages = keep
		history := toolTurns(append([]domain.Message(nil), head...), 0, 40)
		trimmed, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
		s.Require().NoError(err)
		s.Require().True(ok, "keep_recent=%d", keep)
		s.assertToolPairing(trimmed)
	}
}

func (s *StableTrimSuite) TestSummaryNeverSplitsTheHeadsToolTurn() {
	b := stableBudget()
	sum := &recordingSummarizer{}

	head := append(stableHead(), domain.Message{
		Role:      domain.RoleAssistant,
		Content:   "opening move",
		ToolCalls: []domain.ToolCall{{ID: "head-tc", Type: "function", Function: domain.FunctionCall{Name: "read_file"}}},
	})
	history := append(append([]domain.Message(nil), head...), domain.Message{
		Role: domain.RoleTool, ToolCallID: "head-tc", Name: "read_file", Content: repeat("h", 400),
	})
	history = toolTurns(history, 0, 40)

	trimmed, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(len(head)+1, SummaryBlockIndex(trimmed), "the head's own tool result must stay glued to its call")
	s.assertToolPairing(trimmed)
}

func (s *StableTrimSuite) TestCapsWhatOneSummarizeCallIsFed() {
	b := Budget{MaxTokens: 200000, ReserveOutput: 1000, SummarizeThreshold: 4000, KeepRecentMessages: 4}
	sum := &recordingSummarizer{}
	head := stableHead()

	history := toolTurns(append([]domain.Message(nil), head...), 0, 300)
	s.Greater(CountTokens(history), b.TokenLimit())

	_, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Require().Len(sum.calls, 1)
	s.LessOrEqual(CountTokens(sum.calls[0]), maxSummaryInputTokens)
	newestDropped := history[len(history)-b.KeepRecentMessages-1]
	s.Equal(newestDropped.Content, sum.calls[0][len(sum.calls[0])-1].Content)
	s.NotContains(encode(s.T(), sum.calls[0]), "file 0: ")
}

func (s *StableTrimSuite) TestNilSummarizerDeclines() {
	b := stableBudget()
	head := stableHead()
	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)

	trimmed, ok, err := StableTrim(gocontext.Background(), b, nil, history, StableTrimOptions{HeadLen: len(head)})
	s.NoError(err)
	s.False(ok)
	s.Nil(trimmed)

	applied := b.Apply(history)
	s.LessOrEqual(CountTokens(applied), b.TokenLimit())
}

func (s *StableTrimSuite) TestSummarizerFailureDeclines() {
	b := stableBudget()
	sum := &recordingSummarizer{err: errors.New("provider is down")}
	head := stableHead()
	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)

	trimmed, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Error(err)
	s.False(ok)
	s.Nil(trimmed)
}

func (s *StableTrimSuite) TestEmptySummaryDeclines() {
	b := stableBudget()
	sum := &recordingSummarizer{reply: func(int) string { return "   " }}
	head := stableHead()
	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)

	_, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.NoError(err)
	s.False(ok)
}

func (s *StableTrimSuite) TestAllHeadDeclines() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	history := toolTurns(append([]domain.Message(nil), stableHead()...), 0, 40)

	_, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(history)})
	s.NoError(err)
	s.False(ok)
	s.Empty(sum.calls, "nothing may be summarized out of a history that is entirely head")
}

func (s *StableTrimSuite) TestUnderBudgetDoesNothing() {
	b := stableBudget()
	sum := &recordingSummarizer{}
	head := stableHead()
	history := toolTurns(append([]domain.Message(nil), head...), 0, 2)

	_, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.NoError(err)
	s.False(ok)
	s.Empty(sum.calls)
}

func (s *StableTrimSuite) TestSummarizeRunsOnTheRunsModelAndProvider() {
	b := stableBudget()
	sum := &providerRecordingSummarizer{}
	head := stableHead()
	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)

	_, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{
		HeadLen:  len(head),
		Model:    "claude-sonnet",
		Provider: domain.LLMProviderAnthropic,
	})
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal([]string{"claude-sonnet"}, sum.models)
	s.Equal([]domain.LLMProviderType{domain.LLMProviderAnthropic}, sum.provider)
}

func (s *StableTrimSuite) TestUnsetWatermarkStillLeavesHeadroom() {
	b := Budget{MaxTokens: 40000, ReserveOutput: 4000, KeepRecentMessages: 4}
	sum := &recordingSummarizer{}
	head := stableHead()
	history := toolTurns(append([]domain.Message(nil), head...), 0, 40)

	trimmed, ok, err := StableTrim(gocontext.Background(), b, sum, history, StableTrimOptions{HeadLen: len(head)})
	s.Require().NoError(err)
	s.Require().True(ok)
	s.LessOrEqual(CountTokens(trimmed), b.TokenLimit()*3/4)
}
