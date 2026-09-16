package agent

import (
	"context"
	"sync"
)

// CLISession is the host-executed session id a run's follow-up steps share
// with the main run that opened it, so a verify-fix round, a criteria sweep
// or a review verdict answers from the context the CLI session already
// holds instead of paying to rebuild it from scratch.
//
// A holder rather than a bare string: the executor hands back a (possibly
// new) session id after every resumed turn, and each step after the first
// must see the latest one, not the one the main run started with.
type CLISession struct {
	mu sync.Mutex
	id string
}

// ID returns the current session id. Nil-safe: a run that never plumbed a
// holder (a loop run, a bare router call) reads "" and takes the fresh-run
// path, exactly as if no session existed.
func (s *CLISession) ID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// Set records the session id the executor just returned. Nil-safe for the
// same reason ID is: callers that share code with runs carrying no holder
// must not have to branch on it.
func (s *CLISession) Set(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = id
}

type cliSessionCtxKey struct{}

// ContextWithCLISession attaches the run's session holder to ctx so every
// follow-up step dispatched on it — verify, the criteria sweep, the review
// sweep — reaches the same holder the main run's executor call filled in.
func ContextWithCLISession(ctx context.Context, session *CLISession) context.Context {
	return context.WithValue(ctx, cliSessionCtxKey{}, session)
}

// CLISessionFromContext returns the holder ContextWithCLISession attached, or
// nil when the run never plumbed one.
func CLISessionFromContext(ctx context.Context) *CLISession {
	session, _ := ctx.Value(cliSessionCtxKey{}).(*CLISession)
	return session
}
