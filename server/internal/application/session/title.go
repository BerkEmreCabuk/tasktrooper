package session

import (
	gocontext "context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Small on purpose — a title is a handful of words, not a summary.
const titleMaxTokens = 24
const titleFallbackMaxBytes = 60
const titleMaxBytes = 120

type TitleGenerator interface {
	GenerateTitle(ctx gocontext.Context, userMessage, assistantReply, model string, provider domain.LLMProviderType) (string, error)
}

type LLMTitleGenerator struct {
	client port.LLMClient
}

func NewLLMTitleGenerator(client port.LLMClient) *LLMTitleGenerator {
	return &LLMTitleGenerator{client: client}
}

var titleSystemPromptKey = prompt.Define[struct{}]("session.title_system_prompt", struct{}{})

func (g *LLMTitleGenerator) GenerateTitle(ctx gocontext.Context, userMessage, assistantReply, model string, provider domain.LLMProviderType) (string, error) {
	resp, err := g.client.Chat(ctx, domain.AgentRequest{
		Messages: []domain.Message{
			{
				Role:    domain.RoleSystem,
				Content: prompt.Text(titleSystemPromptKey),
			},
			{Role: domain.RoleUser, Content: "User: " + userMessage + "\nAssistant: " + assistantReply},
		},
		Model:        model,
		ProviderType: provider,
		MaxTokens:    titleMaxTokens,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Message.Content), nil
}

// FallbackTitle is what a chat is named when the LLM call fails or returns
// nothing usable: the user's own opening line, trimmed to a sidebar-sized
// title. Never splits a rune (domain.TruncateHead).
func FallbackTitle(userMessage string) string {
	return domain.TruncateHead(strings.TrimSpace(userMessage), titleFallbackMaxBytes)
}
