package main

import (
	"io"
)

// stdinClosed returns a channel that closes when r reaches EOF, or nil (which
// blocks forever) when the watch is off.
//
// It is how the desktop app stops the server on every platform. Windows has no
// SIGTERM — killing a process there is TerminateProcess, which skips the drain
// and orphans the embedded Postgres — and a pipe also closes when the app that
// holds it dies, so the server never outlives its supervisor.
func stdinClosed(enabled bool, r io.Reader) <-chan struct{} {
	if !enabled {
		return nil
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	return done
}
