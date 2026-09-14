package usage

import (
	"context"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TokenTotals is one run's accumulated token spend. PromptTokens keeps the
// domain.Usage contract: it is the TOTAL prompt size, with the cache columns
// as subsets of it.
type TokenTotals struct {
	LLMCalls         int
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// TokenUsage accumulates every chat call's Usage under one context, the same
// way registry.ToolUsage counts tool calls: the board runner plants one in the
// run's context, RecordingClient adds each response to it, and the runner
// stamps the totals onto the task_agent_runs row when the run ends. Safe for
// concurrent use — orchestrator subtasks add from their own goroutines.
type TokenUsage struct {
	mu sync.Mutex
	t  TokenTotals
}

func (u *TokenUsage) Add(usage domain.Usage) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.t.LLMCalls++
	u.t.PromptTokens += int64(usage.PromptTokens)
	u.t.CompletionTokens += int64(usage.CompletionTokens)
	u.t.CacheReadTokens += int64(usage.CacheReadTokens)
	u.t.CacheWriteTokens += int64(usage.CacheWriteTokens)
}

// Totals is nil-safe: an unmeasured run reads as zeros.
func (u *TokenUsage) Totals() TokenTotals {
	if u == nil {
		return TokenTotals{}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.t
}

type tokenUsageKey struct{}

func ContextWithTokenUsage(ctx context.Context) (context.Context, *TokenUsage) {
	u := &TokenUsage{}
	return context.WithValue(ctx, tokenUsageKey{}, u), u
}

func TokenUsageFromContext(ctx context.Context) *TokenUsage {
	u, _ := ctx.Value(tokenUsageKey{}).(*TokenUsage)
	return u
}
