package mocks

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/mock"
)

type ToolRegistry struct {
	mock.Mock
}

func (m *ToolRegistry) Register(executor port.ToolExecutor) {
	m.Called(executor)
}

func (m *ToolRegistry) Definitions() []domain.ToolDefinition {
	args := m.Called()
	return args.Get(0).([]domain.ToolDefinition)
}

func (m *ToolRegistry) DefinitionsForPolicy(policy domain.ToolPolicy) []domain.ToolDefinition {
	args := m.Called(policy)
	return args.Get(0).([]domain.ToolDefinition)
}

func (m *ToolRegistry) AllToolNames() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *ToolRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	args := m.Called(ctx, call)
	return args.Get(0).(domain.ToolResult)
}

func (m *ToolRegistry) ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, policy domain.ToolPolicy) domain.ToolResult {
	args := m.Called(ctx, call, policy)
	return args.Get(0).(domain.ToolResult)
}
