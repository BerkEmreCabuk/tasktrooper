//go:build windows

package localpreview

import (
	"os/exec"
	"strconv"
	"syscall"
)

// shellCommand builds the command that runs the workspace's dev/start
// command. There is no POSIX process group on Windows, so the isolation
// terminateProcessGroup/killProcessGroup need comes from
// CREATE_NEW_PROCESS_GROUP instead: it gives the shell its own group id
// (equal to its own pid, the same convention Setpgid gives it on unix) so a
// taskkill /T against that pid reaches the whole tree without also reaching
// this server's own process.
//
// cmd.exe rather than sh: a workspace's dev/start command is written for
// whatever shell actually runs it, and on Windows that is cmd's own syntax,
// not POSIX — a repository targeting this platform would have configured its
// run command accordingly.
func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("cmd", "/C", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	return cmd
}

// terminateProcessGroup asks the tree to shut down on its own. taskkill
// without /F sends WM_CLOSE to a window-owning process or a console
// close event to a console one — closer to SIGTERM than an outright kill,
// though a process that ignores both keeps running until killProcessGroup's
// /F escalates. Errors are not reported here, the same as the unix side:
// a tree that already exited makes this a harmless no-op.
func terminateProcessGroup(pid int) {
	_ = exec.Command("taskkill", "/T", "/PID", strconv.Itoa(pid)).Run()
}

// killProcessGroup is terminateProcessGroup's unconditional counterpart, for
// a tree that did not exit within stopGrace. /F forces it; running it again
// against an already-gone tree (taskkill's own exit code aside) is exactly
// as harmless as the unix side's second SIGKILL.
func killProcessGroup(pid int) {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}
