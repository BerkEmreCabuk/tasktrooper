package mocks

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/mock"
)

type LLMClient struct {
	mock.Mock
}

func (m *LLMClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(domain.AgentResponse), args.Error(1)
}

func (m *LLMClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	args := m.Called(ctx, req, onToken)
	return args.Get(0).(domain.AgentResponse), args.Error(1)
}

func (m *LLMClient) Models(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func (m *LLMClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	args := m.Called(ctx, input, model)
	return args.Get(0).([]float32), args.Error(1)
}
