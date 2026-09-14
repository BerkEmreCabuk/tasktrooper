package registry

import (
	"context"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type AuditingRegistry struct {
	inner port.ToolRegistry
	audit port.AuditLogger
}

func NewAuditingRegistry(inner port.ToolRegistry, audit port.AuditLogger) port.ToolRegistry {
	if audit == nil {
		return inner
	}
	return &AuditingRegistry{inner: inner, audit: audit}
}

func (a *AuditingRegistry) Register(executor port.ToolExecutor) {
	a.inner.Register(executor)
}

func (a *AuditingRegistry) Definitions() []domain.ToolDefinition {
	return a.inner.Definitions()
}

func (a *AuditingRegistry) DefinitionsForPolicy(policy domain.ToolPolicy) []domain.ToolDefinition {
	return a.inner.DefinitionsForPolicy(policy)
}

func (a *AuditingRegistry) AllToolNames() []string {
	return a.inner.AllToolNames()
}

func (a *AuditingRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return a.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (a *AuditingRegistry) ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, policy domain.ToolPolicy) domain.ToolResult {
	start := time.Now()
	result := a.inner.ExecuteWithPolicy(ctx, call, policy)
	if a.audit != nil {
		// Byte-safe: a tool result or an argument blob is LLM text, and a cut
		// through a rune makes the audit row unwritable (see domain.TruncateHead).
		preview := domain.TruncateHead(result.Content, 500)
		args := domain.TruncateHead(call.Function.Arguments, 1000)
		entry := domain.AuditEntry{
			RequestID:     RequestIDFromContext(ctx),
			APIKeyName:    APIKeyNameFromContext(ctx),
			ToolName:      call.Function.Name,
			Arguments:     args,
			ResultPreview: preview,
			DurationMs:    time.Since(start).Milliseconds(),
			IsError:       result.IsError,
		}
		_ = a.audit.Log(ctx, entry)
	}
	return result
}

type contextKey string

const requestIDKey contextKey = "request_id"
const apiKeyNameKey contextKey = "api_key_name"
const workspaceDirKey contextKey = "workspace_dir"
const subtaskWorkspaceKey contextKey = "subtask_workspace"
const sessionIDKey contextKey = "session_id"
const repositoryIDKey contextKey = "repository_id"
const agentIDKey contextKey = "agent_id"
const taskIDKey contextKey = "task_id"
const actorUserIDKey contextKey = "actor_user_id"

func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func ContextWithAPIKeyName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, apiKeyNameKey, name)
}

// ContextWithActorUserID carries the Firebase UID of the human whose action
// (a board move, a comment) produced this request, resolved from the signed
// X-Internal-Actor header. Absent whenever the caller is not the SPA behind
// the cloud gateway — a local agent run, self-hosted/desktop, or any request
// the gateway did not attach an actor to.
func ContextWithActorUserID(ctx context.Context, uid string) context.Context {
	return context.WithValue(ctx, actorUserIDKey, uid)
}

// ActorUserIDFromContext returns the human actor's uid, or "" when none was
// resolved for this request.
func ActorUserIDFromContext(ctx context.Context) string {
	if v := ctx.Value(actorUserIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(requestIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func APIKeyNameFromContext(ctx context.Context) string {
	if v := ctx.Value(apiKeyNameKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
