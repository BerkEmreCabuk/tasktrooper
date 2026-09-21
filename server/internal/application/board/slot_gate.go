package board

import (
	"context"
	"sync"
)

// slotGate is a resizable semaphore for the runner's concurrency limits. It
// admits at most limit waiters at once; limit 0 admits everyone (unlimited).
// setLimit broadcasts so a lowering takes effect on the next run that is about
// to wait, and a raising wakes everyone already waiting on the old, lower cap.
type slotGate struct {
	mu    sync.Mutex
	cond  *sync.Cond
	limit int
	used  int
}

func newSlotGate() *slotGate {
	g := &slotGate{}
	g.cond = sync.NewCond(&g.mu)
	return g
}

func (g *slotGate) setLimit(n int) {
	if n < 0 {
		n = 0
	}
	g.mu.Lock()
	g.limit = n
	g.cond.Broadcast()
	g.mu.Unlock()
}

// acquire blocks until a slot is free or ctx is done, reporting whether the
// caller got a slot. The cond has no way to select on ctx, so a watcher wakes
// every waiter when the context fires and each re-checks ctx.Err() itself.
func (g *slotGate) acquire(ctx context.Context) bool {
	stop := context.AfterFunc(ctx, func() { g.cond.Broadcast() })
	defer stop()
	g.mu.Lock()
	defer g.mu.Unlock()
	for ctx.Err() == nil && g.limit > 0 && g.used >= g.limit {
		g.cond.Wait()
	}
	if ctx.Err() != nil {
		return false
	}
	g.used++
	return true
}

func (g *slotGate) release() {
	g.mu.Lock()
	if g.used > 0 {
		g.used--
	}
	g.cond.Signal()
	g.mu.Unlock()
}