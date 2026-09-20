package postgres

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// These cover the read-time translation without a database: localizeSessionPaths
// is what every scan of a sessions row runs through, and its whole job is to
// decide whether the absolute paths in the row belong to this host.

// The production shape: the pod wrote a task checkout under its PVC, and the
// Mac resuming that chat must get its own task-<id> directory instead of
// os.MkdirAll("/data/...").
func TestLocalizeSessionPaths_ForeignTaskWorkspaceReanchored(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "local-runner", "data", "workspaces")
	store := (&SessionStore{}).SetHostRoots(wsRoot, nil)

	taskID := uuid.New()
	sess := domain.Session{ID: uuid.New(), WorkspaceDir: "/data/workspaces/task-" + taskID.String()}
	store.localizeSessionPaths(&sess)

	// Exactly what board.TaskPRService.TaskWorkspacePath derives on this host,
	// so a chat resumed here and a board run on the same task share one tree.
	if want := filepath.Join(wsRoot, "task-"+taskID.String()); sess.WorkspaceDir != want {
		t.Fatalf("workspace dir = %q, want %q", sess.WorkspaceDir, want)
	}
}

// An unbound chat's own scratch directory is {root}/{session-id}
// (workspace.SessionDir), so the re-anchor lands on precisely the directory
// this host would have created for it.
func TestLocalizeSessionPaths_ForeignSessionDirReanchored(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	store := (&SessionStore{}).SetHostRoots(wsRoot, nil)

	id := uuid.New()
	sess := domain.Session{ID: id, WorkspaceDir: "/data/workspaces/" + id.String()}
	store.localizeSessionPaths(&sess)

	if want := filepath.Join(wsRoot, id.String()); sess.WorkspaceDir != want {
		t.Fatalf("workspace dir = %q, want %q", sess.WorkspaceDir, want)
	}
}

// project_root gets the same treatment as workspace_dir: it is handed to the
// indexer and to the code tools as the tree they are scoped to.
func TestLocalizeSessionPaths_ForeignProjectRootReanchored(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	store := (&SessionStore{}).SetHostRoots(wsRoot, nil)

	sess := domain.Session{
		ID:           uuid.New(),
		WorkspaceDir: "/data/workspaces/repos/acme-web",
		ProjectRoot:  "/data/workspaces/repos/acme-web",
	}
	store.localizeSessionPaths(&sess)

	want := filepath.Join(wsRoot, "repos", "acme-web")
	if sess.WorkspaceDir != want {
		t.Fatalf("workspace dir = %q, want %q", sess.WorkspaceDir, want)
	}
	if sess.ProjectRoot != want {
		t.Fatalf("project root = %q, want %q", sess.ProjectRoot, want)
	}
}

// A live local workspace must never be traded for an empty new one: a path that
// really is here comes back untouched even though it lies nowhere near the
// workspace root.
func TestLocalizeSessionPaths_ExistingLocalPathUnchanged(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	elsewhere := t.TempDir()
	store := (&SessionStore{}).SetHostRoots(wsRoot, nil)

	sess := domain.Session{ID: uuid.New(), WorkspaceDir: elsewhere, ProjectRoot: elsewhere}
	store.localizeSessionPaths(&sess)

	if sess.WorkspaceDir != elsewhere {
		t.Fatalf("workspace dir = %q, want %q", sess.WorkspaceDir, elsewhere)
	}
	if sess.ProjectRoot != elsewhere {
		t.Fatalf("project root = %q, want %q", sess.ProjectRoot, elsewhere)
	}
}

// A path under this host's workspace root that has not been created yet is this
// host's to create, not a foreign one — the state a fresh pod is in before the
// first turn of a restored chat runs.
func TestLocalizeSessionPaths_UnderWorkspaceRootUnchanged(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	stored := filepath.Join(wsRoot, "task-"+uuid.NewString())
	store := (&SessionStore{}).SetHostRoots(wsRoot, nil)

	sess := domain.Session{ID: uuid.New(), WorkspaceDir: stored}
	store.localizeSessionPaths(&sess)

	if sess.WorkspaceDir != stored {
		t.Fatalf("workspace dir = %q, want it unchanged", sess.WorkspaceDir)
	}
}

// A store that was never told about a host (tests, any caller that does not
// touch working copies) must behave exactly as it did before this existed.
func TestLocalizeSessionPaths_NoHostRootsIsIdentity(t *testing.T) {
	store := &SessionStore{}
	sess := domain.Session{
		ID:           uuid.New(),
		WorkspaceDir: "/data/workspaces/x",
		ProjectRoot:  "/data/workspaces/x/sub",
	}
	store.localizeSessionPaths(&sess)
	if sess.WorkspaceDir != "/data/workspaces/x" || sess.ProjectRoot != "/data/workspaces/x/sub" {
		t.Fatalf("paths = %q / %q, want them unchanged", sess.WorkspaceDir, sess.ProjectRoot)
	}
}

// The translation has to be symmetric, because neither host owns the column:
// whatever the Mac writes must be readable on the pod by the same rule, and
// coming back must restore the Mac's own path.
func TestLocalizeSessionPaths_Symmetric(t *testing.T) {
	macRoot := filepath.Join(t.TempDir(), "local-runner", "data", "workspaces")
	const podRoot = "/data/workspaces"
	id := uuid.New()

	macStored := filepath.Join(macRoot, "task-"+id.String())

	// On the pod: the Mac's path is nowhere near /data, so it re-anchors.
	onPod := domain.Session{ID: id, WorkspaceDir: macStored}
	(&SessionStore{}).SetHostRoots(podRoot, nil).localizeSessionPaths(&onPod)
	if want := filepath.Join(podRoot, "task-"+id.String()); onPod.WorkspaceDir != want {
		t.Fatalf("on pod = %q, want %q", onPod.WorkspaceDir, want)
	}

	// Back on the Mac: the pod's path re-anchors to exactly where it started.
	onMac := domain.Session{ID: id, WorkspaceDir: onPod.WorkspaceDir}
	(&SessionStore{}).SetHostRoots(macRoot, nil).localizeSessionPaths(&onMac)
	if onMac.WorkspaceDir != macStored {
		t.Fatalf("back on mac = %q, want %q", onMac.WorkspaceDir, macStored)
	}
}
