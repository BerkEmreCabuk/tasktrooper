package domain

import (
	"errors"
	"os/exec"
	"syscall"
)

// ExitSignal reports the signal that killed a child process, if it was killed
// by one. The agent CLIs are only ever stopped by this codebase through their
// own context; a signaled exit reaching here came from outside — the OS, the
// supervisor's process-tree kill, App Nap — and would otherwise surface as a
// bare "exit status 143".
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
	// A child that traps the signal to shut down cleanly exits on its own with
	// the shell convention 128+n. The upper bound is the standard signal range
	// (1-31), not syscall.SIGUSR2: that constant is platform-dependent (12 on
	// Linux, 31 on Darwin), so using it excluded 128+15=143 (SIGTERM) on Linux
	// while passing on macOS.
	if code := status.ExitStatus(); code > 128 && code <= 128+31 {
		return syscall.Signal(code - 128), true
	}
	return 0, false
}
