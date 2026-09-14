package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

var (
	tenantA = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantB = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

func ctxFor(id uuid.UUID) context.Context {
	return tenant.With(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleMember})
}

// repoDir is where a re-anchor is expected to land: inside the calling tenant's
// own repos directory, never at the shared workspace root.
func repoDir(root string, id uuid.UUID, name string) string {
	return filepath.Join(root, "tenants", id.String(), "repos", name)
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

	got, reanchored := workspace.HostRootPath(ctxFor(tenantA), repo, wsRoot, nil)
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

	got, reanchored := workspace.HostRootPath(ctxFor(tenantA), stored, wsRoot, nil)
	if !reanchored {
		t.Fatalf("foreign path %q was not re-anchored", stored)
	}
	if want := repoDir(wsRoot, tenantA, "foo"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestHostRootPath_TwoTenantsDoNotShareAReanchor is the isolation property this
// translation used to break: the final segment of a foreign path is a
// REPOSITORY NAME, and "api" is the same name for everybody. Re-anchoring onto
// the bare workspace root landed two customers' rows on one directory, which is
// the collision the whole layout exists to prevent.
func TestHostRootPath_TwoTenantsDoNotShareAReanchor(t *testing.T) {
	const stored = "/data/workspaces/repos/api"
	if _, err := os.Stat(stored); err == nil {
		t.Skipf("%s exists on this host", stored)
	}
	wsRoot := filepath.Join(t.TempDir(), "workspaces")

	forA, okA := workspace.HostRootPath(ctxFor(tenantA), stored, wsRoot, nil)
	forB, okB := workspace.HostRootPath(ctxFor(tenantB), stored, wsRoot, nil)
	if !okA || !okB {
		t.Fatalf("foreign path was not re-anchored: A=%v B=%v", okA, okB)
	}
	if forA == forB {
		t.Fatalf("two tenants re-anchored onto one directory: %q", forA)
	}
	if want := repoDir(wsRoot, tenantA, "api"); forA != want {
		t.Fatalf("tenant A: got %q, want %q", forA, want)
	}
	if want := repoDir(wsRoot, tenantB, "api"); forB != want {
		t.Fatalf("tenant B: got %q, want %q", forB, want)
	}
}

// TestHostRootPath_NoTenantIsNeverReanchored: with no identity there is no
// destination that could be right, so nothing is invented and the caller keeps
// the real path in its error.
func TestHostRootPath_NoTenantIsNeverReanchored(t *testing.T) {
	const stored = "/data/workspaces/foo"
	if _, err := os.Stat(stored); err == nil {
		t.Skipf("%s exists on this host", stored)
	}
	wsRoot := filepath.Join(t.TempDir(), "workspaces")

	got, reanchored := workspace.HostRootPath(context.Background(), stored, wsRoot, nil)
	if reanchored || got != stored {
		t.Fatalf("got (%q, %v), want (%q, false)", got, reanchored, stored)
	}
}

// TestHostRootPath_Symmetric: the same function run "on the pod" translates a
// Mac-written path back. Neither host owns the column, so a write from either
// side stays harmless to the other.
func TestHostRootPath_Symmetric(t *testing.T) {
	macRoot := filepath.Join(t.TempDir(), "local-runner", "data", "workspaces")
	podRoot := filepath.Join(t.TempDir(), "data", "workspaces")
	macStored := repoDir(macRoot, tenantA, "acme-web")
	ctx := ctxFor(tenantA)

	// On the pod, the Mac's path exists nowhere and is not under the pod root.
	onPod, reanchored := workspace.HostRootPath(ctx, macStored, podRoot, nil)
	if !reanchored {
		t.Fatalf("mac path %q was not re-anchored on the pod", macStored)
	}
	if want := repoDir(podRoot, tenantA, "acme-web"); onPod != want {
		t.Fatalf("got %q, want %q", onPod, want)
	}

	// And back again.
	onMac, reanchored := workspace.HostRootPath(ctx, onPod, macRoot, nil)
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

	got, reanchored := workspace.HostRootPath(ctxFor(tenantA), stored, wsRoot, []string{allowed})
	if reanchored {
		t.Fatalf("path under an allowed root was re-anchored to %q", got)
	}
	if got != stored {
		t.Fatalf("got %q, want %q", got, stored)
	}
}

// TestHostRootPath_UnderOwnTenantRootUnchanged covers the same for the tenant's
// own subtree, which is always allowed.
func TestHostRootPath_UnderOwnTenantRootUnchanged(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	stored := repoDir(wsRoot, tenantA, "acme-web")

	got, reanchored := workspace.HostRootPath(ctxFor(tenantA), stored, wsRoot, nil)
	if reanchored {
		t.Fatalf("path under the tenant's own root was re-anchored to %q", got)
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
	got, reanchored := workspace.HostRootPath(ctxFor(tenantA), stored, "  ", nil)
	if reanchored || got != stored {
		t.Fatalf("got (%q, %v), want (%q, false)", got, reanchored, stored)
	}
}

func TestHostRootPath_Empty(t *testing.T) {
	got, reanchored := workspace.HostRootPath(ctxFor(tenantA), "", "/tmp/workspaces", nil)
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
	ctx := ctxFor(tenantA)

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"existing outside every root", existing, true},
		{"missing under this tenant's root", repoDir(wsRoot, tenantA, "a"), true},
		{"missing under allowed root", filepath.Join(allowed, "a"), true},
		{"this tenant's root itself", filepath.Join(wsRoot, "tenants", tenantA.String()), true},
		// The shared root and another tenant's subtree are both foreign: a
		// destination this host "may still create" must be one this TENANT may
		// still create, or the not-yet-cloned case hands one customer a path
		// inside another's.
		{"missing at the shared workspace root", filepath.Join(wsRoot, "repos", "a"), false},
		{"missing under another tenant's root", repoDir(wsRoot, tenantB, "a"), false},
		{"missing and foreign", filepath.Join(dir, "nope", "a"), false},
		{"empty", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspace.UsableHostPath(ctx, tc.path, wsRoot, []string{allowed}); got != tc.want {
				t.Fatalf("UsableHostPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
