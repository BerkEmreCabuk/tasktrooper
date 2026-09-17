package domain

import (
	"errors"
	"os/exec"
	"syscall"
)

// ExitSignal reports the signal that killed a child process, if it was killed
// by one. The agent CLIs are only ever stopped by this codebase through their
// own context (the run timeout or the caller's cancellation), both of which
// are already attributed elsewhere; a signaled exit reaching here came from
// outside — the OS, the desktop supervisor's own process-tree kill, App Nap,
// or a stray `kill` — and would otherwise surface as a bare, unexplained
// "exit status 143".
func ExitSignal(err error) (syscall.Signal, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return 0, false
	}
	return status.Signal(), true
}
