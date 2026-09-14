package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type LLMClient interface {
	Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error)
	ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error)
	Models(ctx context.Context) ([]string, error)
	Embed(ctx context.Context, input string, model string) ([]float32, error)
}
