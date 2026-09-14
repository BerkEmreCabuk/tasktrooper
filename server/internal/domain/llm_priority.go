package domain

import "context"

// LLM calls come in two kinds and they share one provider quota: a person is
// waiting for a chat reply, and nobody is waiting for a reindex. When the
// window is full, one of them has to wait — and it must always be the one
// nobody asked for. The kind travels on the context because it is decided where
// the work is started (an indexing goroutine, a webhook) and consumed far away,
// in the provider adapter, with several layers in between that have no business
// knowing about it.
//
// The default is interactive on purpose: an unmarked caller is either a user
// request or a new code path, and the failure mode of guessing "interactive"
// (background work is paced slightly less aggressively) is much cheaper than
// the failure mode of guessing "background" (a person's chat reply queued
// behind an index).
type backgroundLLMKey struct{}

// WithBackgroundLLM marks ctx as work nobody is waiting for. Provider calls
// made under it yield their place in the rate-limit window to interactive ones.
func WithBackgroundLLM(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundLLMKey{}, true)
}

// IsBackgroundLLM reports whether ctx was marked as background work.
func IsBackgroundLLM(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	bg, _ := ctx.Value(backgroundLLMKey{}).(bool)
	return bg
}
