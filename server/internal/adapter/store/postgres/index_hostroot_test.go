package postgres

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// workspace_indexes.root_path is re-anchored rather than invalidated — see the
// reasoning on localizeIndexRootPath. These pin the behaviour that decision
// produces.

// A project index built on the pod points at the pod's clone; read here it must
// name this host's clone of the same repository, because that is the tree the
// skeleton walk will actually read.
func TestLocalizeIndexRootPath_ForeignProjectRootReanchored(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	store := (&IndexStore{}).SetHostRoots(wsRoot, nil)

	idx := domain.WorkspaceIndex{ID: uuid.New(), RootPath: "/data/workspaces/repos/acme-web"}
	store.localizeIndexRootPath(tenantCtx(hostTenantA), &idx)

	if want := filepath.Join(tenantRoot(wsRoot, hostTenantA), "repos", "acme-web"); idx.RootPath != want {
		t.Fatalf("root path = %q, want %q", idx.RootPath, want)
	}
}

// A branch index is anchored on the task checkout, so it re-anchors onto the
// same task-<id> directory the runner and the task chat use here.
func TestLocalizeIndexRootPath_ForeignBranchRootReanchored(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	store := (&IndexStore{}).SetHostRoots(wsRoot, nil)

	taskID := uuid.New()
	idx := domain.WorkspaceIndex{
		ID:       uuid.New(),
		Branch:   "feature/t-12",
		RootPath: "/data/workspaces/task-" + taskID.String(),
	}
	store.localizeIndexRootPath(tenantCtx(hostTenantA), &idx)

	if want := filepath.Join(tenantRoot(wsRoot, hostTenantA), "task-"+taskID.String()); idx.RootPath != want {
		t.Fatalf("root path = %q, want %q", idx.RootPath, want)
	}
}

// An index this host built is left exactly as it is; nothing is invalidated and
// nothing is moved.
func TestLocalizeIndexRootPath_LocalPathUnchanged(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	local := filepath.Join(tenantRoot(wsRoot, hostTenantA), "repos", "acme-web")
	store := (&IndexStore{}).SetHostRoots(wsRoot, nil)

	idx := domain.WorkspaceIndex{ID: uuid.New(), RootPath: local}
	store.localizeIndexRootPath(tenantCtx(hostTenantA), &idx)

	if idx.RootPath != local {
		t.Fatalf("root path = %q, want %q", idx.RootPath, local)
	}
}

func TestLocalizeIndexRootPath_NoHostRootsIsIdentity(t *testing.T) {
	store := &IndexStore{}
	idx := domain.WorkspaceIndex{ID: uuid.New(), RootPath: "/data/workspaces/repos/acme-web"}
	store.localizeIndexRootPath(tenantCtx(hostTenantA), &idx)
	if idx.RootPath != "/data/workspaces/repos/acme-web" {
		t.Fatalf("root path = %q, want it unchanged", idx.RootPath)
	}
}
