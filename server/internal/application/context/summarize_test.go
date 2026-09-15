package context

import (
	gocontext "context"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// capturingChatClient is a minimal port.LLMClient fake that records the last
// request it was sent, so the summarizer's own request shape can be asserted
// without a real provider.
type capturingChatClient struct {
	seen  domain.AgentRequest
	reply string
}

func (c *capturingChatClient) Chat(_ gocontext.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	c.seen = req
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: c.reply}}, nil
}

func (c *capturingChatClient) ChatStream(ctx gocontext.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return c.Chat(ctx, req)
}

func (c *capturingChatClient) Models(gocontext.Context) ([]string, error) { return nil, nil }

func (c *capturingChatClient) Embed(gocontext.Context, string, string) ([]float32, error) {
	return nil, nil
}

// The summarize call condenses pages of dropped history into one paragraph;
// it must cap its own output rather than inherit whatever the caller's own
// turn was allowed, which the loop enforces separately (see agent.Loop) and
// which is usually far larger than a summary ever needs to be.
func TestSummarizeForCapsItsOwnRequestAtSummarizeMaxTokens(t *testing.T) {
	client := &capturingChatClient{reply: "condensed"}
	s := NewLLMSummarizer(client)

	messages := []domain.Message{{Role: domain.RoleUser, Content: "did a bunch of work"}}
	if _, err := s.SummarizeFor(gocontext.Background(), messages, "test-model", domain.LLMProviderAnthropic); err != nil {
		t.Fatalf("SummarizeFor: %v", err)
	}

	if client.seen.MaxTokens != summarizeMaxTokens {
		t.Errorf("max tokens = %d, want %d", client.seen.MaxTokens, summarizeMaxTokens)
	}
	if client.seen.Model != "test-model" || client.seen.ProviderType != domain.LLMProviderAnthropic {
		t.Errorf("model/provider = %q/%q, want test-model/anthropic", client.seen.Model, client.seen.ProviderType)
	}
}

// A rolling summarize must carry the conversation's PROVIDER, not just its
// model.
//
// This is the housekeeping call that broke a long chat with an agent on a
// host-executed provider: the model on such an agent is a CLI routing alias
// ("opus", "sonnet[1m]"), and SummarizeRolling sent it with no provider — so it
// was routed at the default provider, which answers 400 to a name that means
// nothing to it. Named, the provider reaches the llm client, which is the one
// place that knows to redirect the call AND drop the alias with it.
func TestSummarizeRollingForCarriesTheProvider(t *testing.T) {
	client := &capturingChatClient{reply: "condensed"}
	s := NewLLMSummarizer(client)

	// A budget that is certain to trigger: threshold 1 token, keep nothing.
	budget := Budget{SummarizeThreshold: 1, KeepRecentMessages: 1}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: "you are an agent"},
		{Role: domain.RoleUser, Content: "a long first turn worth condensing"},
		{Role: domain.RoleAssistant, Content: "an answer"},
		{Role: domain.RoleUser, Content: "the newest turn"},
	}

	out, err := SummarizeRollingFor(gocontext.Background(), budget, s, messages, "sonnet[1m]", domain.LLMProviderClaudeCode)
	if err != nil {
		t.Fatalf("SummarizeRollingFor: %v", err)
	}
	if len(out) >= len(messages) {
		t.Fatalf("nothing was summarized: %d messages in, %d out", len(messages), len(out))
	}
	if client.seen.ProviderType != domain.LLMProviderClaudeCode {
		t.Errorf("provider = %q, want the conversation's own", client.seen.ProviderType)
	}
	if client.seen.Model != "sonnet[1m]" {
		t.Errorf("model = %q, want the conversation's own", client.seen.Model)
	}
}
