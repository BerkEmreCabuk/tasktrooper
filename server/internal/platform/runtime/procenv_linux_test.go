//go:build linux

package runtime

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// These tests exist because the obvious fix does not work and "looks like it
// works" is exactly how this class of hole survives a review. os.Unsetenv only
// edits the Go runtime's view; /proc/<pid>/environ is the exec-time stack
// region, so a child can still `cat /proc/$PPID/environ` unless the parent
// makes itself undumpable. The probe runs as a subprocess so the secret is
// present at execve — a t.Setenv value was never in the initial block at all.

const (
	probeSwitch    = "TASKTROOPER_PROC_ENVIRON_PROBE"
	probeSecretVar = "TASKTROOPER_PROBE_SECRET"
	probeSecret    = "probe-secret-value-8f21"
)

// TestProcEnvironProbeHelper is not a test; it is the subprocess body.
func TestProcEnvironProbeHelper(t *testing.T) {
	if os.Getenv(probeSwitch) == "" {
		t.Skip("subprocess body for TestUnsetenvDoesNotClearProcSelfEnviron")
	}

	report := func(key, value string) { os.Stdout.WriteString(key + "=" + value + "\n") }

	report("secret_in_environ_at_exec", contains(readOwnEnviron(), probeSecret))
	if err := os.Unsetenv(probeSecretVar); err != nil {
		report("unsetenv_error", err.Error())
	}
	report("secret_in_go_env_after_unset", boolStr(os.Getenv(probeSecretVar) != ""))
	report("secret_in_proc_environ_after_unset", contains(readOwnEnviron(), probeSecret))
	report("child_read_before_prctl", childReadOfParentEnviron())
	if err := denyProcEnvironReads(); err != nil {
		report("prctl_error", err.Error())
	}
	report("child_read_after_prctl", childReadOfParentEnviron())
	report("self_read_after_prctl", selfEnvironState())
}

// selfEnvironState distinguishes "the file is gone for us too" from "readable
// but secret absent" — contains() collapses both, and the difference is what
// the undumpable test pins.
func selfEnvironState() string {
	data, err := os.ReadFile("/proc/self/environ")
	if err != nil {
		return "unreadable"
	}
	if strings.Contains(string(data), probeSecret) {
		return "secret-present"
	}
	return "secret-absent"
}

func readOwnEnviron() string {
	data, err := os.ReadFile("/proc/self/environ")
	if err != nil {
		return "<unreadable: " + err.Error() + ">"
	}
	return string(bytes.ReplaceAll(data, []byte{0}, []byte{'\n'}))
}

// childReadOfParentEnviron is the attack, verbatim.
func childReadOfParentEnviron() string {
	cmd := exec.Command("sh", "-c", "cat /proc/$PPID/environ")
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "denied"
	}
	if strings.Contains(strings.ReplaceAll(string(out), "\x00", "\n"), probeSecret) {
		return "leaked"
	}
	return "empty"
}

func contains(haystack, needle string) string { return boolStr(strings.Contains(haystack, needle)) }

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestUnsetenvDoesNotClearProcSelfEnviron pins the reason denyProcEnvironReads
// exists: if Go or the kernel ever made unsetting rewrite the block, this fails
// and the PR_SET_DUMPABLE call could go.
func TestUnsetenvDoesNotClearProcSelfEnviron(t *testing.T) {
	results := runProbe(t)

	if results["secret_in_environ_at_exec"] != "true" {
		t.Skipf("/proc/self/environ did not reflect the exec-time environment here (%q); "+
			"this sandbox does not model production procfs",
			results["secret_in_environ_at_exec"])
	}
	if got := results["secret_in_go_env_after_unset"]; got != "false" {
		t.Fatalf("os.Unsetenv did not remove the variable from the Go environment: %q", got)
	}
	if got := results["secret_in_proc_environ_after_unset"]; got != "true" {
		t.Errorf("expected /proc/self/environ to still carry the secret after os.Unsetenv, got %q. "+
			"If this is now false, unsetting alone closes the bypass and the PR_SET_DUMPABLE "+
			"call in procenv_linux.go can go — update the comments there before removing it.", got)
	}
}

// TestUndumpableBlocksChildProcEnvironReads asserts the bypass is actually
// shut, and pins the cost: the undumpable process loses its own /proc self
// inspection along with the children's cross-process read.
func TestUndumpableBlocksChildProcEnvironReads(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: CAP_SYS_PTRACE bypasses the dumpable check, so this proves nothing here")
	}

	results := runProbe(t)

	if results["prctl_error"] != "" {
		t.Fatalf("PR_SET_DUMPABLE failed: %s", results["prctl_error"])
	}
	if results["child_read_before_prctl"] != "leaked" {
		t.Skipf("a child could not read the parent's /proc environ even before the fix (%q); "+
			"this sandbox already blocks it, so it cannot demonstrate the fix",
			results["child_read_before_prctl"])
	}
	if got := results["child_read_after_prctl"]; got == "leaked" {
		t.Errorf("child still read the parent's secrets out of /proc after PR_SET_DUMPABLE=0")
	}
	switch got := results["self_read_after_prctl"]; got {
	case "unreadable":
		// Expected: a non-dumpable process's /proc/<pid> entries belong to root.
	case "secret-present":
		// Kernel/mount config that still lets the owner through — harmless, since
		// the cross-process read is the one that matters.
	default:
		t.Errorf("unexpected /proc/self/environ state after PR_SET_DUMPABLE=0: %q "+
			"(readable but without the exec-time secret should be impossible)", got)
	}
}

func runProbe(t *testing.T) map[string]string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestProcEnvironProbeHelper", "-test.v")
	cmd.Env = append(os.Environ(),
		probeSwitch+"=1",
		probeSecretVar+"="+probeSecret,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe subprocess failed: %v\n%s", err, out)
	}
	results := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(key, "secret_"),
			strings.HasPrefix(key, "child_"),
			strings.HasPrefix(key, "self_"),
			strings.HasSuffix(key, "_error"):
			results[key] = value
		}
	}
	return results
}
