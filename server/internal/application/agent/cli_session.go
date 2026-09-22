package agent

import (
	"context"
	"sync"
)

// Session id a run's follow-up steps share with the main run, so they resume the CLI session instead of rebuilding it.
type CLISession struct {
	mu sync.Mutex
	id string
}

// Nil-safe so a run without a holder reads "" and takes the fresh-run path.
func (s *CLISession) ID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// Nil-safe for the same reason ID is.
func (s *CLISession) Set(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = id
}

type cliSessionCtxKey struct{}

// Attaches the run's holder so every follow-up step reaches the one the main run filled in.
func ContextWithCLISession(ctx context.Context, session *CLISession) context.Context {
	return context.WithValue(ctx, cliSessionCtxKey{}, session)
}

// Returns the attached holder, or nil when the run never plumbed one.
func CLISessionFromContext(ctx context.Context) *CLISession {
	session, _ := ctx.Value(cliSessionCtxKey{}).(*CLISession)
	return session
}
