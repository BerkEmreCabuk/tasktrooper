package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TaskExecutor runs one board task to a finished answer WITHOUT going through
// the in-process agent loop.
//
// It exists so the board runner has exactly one branch to make — "is there an
// executor for this agent's provider" — instead of knowing anything about how
// the work gets done. Everything on either side of the call is unchanged: the
// runner still clones the task workspace and checks out tt-<key> before, and
// still runs the grounding checks, the verify gate, the commit/PR and the
// column advance after. Only the middle is swapped.
//
// The interface is deliberately narrow (two methods, one value-object
// argument) because it is a seam, not an abstraction layer: there is one
// implementation today (the Claude Code CLI) and the shape must not grow to
// accommodate a second one before it exists.
type TaskExecutor interface {
	// Supports reports whether this executor handles runs on that provider AND
	// is presently able to. It is asked per run rather than answered once at
	// boot so an executor that is registered but temporarily unusable can say
	// so — and so the runner's fallback is a decision, not an assumption.
	Supports(provider domain.LLMProviderType) bool
	// Execute runs the task. The response is the same shape the agent loop
	// returns, so every post-run step reads it identically.
	//
	// A *domain.QuotaBlock error means the run was parked, not that it failed:
	// the caller must park the task rather than spend one of its
	// consecutive-failure lives (see application/board/runner.go).
	Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error)
}

// ChatExecutor is the same seam, for a chat turn instead of a board task.
//
// It is a SECOND interface rather than a third method on TaskExecutor because
// the two have different callers with different needs: the board runner has no
// use for streaming and the session service has no use for Execute, and one
// interface would force each to depend on the other's half. The one
// implementation (the Claude Code CLI adapter) satisfies both, and
// platform/runtime hands the same instance to both callers.
//
// It exists at all because the alternative was a chat that could not work.
// Board runs on the claude_code provider went through this seam from the day it
// was added; chat did not, so a claude_code agent's chat turn fell through to
// the HTTP LLM client — which for a provider with no base URL and no key built
// an empty endpoint and failed three times over. See
// domain.ErrHostExecutedProvider, which is now what that path returns instead.
type ChatExecutor interface {
	// Supports reports whether this executor handles chat on that provider AND
	// is presently able to — the same question TaskExecutor.Supports answers,
	// asked per turn for the same reason.
	Supports(provider domain.LLMProviderType) bool
	// ExecuteChat runs one conversational turn, streaming what the assistant
	// says into out as it is produced.
	//
	// A *domain.QuotaBlock error means the subscription is spent. Unlike a board
	// task there is nothing to park — no card, no sweeper — so the caller must
	// turn it into a sentence the person who just typed can act on. See
	// domain.QuotaBlock.UserMessage.
	ExecuteChat(ctx context.Context, req domain.ChatExecution, out ChatStream) (domain.ChatResult, error)
}

// ChatStream is how a chat turn's output reaches the caller while it is still
// being produced.
//
// It is a struct rather than a bare onToken func because a streamed turn has two
// kinds of event, and the existing SSE transcript already distinguishes them:
// text, and the boundary where a stretch of reasoning ends. Both fields are
// optional; a nil field is simply not reported.
type ChatStream struct {
	// OnText receives assistant text as it arrives, token-ish rather than
	// turn-at-a-time. It is wired to the same capture callback agent.Loop's
	// RunStream is given, so the web SSE transcript is fed identically whether a
	// turn came from the loop or from a CLI session.
	OnText func(string)
	// OnSegmentBreak closes a stretch of reasoning: everything streamed before
	// it was the agent thinking out loud on its way to a tool call, and only the
	// final segment is the answer.
	//
	// It matters more here than in the loop. A CLI session narrates freely
	// between tool calls, and its terminal result event carries ONLY the closing
	// answer — so without this boundary the client would render the whole
	// narration as the reply and then have it replaced by the shorter text that
	// gets persisted. With it, what is on screen and what is in the transcript
	// are the same thing. The session service wires this to
	// agent.WithSegmentBreak's reporter, which is what the SSE handler already
	// listens to.
	OnSegmentBreak func()
}

// text reports s to OnText when anyone is listening.
func (c ChatStream) Text(s string) {
	if c.OnText != nil {
		c.OnText(s)
	}
}

// SegmentBreak closes the current stretch of streamed text, if anyone asked.
func (c ChatStream) SegmentBreak() {
	if c.OnSegmentBreak != nil {
		c.OnSegmentBreak()
	}
}
