//go:build unix

package localpreview

import (
	"os/exec"
	"syscall"
)

// shellCommand builds the command that runs the workspace's dev/start
// command, in its own process group so terminateProcessGroup/killProcessGroup
// can reach every child a shell-wrapped command spawns (webpack behind
// `npm run dev`, say), not just the shell.
func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// terminateProcessGroup asks the group to shut down on its own. pid is the
// shell's own pid, which is also the process group id Setpgid gave it; the
// negative form is the POSIX convention for "the whole group", not "the
// process". Errors are not reported here — SIGTERM just went to
// a group Stop is about to escalate on anyway.
func terminateProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}

// killProcessGroup is terminateProcessGroup's unconditional counterpart, for
// a group that did not exit within stopGrace.
func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
