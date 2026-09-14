package postgres

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

var (
	hostTenantA = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	hostTenantB = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// tenantCtx is the identity every read-time path translation now needs: the
// re-anchor destination is inside the calling tenant's subtree, so a scan with
// no tenant re-anchors nothing.
func tenantCtx(id uuid.UUID) context.Context {
	return tenant.With(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleMember})
}

// tenantRoot mirrors workspace.TenantRoot for the expectations below.
func tenantRoot(wsRoot string, id uuid.UUID) string {
	return filepath.Join(wsRoot, "tenants", id.String())
}

// These cover the read-time translation without a database: localizeRootPath is
// what every scan of a repositories row runs through, and its whole job is to
// decide whether a stored absolute path belongs to this host.

func TestLocalizeRootPath_ForeignPathReanchored(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "x", "workspaces")
	store := (&RepositoryStore{}).SetHostRoots(wsRoot, nil)

	repo := domain.Repository{Name: "acme-web", RootPath: "/data/workspaces/acme-web"}
	store.localizeRootPath(tenantCtx(hostTenantA), &repo)

	if want := filepath.Join(tenantRoot(wsRoot, hostTenantA), "repos", "acme-web"); repo.RootPath != want {
		t.Fatalf("root path = %q, want %q", repo.RootPath, want)
	}
}

func TestLocalizeRootPath_LocalPathUnchanged(t *testing.T) {
	dir := t.TempDir()
	wsRoot := filepath.Join(dir, "workspaces")
	local := filepath.Join(tenantRoot(wsRoot, hostTenantA), "repos", "acme-web")
	store := (&RepositoryStore{}).SetHostRoots(wsRoot, nil)

	repo := domain.Repository{Name: "acme-web", RootPath: local}
	store.localizeRootPath(tenantCtx(hostTenantA), &repo)

	if repo.RootPath != local {
		t.Fatalf("root path = %q, want %q", repo.RootPath, local)
	}
}

// A store that was never told about a host (tests, any caller that does not
// touch working copies) must behave exactly as it did before this existed.
func TestLocalizeRootPath_NoHostRootsIsIdentity(t *testing.T) {
	store := &RepositoryStore{}
	repo := domain.Repository{Name: "acme-web", RootPath: "/data/workspaces/acme-web"}
	store.localizeRootPath(tenantCtx(hostTenantA), &repo)
	if repo.RootPath != "/data/workspaces/acme-web" {
		t.Fatalf("root path = %q, want it unchanged", repo.RootPath)
	}
}

func TestRepoDirName(t *testing.T) {
	cases := map[string]string{
		"/data/workspaces/repos/acme-web": "acme-web",
		"/data/workspaces/repos/":         "repos",
		"acme-web":                        "acme-web",
		"  /data/x  ":                     "x",
		"/":                               "",
		"":                                "",
		".":                               "",
	}
	for in, want := range cases {
		if got := repoDirName(in); got != want {
			t.Fatalf("repoDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The isolation property this translation used to break. A foreign path's final
// segment is a repository NAME, and two customers with a repository called
// "api" both re-anchored onto one directory under the shared workspace root —
// which is how one tenant's index pass came to walk another's checkout.
func TestLocalizeRootPath_TwoTenantsDoNotShareADirectory(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "x", "workspaces")
	store := (&RepositoryStore{}).SetHostRoots(wsRoot, nil)

	forA := domain.Repository{Name: "api", RootPath: "/data/workspaces/repos/api"}
	forB := domain.Repository{Name: "api", RootPath: "/data/workspaces/repos/api"}
	store.localizeRootPath(tenantCtx(hostTenantA), &forA)
	store.localizeRootPath(tenantCtx(hostTenantB), &forB)

	if forA.RootPath == forB.RootPath {
		t.Fatalf("two tenants localized onto one working copy: %q", forA.RootPath)
	}
	if want := filepath.Join(tenantRoot(wsRoot, hostTenantA), "repos", "api"); forA.RootPath != want {
		t.Fatalf("tenant A root path = %q, want %q", forA.RootPath, want)
	}
}

// With no identity there is no destination that could be right, so the stored
// path survives and the caller still fails with the real path in its error.
func TestLocalizeRootPath_NoTenantIsIdentity(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "x", "workspaces")
	store := (&RepositoryStore{}).SetHostRoots(wsRoot, nil)

	repo := domain.Repository{Name: "api", RootPath: "/data/workspaces/repos/api"}
	store.localizeRootPath(context.Background(), &repo)

	if repo.RootPath != "/data/workspaces/repos/api" {
		t.Fatalf("root path = %q, want it unchanged", repo.RootPath)
	}
}
