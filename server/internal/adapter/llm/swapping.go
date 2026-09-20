package llm

import (
	"context"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type SwappingClient struct {
	mu     sync.RWMutex
	client port.LLMClient
}

func NewSwappingClient(initial port.LLMClient) *SwappingClient {
	return &SwappingClient{client: initial}
}

func (s *SwappingClient) Set(client port.LLMClient) {
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
}

func (s *SwappingClient) get() port.LLMClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

func (s *SwappingClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	return s.get().Chat(ctx, req)
}

func (s *SwappingClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	return s.get().ChatStream(ctx, req, onToken)
}

func (s *SwappingClient) Models(ctx context.Context) ([]string, error) {
	return s.get().Models(ctx)
}

func (s *SwappingClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	return s.get().Embed(ctx, input, model)
}

func (s *SwappingClient) Unwrap() port.LLMClient { return s.get() }
