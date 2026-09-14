package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ToolExecutor interface {
	Name() string
	Definition() domain.ToolDefinition
	Execute(ctx context.Context, arguments string) domain.ToolResult
}

type ScopedFilesystemExecutor interface {
	ExecuteScoped(ctx context.Context, root string, call domain.ToolCall) domain.ToolResult
}

type ToolRegistry interface {
	Register(executor ToolExecutor)
	Definitions() []domain.ToolDefinition
	DefinitionsForPolicy(policy domain.ToolPolicy) []domain.ToolDefinition
	Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult
	ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, policy domain.ToolPolicy) domain.ToolResult
	AllToolNames() []string
}

type AuditLogger interface {
	Log(ctx context.Context, entry domain.AuditEntry) error
	List(ctx context.Context, limit int) ([]domain.AuditEntry, error)
}
