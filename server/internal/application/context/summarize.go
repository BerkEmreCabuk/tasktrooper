package context

import (
	gocontext "context"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Summarizer interface {
	Summarize(ctx gocontext.Context, messages []domain.Message, model string) (string, error)
}

// ProviderSummarizer points the summary at one provider; a board run must summarize with its own provider's model, and unnamed that model routes to a default client that cannot serve it. Discovered by type assertion so plain Summarizer callers are untouched.
type ProviderSummarizer interface {
	Summarizer
	SummarizeFor(ctx gocontext.Context, messages []domain.Message, model string, provider domain.LLMProviderType) (string, error)
}

// Near the agent loop's own output budget the call would defeat the point of condensing at all.
const summarizeMaxTokens = 1024

type NoOpSummarizer struct{}

func (NoOpSummarizer) Summarize(gocontext.Context, []domain.Message, string) (string, error) {
	return "", nil
}

type LLMSummarizer struct {
	client port.LLMClient
}

func NewLLMSummarizer(client port.LLMClient) *LLMSummarizer {
	return &LLMSummarizer{client: client}
}

// Summarize condenses a span using the configured default provider; callers that know the conversation's provider should use SummarizeFor.
func (s *LLMSummarizer) Summarize(ctx gocontext.Context, messages []domain.Message, model string) (string, error) {
	return s.SummarizeFor(ctx, messages, model, "")
}

// SummarizeFor is Summarize routed at a named provider; empty keeps the default.
func (s *LLMSummarizer) SummarizeFor(
	ctx gocontext.Context,
	messages []domain.Message,
	model string,
	provider domain.LLMProviderType,
) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	resp, err := s.client.Chat(ctx, domain.AgentRequest{
		Messages: []domain.Message{
			{
				Role:    domain.RoleSystem,
				Content: "Summarize the following conversation history concisely. Preserve key facts, decisions, file paths, and tool outcomes. Write in the same language as the conversation.",
			},
			{Role: domain.RoleUser, Content: formatMessagesForSummary(messages)},
		},
		Model:        model,
		ProviderType: provider,
		MaxTokens:    summarizeMaxTokens,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Message.Content), nil
}

// summarizeWith uses the provider-routed call when the summarizer offers one.
func summarizeWith(
	ctx gocontext.Context,
	s Summarizer,
	messages []domain.Message,
	model string,
	provider domain.LLMProviderType,
) (string, error) {
	if ps, ok := s.(ProviderSummarizer); ok {
		return ps.SummarizeFor(ctx, messages, model, provider)
	}
	return s.Summarize(ctx, messages, model)
}

func SummarizeRolling(ctx gocontext.Context, b Budget, s Summarizer, messages []domain.Message, model string) ([]domain.Message, error) {
	return SummarizeRollingFor(ctx, b, s, messages, model, "")
}

// SummarizeRollingFor is SummarizeRolling routed at the conversation's provider; a host-executed alias like "opus" or "sonnet[1m]" means nothing to an HTTP endpoint, so the model alone sent a past run's housekeeping into a 400.
func SummarizeRollingFor(
	ctx gocontext.Context,
	b Budget,
	s Summarizer,
	messages []domain.Message,
	model string,
	provider domain.LLMProviderType,
) ([]domain.Message, error) {
	if s == nil || CountTokens(messages) <= b.SummarizeThreshold {
		out := make([]domain.Message, len(messages))
		copy(out, messages)
		return out, nil
	}

	system, middle, recent := splitForSummary(messages, b.KeepRecentMessages)
	if len(middle) == 0 {
		out := make([]domain.Message, len(messages))
		copy(out, messages)
		return out, nil
	}

	summary, err := summarizeWith(ctx, s, middle, model, provider)
	if err != nil {
		return nil, err
	}
	if summary == "" {
		out := make([]domain.Message, len(messages))
		copy(out, messages)
		return out, nil
	}

	out := make([]domain.Message, 0, len(system)+1+len(recent))
	out = append(out, system...)
	out = append(out, domain.Message{
		Role:    domain.RoleSystem,
		Content: "Conversation summary:\n" + summary,
	})
	out = append(out, recent...)
	return out, nil
}

func splitForSummary(messages []domain.Message, keepRecent int) (system, middle, recent []domain.Message) {
	if len(messages) == 0 {
		return nil, nil, nil
	}
	if keepRecent < 0 {
		keepRecent = 0
	}
	if keepRecent > len(messages) {
		keepRecent = len(messages)
	}

	splitAt := len(messages) - keepRecent
	recent = append(recent, messages[splitAt:]...)

	for _, m := range messages[:splitAt] {
		if m.Role == domain.RoleSystem {
			system = append(system, m)
			continue
		}
		middle = append(middle, m)
	}
	return system, middle, recent
}

func formatMessagesForSummary(messages []domain.Message) string {
	var b strings.Builder
	for _, m := range messages {
		b.WriteString(string(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		// Base64 would dwarf the summarized text; a count keeps the fact the images existed.
		if len(m.Images) > 0 {
			b.WriteString(" [")
			b.WriteString(strconv.Itoa(len(m.Images)))
			b.WriteString(" image(s) attached]")
		}
		if m.Name != "" {
			b.WriteString(" [")
			b.WriteString(m.Name)
			b.WriteString("]")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
