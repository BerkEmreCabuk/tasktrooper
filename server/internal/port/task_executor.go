package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TaskExecutor runs one board task to a finished answer without going through
// the in-process agent loop. It is a seam, not an abstraction layer: one
// implementation today (the Claude Code CLI), and the runner's only branch is
// "is there an executor for this provider".
type TaskExecutor interface {
	// Asked per run rather than answered once at boot, so an executor that is
	// registered but temporarily unusable can say so.
	Supports(provider domain.LLMProviderType) bool
	// Response is the same shape the agent loop returns, so every post-run
	// step reads it identically.
	//
	// A *domain.QuotaBlock error means the run was parked, not failed: the
	// caller must park the task rather than spend a consecutive-failure life.
	Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error)
}

// ChatExecutor is the same seam, for a chat turn instead of a board task. A
// SECOND interface because the two callers have no use for each other's half;
// the one implementation satisfies both.
type ChatExecutor interface {
	// The same question TaskExecutor.Supports answers, asked per turn.
	Supports(provider domain.LLMProviderType) bool
	// Streams the assistant's text into out as it is produced.
	//
	// A *domain.QuotaBlock error means the subscription is spent; there is
	// nothing to park for a chat, so the caller must turn it into a sentence
	// the person who typed can act on. See domain.QuotaBlock.UserMessage.
	ExecuteChat(ctx context.Context, req domain.ChatExecution, out ChatStream) (domain.ChatResult, error)
}

// ChatStream carries a streamed turn's two kinds of event: text, and the
// boundary where a stretch of reasoning ends. A nil field is simply not
// reported.
type ChatStream struct {
	// Wired to the same capture callback agent.Loop's RunStream is given, so
	// the SSE transcript is fed identically whether a turn came from the loop
	// or from a CLI session.
	OnText func(string)
	// A CLI session narrates freely between tool calls and its terminal event
	// carries ONLY the closing answer, so without this boundary the client
	// would render the whole narration as the reply before it is replaced by
	// what gets persisted.
	OnSegmentBreak func()
}

func (c ChatStream) Text(s string) {
	if c.OnText != nil {
		c.OnText(s)
	}
}

func (c ChatStream) SegmentBreak() {
	if c.OnSegmentBreak != nil {
		c.OnSegmentBreak()
	}
}
