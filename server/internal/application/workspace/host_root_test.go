package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
)

// repoDir is where a re-anchor is expected to land: this host's repos directory.
func repoDir(root, name string) string {
	return filepath.Join(root, "repos", name)
}

// TestHostRootPath_ExistingPathUnchanged: a stored path that is really here is
// never rewritten, even when it sits nowhere near the workspace root. That is
// the self-hosted user who pointed a repository at their own checkout.
func TestHostRootPath_ExistingPathUnchanged(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "elsewhere", "acme-web")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	wsRoot := filepath.Join(t.TempDir(), "workspaces")

	got, reanchored := workspace.HostRootPath(repo, wsRoot, nil)
	if reanchored {
		t.Fatalf("existing path was re-anchored to %q", got)
	}
	if got != repo {
		t.Fatalf("got %q, want %q", got, repo)
	}
}

// TestHostRootPath_ForeignCloudPathReanchored is the production failure: the
// Mac read the pod's /data path and tried to mkdir it on a read-only root.
func TestHostRootPath_ForeignCloudPathReanchored(t *testing.T) {
	const stored = "/data/workspaces/foo"
	if _, err := os.Stat(stored); err == nil {
		t.Skipf("%s exists on this host; the foreign-path case cannot be simulated here", stored)
	}
	wsRoot := filepath.Join(t.TempDir(), "x", "workspaces")

	got, reanchored := workspace.HostRootPath(stored, wsRoot, nil)
	if !reanchored {
		t.Fatalf("foreign path %q was not re-anchored", stored)
	}
	if want := repoDir(wsRoot, "foo"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestHostRootPath_Symmetric: the same function run "on the pod" translates a
// Mac-written path back. Neither host owns the column, so a write from either
// side stays harmless to the other.
func TestHostRootPath_Symmetric(t *testing.T) {
	macRoot := filepath.Join(t.TempDir(), "local-runner", "data", "workspaces")
	podRoot := filepath.Join(t.TempDir(), "data", "workspaces")
	macStored := repoDir(macRoot, "acme-web")

	// On the pod, the Mac's path exists nowhere and is not under the pod root.
	onPod, reanchored := workspace.HostRootPath(macStored, podRoot, nil)
	if !reanchored {
		t.Fatalf("mac path %q was not re-anchored on the pod", macStored)
	}
	if want := repoDir(podRoot, "acme-web"); onPod != want {
		t.Fatalf("got %q, want %q", onPod, want)
	}

	// And back again.
	onMac, reanchored := workspace.HostRootPath(onPod, macRoot, nil)
	if !reanchored {
		t.Fatalf("pod path %q was not re-anchored on the mac", onPod)
	}
	if onMac != macStored {
		t.Fatalf("got %q, want %q", onMac, macStored)
	}
}

// TestHostRootPath_UnderAllowedRootUnchanged: a path that does not exist yet
// but lies under an operator-configured allowed root is this host's to create
// (the fresh-pod, clone-from-remote_url case), so it must survive untouched.
func TestHostRootPath_UnderAllowedRootUnchanged(t *testing.T) {
	allowed := filepath.Join(t.TempDir(), "srv", "code")
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	stored := filepath.Join(allowed, "acme-web")

	got, reanchored := workspace.HostRootPath(stored, wsRoot, []string{allowed})
	if reanchored {
		t.Fatalf("path under an allowed root was re-anchored to %q", got)
	}
	if got != stored {
		t.Fatalf("got %q, want %q", got, stored)
	}
}

// TestHostRootPath_UnderWorkspaceRootUnchanged covers the same for the
// workspace root, which is always allowed.
func TestHostRootPath_UnderWorkspaceRootUnchanged(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	stored := repoDir(wsRoot, "acme-web")

	got, reanchored := workspace.HostRootPath(stored, wsRoot, nil)
	if reanchored {
		t.Fatalf("path under the workspace root was re-anchored to %q", got)
	}
	if got != stored {
		t.Fatalf("got %q, want %q", got, stored)
	}
}

// TestHostRootPath_NoWorkspaceRoot: with nothing to re-anchor onto, the stored
// path is returned as-is and the caller still fails with the real path in the
// error rather than one this code invented.
func TestHostRootPath_NoWorkspaceRoot(t *testing.T) {
	const stored = "/data/workspaces/foo"
	if _, err := os.Stat(stored); err == nil {
		t.Skipf("%s exists on this host", stored)
	}
	got, reanchored := workspace.HostRootPath(stored, "  ", nil)
	if reanchored || got != stored {
		t.Fatalf("got (%q, %v), want (%q, false)", got, reanchored, stored)
	}
}

func TestHostRootPath_Empty(t *testing.T) {
	got, reanchored := workspace.HostRootPath("", "/tmp/workspaces", nil)
	if reanchored || got != "" {
		t.Fatalf("got (%q, %v), want (\"\", false)", got, reanchored)
	}
}

func TestUsableHostPath(t *testing.T) {
	dir := t.TempDir()
	wsRoot := filepath.Join(dir, "workspaces")
	allowed := filepath.Join(dir, "allowed")
	existing := filepath.Join(dir, "existing")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"existing outside every root", existing, true},
		{"missing under the workspace root", repoDir(wsRoot, "a"), true},
		{"missing under allowed root", filepath.Join(allowed, "a"), true},
		{"the workspace root itself", wsRoot, true},
		{"missing and foreign", filepath.Join(dir, "nope", "a"), false},
		{"empty", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspace.UsableHostPath(tc.path, wsRoot, []string{allowed}); got != tc.want {
				t.Fatalf("UsableHostPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
