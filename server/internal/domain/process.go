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
	if !ok {
		return 0, false
	}
	if status.Signaled() {
		return status.Signal(), true
	}
	// A child that traps the signal to shut down cleanly — the Node CLIs all
	// do — is never "signaled" to the kernel: it exits on its own with the
	// shell convention 128+n, which is how SIGTERM arrives here as 143.
	if code := status.ExitStatus(); code > 128 && code <= 128+int(syscall.SIGUSR2) {
		return syscall.Signal(code - 128), true
	}
	return 0, false
}
