//go:build linux

package runtime

import "syscall"

// prSetDumpable is PR_SET_DUMPABLE from <linux/prctl.h>, hard-coded so this
// stays a stdlib-only file (x/sys is only an indirect dependency).
const prSetDumpable = 4

// denyProcEnvironReads makes this process undumpable, the only unprivileged way
// to stop a same-UID child from reading the parent's secrets out of procfs.
// /proc/<pid>/environ reads the exec-time stack region, never updated by
// setenv/unsetenv, so no amount of env scrubbing hides DATABASE_URL from a
// prompt-injected `cat /proc/$PPID/environ`. Its PTRACE_MODE_READ_FSCREDS gate
// fails for a non-dumpable target, so clearing the flag takes the file away
// from every other process on the pod too.
//
// Costs, both accepted: the kernel reassigns a non-dumpable process's /proc/<pid>
// entries to root, so even this process cannot self-read that file (unneeded —
// os.Environ() reads runtime memory), and an undumpable process produces no
// core dumps, which for a process holding the database password is a benefit
// rather than a loss. A child that execve's a normal binary becomes dumpable
// again, which is correct: the child holds none of the secrets.
func denyProcEnvironReads() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetDumpable, 0, 0); errno != 0 {
		return errno
	}
	return nil
}
