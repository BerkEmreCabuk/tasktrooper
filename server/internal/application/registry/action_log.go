package registry

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ActionRecordingRegistry writes every board-affecting tool call into the
// session's action ledger.
//
// It decorates the registry rather than the agent loop on purpose: chat,
// orchestrated subtasks and board runs all execute tools through the same
// registry, so one decorator covers every path. The loop's own tool trace is
// discarded when the loop returns — this is what survives it.
type ActionRecordingRegistry struct {
	inner port.ToolRegistry
	store port.SessionActionStore
}

func NewActionRecordingRegistry(inner port.ToolRegistry, store port.SessionActionStore) port.ToolRegistry {
	if store == nil {
		return inner
	}
	return &ActionRecordingRegistry{inner: inner, store: store}
}

func (a *ActionRecordingRegistry) Register(executor port.ToolExecutor) {
	a.inner.Register(executor)
}

func (a *ActionRecordingRegistry) Definitions() []domain.ToolDefinition {
	return a.inner.Definitions()
}

func (a *ActionRecordingRegistry) DefinitionsForPolicy(policy domain.ToolPolicy) []domain.ToolDefinition {
	return a.inner.DefinitionsForPolicy(policy)
}

func (a *ActionRecordingRegistry) AllToolNames() []string {
	return a.inner.AllToolNames()
}

func (a *ActionRecordingRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return a.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (a *ActionRecordingRegistry) ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, policy domain.ToolPolicy) domain.ToolResult {
	result := a.inner.ExecuteWithPolicy(ctx, call, policy)
	a.record(ctx, call, result)
	return result
}

func (a *ActionRecordingRegistry) record(ctx context.Context, call domain.ToolCall, result domain.ToolResult) {
	sessionID := SessionIDFromContext(ctx)
	if sessionID == uuid.Nil {
		return
	}
	action, ok := domain.NewSessionAction(call.Function.Name, result.Content, result.IsError)
	if !ok {
		return
	}
	action.SessionID = sessionID
	if agentID := AgentIDFromContext(ctx); agentID != uuid.Nil {
		action.AgentID = &agentID
	}
	if rec := activity.FromContext(ctx); rec != nil {
		if runID := rec.RunID(); runID != uuid.Nil {
			action.RunID = &runID
		}
	}
	if _, err := a.store.AppendAction(ctx, action); err != nil {
		// A ledger write must never fail the tool call the user asked for; the
		// board change already happened.
		log.Warn().Err(err).Str("tool", call.Function.Name).Msg("session action not recorded")
	}
}
