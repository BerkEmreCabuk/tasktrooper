//go:build linux

package runtime

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// This file exists because the obvious fix for the /proc bypass does not work,
// and "it looks like it works" is exactly how this class of hole survives a
// review.
//
// os.Unsetenv removes a variable from the Go runtime's view of the environment.
// /proc/<pid>/environ is not served from that view: it is the
// [env_start, env_end) region of the process's initial stack, written once by
// execve. Nothing short of CAP_SYS_RESOURCE (PR_SET_MM_ENV_START) rewrites it.
// So after the scrub a child can still run `cat /proc/$PPID/environ` and read
// every secret the pod was started with — unless the parent has made itself
// undumpable, which is what denyProcEnvironReads does.
//
// The probe below runs as a subprocess of the test binary so the secret is
// present at execve, which is the only way to observe the real behaviour;
// t.Setenv writes a value that was never in the initial block at all.

const (
	probeSwitch    = "TASKTROOPER_PROC_ENVIRON_PROBE"
	probeSecretVar = "TASKTROOPER_PROBE_SECRET"
	probeSecret    = "probe-secret-value-8f21"
)

// TestProcEnvironProbeHelper is not a test. It is the body of the subprocess
// the real test launches, selected with -test.run.
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

// selfEnvironState distinguishes "the file is gone for us too" from "the file
// is readable but the secret is not in it" — contains() collapses both to
// "false", and the difference is exactly what the undumpable test pins.
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

// childReadOfParentEnviron is the attack, verbatim: a child process reading its
// parent's environment block out of procfs.
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
// exists. If Go or the kernel ever made os.Unsetenv rewrite the environ block,
// this test would fail and the prctl call could be deleted — which is the only
// circumstance under which deleting it would be correct.
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

// TestUndumpableBlocksChildProcEnvironReads is the assertion that the bypass is
// actually shut: the same `cat /proc/$PPID/environ` that worked a moment
// earlier must fail once the parent is undumpable. It also pins the cost:
// clearing the dumpable flag hands the process's own /proc/<pid> files to root,
// so the process loses procfs self-inspection along with everyone else.
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
		// Expected. A non-dumpable process's /proc/<pid> entries belong to
		// root (task_dump_owner), so the unprivileged self read dies at
		// open(). os.Environ() still works; nothing in the codebase reads
		// /proc/self/environ.
	case "secret-present":
		// A kernel or mount configuration that still lets the owner through.
		// Harmless — the cross-process leak above is the one that matters.
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
