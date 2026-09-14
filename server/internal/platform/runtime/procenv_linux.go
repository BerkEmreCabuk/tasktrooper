//go:build linux

package runtime

import "syscall"

// prSetDumpable is PR_SET_DUMPABLE from <linux/prctl.h>. Hard-coded rather than
// imported from golang.org/x/sys/unix so this stays a stdlib-only file: x/sys is
// an indirect dependency here and promoting it to a direct one would rewrite
// go.mod for two integers.
const prSetDumpable = 4

// denyProcEnvironReads makes this process undumpable, which is the only
// unprivileged way to stop a same-UID child from reading the parent's secrets
// out of /proc.
//
// Why it is needed at all: scrubbing what a child inherits
// (internal/platform/childenv) and unsetting the variables afterwards
// (scrubProcessSecrets) both operate on the environment as the runtime sees it.
// /proc/<pid>/environ does not read that. It reads the [env_start, env_end)
// region of the process's initial stack, written once by execve and never
// updated by setenv/unsetenv. So a prompt-injected agent could skip every
// scrub with
//
//	cat /proc/$PPID/environ
//
// and get DATABASE_URL, INTERNAL_AUTH_KEY and MCP_SECRETS_KEY back verbatim.
// That cannot be pattern-blocked: $PPID is dynamic, the path has unbounded
// spellings, and the read need not even come from a shell.
//
// Access to /proc/<pid>/environ is gated by a PTRACE_MODE_READ_FSCREDS check,
// and that check fails for a caller without CAP_SYS_PTRACE when the target is
// not dumpable. Clearing the dumpable flag therefore takes the file away from
// every other process on the pod, children included. The flag lives on the mm,
// so one call covers every goroutine and every OS thread the Go runtime
// creates.
//
// It also takes the file away from this process itself: the kernel reassigns a
// non-dumpable process's /proc/<pid> entries to root (task_dump_owner), so even
// a self read of /proc/self/environ fails at open() when running unprivileged.
// The ptrace short-circuit for the own thread group never comes into play
// because the ownership check runs first. Nothing here needs that file —
// self-inspection goes through os.Environ(), which reads runtime memory, not
// procfs — so the loss is accepted and pinned by the tests.
//
// The other cost is core dumps: an undumpable process produces none. For a
// process whose address space holds the tenant's database password and the
// gateway HMAC key, a core dump on disk is a liability rather than a debugging
// asset.
//
// A child that execve's a normal binary becomes dumpable again, which is
// correct — the flag protects the memory of the process that holds the secrets,
// and the child holds none.
func denyProcEnvironReads() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetDumpable, 0, 0); errno != 0 {
		return errno
	}
	return nil
}
