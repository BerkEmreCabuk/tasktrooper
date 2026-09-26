//go:build windows

package localpreview

import (
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("cmd", "/C", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	return cmd
}

func terminateProcessGroup(pid int) {
	_ = exec.Command("taskkill", "/T", "/PID", strconv.Itoa(pid)).Run()
}

func killProcessGroup(pid int) {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}

// processCommand is not read on Windows, so no stale server is ever stopped
// there.
func processCommand(int) string { return "" }

func stopStale(int, time.Duration) {}
