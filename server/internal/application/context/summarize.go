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

// ProviderSummarizer is a Summarizer that can be pointed at one specific
// provider instead of whichever one is configured as the default.
//
// It exists because the model name and the provider are one decision, not two:
// a board run on an Anthropic agent summarizes with that agent's model, and a
// request naming that model with no provider is routed to the default client —
// which does not serve it, so the summary fails and the caller falls back to
// deleting messages. The capability is discovered by type assertion rather than
// required, so the plain Summarizer contract (and every existing caller of it)
// is untouched.
type ProviderSummarizer interface {
	Summarizer
	SummarizeFor(ctx gocontext.Context, messages []domain.Message, model string, provider domain.LLMProviderType) (string, error)
}

// summarizeMaxTokens caps the summary call's own output. It exists to replace
// pages of dropped history with one paragraph, so anything approaching the
// agent loop's own output budget would defeat the point of summarizing at all.
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

// Summarize condenses a span of conversation using whichever provider is the
// configured default. Callers that know which provider the conversation belongs
// to should use SummarizeFor.
func (s *LLMSummarizer) Summarize(ctx gocontext.Context, messages []domain.Message, model string) (string, error) {
	return s.SummarizeFor(ctx, messages, model, "")
}

// SummarizeFor is Summarize routed at a named provider. An empty provider keeps
// the old behaviour: the client picks its default.
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

// SummarizeRolling condenses the middle of a conversation, routed at whichever
// provider the summarizer defaults to. Prefer SummarizeRollingFor.
func SummarizeRolling(ctx gocontext.Context, b Budget, s Summarizer, messages []domain.Message, model string) ([]domain.Message, error) {
	return SummarizeRollingFor(ctx, b, s, messages, model, "")
}

// SummarizeRollingFor is SummarizeRolling routed at the provider the
// conversation belongs to.
//
// The model and the provider are one decision, not two — the same reason
// ProviderSummarizer exists. Passing a model without its provider sends the
// name to whichever client is the default, and it means nothing there: an
// Anthropic model name to an OpenAI endpoint is a 400, and a Claude Code CLI
// alias ("opus", "sonnet[1m]") to ANY HTTP endpoint is a 400. That last case is
// what this signature exists for: a long chat with an agent on a host-executed
// provider crossed the summarize threshold and then failed on its own
// housekeeping, because the alias its board runs on is not a model name anybody
// can serve. Named here, the provider reaches the client, which redirects the
// call to the tenant's default provider AND drops the alias with it.
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
		// Images stay out of the summary prompt on purpose: their base64 would
		// dwarf the text being summarized. A count keeps the fact they existed.
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
