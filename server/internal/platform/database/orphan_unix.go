//go:build unix

package database

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// stopWhenOrphaned leaves a detached watcher behind that shuts the cluster down
// once this process is gone. pg_ctl daemonises postgres, so a test binary that
// is SIGKILLed — a caller's command timeout, `go test -timeout` — never reaches
// Stop, and the cluster it started lives on for days holding its port and
// memory. Setsid keeps the watcher out of the process group such a kill takes
// down. After a clean Stop postgres has removed postmaster.pid and the watcher
// has nothing to signal.
func stopWhenOrphaned(dataDir string) {
	const script = `while kill -0 "$1" 2>/dev/null; do sleep 1; done
pid=$(head -n 1 "$2/postmaster.pid" 2>/dev/null) && kill -INT "$pid" 2>/dev/null`
	cmd := exec.Command("/bin/sh", "-c", script, "sh", strconv.Itoa(os.Getpid()), dataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return
	}
	_ = cmd.Process.Release()
}
