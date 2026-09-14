package agent

import "context"

type segmentBreakKey struct{}

func WithSegmentBreak(ctx context.Context, fn func()) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, segmentBreakKey{}, fn)
}

func segmentBreak(ctx context.Context) {
	if fn, ok := ctx.Value(segmentBreakKey{}).(func()); ok && fn != nil {
		fn()
	}
}

func SegmentBreak(ctx context.Context) { segmentBreak(ctx) }
