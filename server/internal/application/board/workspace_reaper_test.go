package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

type fakeTaskLister struct {
	tasks []domain.BoardTask
	err   error
}

func (f fakeTaskLister) ListAll(context.Context) ([]domain.BoardTask, error) {
	return f.tasks, f.err
}

type fakeActive map[uuid.UUID]bool

func (f fakeActive) HasLiveRunForTask(_ context.Context, id uuid.UUID, _ time.Duration) (bool, error) {
	return f[id], nil
}

// mkTaskDir creates a task workspace with a file inside it, so a partial delete
// shows up as an existing directory rather than as success.
func mkTaskDir(t *testing.T, root string, id uuid.UUID, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(root, "task-"+id.String())
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestWorkspaceReaperKeepsAndReaps(t *testing.T) {
	shared := t.TempDir()
	root := tenantWorkspace(t, shared, reaperTenantA)
	grace := time.Hour

	finished := uuid.New()    // released long ago → reaped
	justDone := uuid.New()    // done, but inside the grace window → kept
	working := uuid.New()     // in_progress → kept whatever its age
	orphan := uuid.New()      // no row at all, old → reaped
	freshOrphan := uuid.New() // no row, but recent → kept
	running := uuid.New()     // released and old, but a run holds it → kept

	dirs := map[uuid.UUID]string{
		finished:    mkTaskDir(t, root, finished, 10*time.Hour),
		justDone:    mkTaskDir(t, root, justDone, 10*time.Hour),
		working:     mkTaskDir(t, root, working, 10*time.Hour),
		orphan:      mkTaskDir(t, root, orphan, 10*time.Hour),
		freshOrphan: mkTaskDir(t, root, freshOrphan, time.Minute),
		running:     mkTaskDir(t, root, running, 10*time.Hour),
	}
	// Directories the reaper must never touch, whatever their age.
	untouchable := []string{"agents", "repos", "lost+found", "task-not-a-uuid"}
	for _, name := range untouchable {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	lister := fakeTaskLister{tasks: []domain.BoardTask{
		{ID: finished, Column: domain.TaskColumnReleased, UpdatedAt: time.Now().Add(-10 * time.Hour)},
		{ID: justDone, Column: domain.TaskColumnDone, UpdatedAt: time.Now().Add(-time.Minute)},
		{ID: working, Column: domain.TaskColumnInProgress, UpdatedAt: time.Now().Add(-10 * time.Hour)},
		{ID: running, Column: domain.TaskColumnReleased, UpdatedAt: time.Now().Add(-10 * time.Hour)},
	}}

	r := NewWorkspaceReaper(lister, fakeActive{running: true}, shared, grace)
	r.Sweep(reaperCtx(reaperTenantA))

	for id, want := range map[uuid.UUID]bool{
		finished:    false,
		orphan:      false,
		justDone:    true,
		working:     true,
		freshOrphan: true,
		running:     true,
	} {
		if got := exists(dirs[id]); got != want {
			t.Errorf("task %s: exists = %v, want %v", id, got, want)
		}
	}
	for _, name := range untouchable {
		if !exists(filepath.Join(root, name)) {
			t.Errorf("%s was removed; only task-<uuid> directories may be reaped", name)
		}
	}
}

// A database that cannot answer makes every directory look orphaned. Reaping on
// that reading would delete the workspace of every running task, so the pass has
// to end instead.
func TestWorkspaceReaperKeepsEverythingWhenTheTaskListFails(t *testing.T) {
	shared := t.TempDir()
	dir := mkTaskDir(t, tenantWorkspace(t, shared, reaperTenantA), uuid.New(), 100*time.Hour)

	r := NewWorkspaceReaper(fakeTaskLister{err: context.DeadlineExceeded}, nil, shared, time.Hour)
	r.Sweep(reaperCtx(reaperTenantA))

	if !exists(dir) {
		t.Fatal("workspace was reaped despite the task list failing")
	}
}

// --- tenancy ---------------------------------------------------------------

var (
	reaperTenantA = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	reaperTenantB = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

func reaperCtx(id uuid.UUID) context.Context {
	return tenant.With(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleMember})
}

func tenantWorkspace(t *testing.T, root string, id uuid.UUID) string {
	t.Helper()
	dir, err := workspace.TenantRoot(reaperCtx(id), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// rlsScopedActive is what HasLiveRunForTask really does on a shared database,
// and reproducing it is the point: task_agent_runs is row-level-security
// scoped, so another tenant's RUNNING task comes back (false, nil) — a
// confident wrong answer, not an error. The reaper's "every uncertain answer is
// keep" rule therefore never fires for it, which is why the guard could not be
// what protects another tenant's directory.
type rlsScopedActive struct {
	live  map[uuid.UUID]bool
	owner map[uuid.UUID]uuid.UUID
}

func (f rlsScopedActive) HasLiveRunForTask(ctx context.Context, id uuid.UUID, _ time.Duration) (bool, error) {
	acting, _ := tenant.ID(ctx)
	if f.owner[id] != acting {
		return false, nil
	}
	return f.live[id], nil
}

// rlsScopedLister returns only the acting tenant's tasks, as ListAll does.
type rlsScopedLister map[uuid.UUID][]domain.BoardTask

func (l rlsScopedLister) ListAll(ctx context.Context) ([]domain.BoardTask, error) {
	acting, _ := tenant.ID(ctx)
	return l[acting], nil
}

// TestWorkspaceReaperLeavesAnotherTenantsWorkspacesAlone is the property the
// old sweep failed: with two tenants on one volume, tenant A's hourly tick
// deleted every one of tenant B's checkouts older than the grace window,
// whatever column B's tasks were in and whatever B was running.
//
// Every directory below is old enough and idle enough that the pre-fix reaper
// would have taken it.
func TestWorkspaceReaperLeavesAnotherTenantsWorkspacesAlone(t *testing.T) {
	root := t.TempDir()
	grace := time.Hour

	rootA := tenantWorkspace(t, root, reaperTenantA)
	rootB := tenantWorkspace(t, root, reaperTenantB)

	aOrphan := uuid.New()   // A's, no row, old → A's own sweep reaps it
	bReleased := uuid.New() // B's, released long ago
	bWorking := uuid.New()  // B's, in progress
	bRunning := uuid.New()  // B's, a live run is writing into it
	bOrphan := uuid.New()   // B's, no row left at all

	dirs := map[uuid.UUID]string{
		aOrphan:   mkTaskDir(t, rootA, aOrphan, 10*time.Hour),
		bReleased: mkTaskDir(t, rootB, bReleased, 10*time.Hour),
		bWorking:  mkTaskDir(t, rootB, bWorking, 10*time.Hour),
		bRunning:  mkTaskDir(t, rootB, bRunning, 10*time.Hour),
		bOrphan:   mkTaskDir(t, rootB, bOrphan, 10*time.Hour),
	}
	// A leftover at the shared root that belongs to B. A must neither reap it
	// nor adopt it into its own subtree.
	sharedB := mkTaskDir(t, root, bReleased, 10*time.Hour)

	lister := rlsScopedLister{
		reaperTenantB: {
			{ID: bReleased, Column: domain.TaskColumnReleased, UpdatedAt: time.Now().Add(-10 * time.Hour)},
			{ID: bWorking, Column: domain.TaskColumnInProgress, UpdatedAt: time.Now().Add(-10 * time.Hour)},
			{ID: bRunning, Column: domain.TaskColumnReleased, UpdatedAt: time.Now().Add(-10 * time.Hour)},
		},
	}
	active := rlsScopedActive{
		live:  map[uuid.UUID]bool{bRunning: true},
		owner: map[uuid.UUID]uuid.UUID{bReleased: reaperTenantB, bWorking: reaperTenantB, bRunning: reaperTenantB},
	}

	r := NewWorkspaceReaper(lister, active, root, grace)
	r.Sweep(reaperCtx(reaperTenantA))

	if exists(dirs[aOrphan]) {
		t.Error("tenant A's own orphaned workspace was not reaped; the reaper stopped doing its job")
	}
	for id, name := range map[uuid.UUID]string{
		bReleased: "released", bWorking: "in progress", bRunning: "held by a live run", bOrphan: "orphaned",
	} {
		if !exists(dirs[id]) {
			t.Errorf("tenant A's sweep deleted tenant B's %s workspace", name)
		}
	}
	if !exists(sharedB) {
		t.Error("tenant A's sweep deleted a directory at the shared root that it could not account for")
	}
	if exists(filepath.Join(rootA, filepath.Base(sharedB))) {
		t.Error("tenant A adopted a shared-root directory belonging to tenant B")
	}
}

// A sweep with no identity cannot tell whose directories it is looking at, so
// it touches none of them — rather than falling back to the shared root, which
// is every tenant's.
func TestWorkspaceReaperWithNoTenantReapsNothing(t *testing.T) {
	root := t.TempDir()
	rootA := tenantWorkspace(t, root, reaperTenantA)
	dir := mkTaskDir(t, rootA, uuid.New(), 100*time.Hour)
	flat := mkTaskDir(t, root, uuid.New(), 100*time.Hour)

	r := NewWorkspaceReaper(rlsScopedLister{}, nil, root, time.Hour)
	r.Sweep(context.Background())

	if !exists(dir) || !exists(flat) {
		t.Fatal("a sweep with no tenant deleted something")
	}
}

// TestWorkspaceReaperAdoptsItsOwnFlatWorkspaces covers the volume that is live
// today: directories written under the layout that predates the tenant segment
// hold uncommitted work, and a fresh layout on its own would strand it. They
// move only on PROVEN ownership — the task uuid is in this tenant's own list,
// and task uuids cannot collide across tenants.
func TestWorkspaceReaperAdoptsItsOwnFlatWorkspaces(t *testing.T) {
	root := t.TempDir()
	rootA := tenantWorkspace(t, root, reaperTenantA)

	mine := uuid.New()
	theirs := uuid.New()
	deleted := uuid.New() // no row anywhere: nobody can prove it is theirs

	flatMine := mkTaskDir(t, root, mine, time.Minute)
	flatTheirs := mkTaskDir(t, root, theirs, time.Minute)
	flatDeleted := mkTaskDir(t, root, deleted, 100*time.Hour)

	lister := rlsScopedLister{
		reaperTenantA: {{ID: mine, Column: domain.TaskColumnInProgress, UpdatedAt: time.Now()}},
		reaperTenantB: {{ID: theirs, Column: domain.TaskColumnInProgress, UpdatedAt: time.Now()}},
	}

	r := NewWorkspaceReaper(lister, nil, root, time.Hour)
	r.Sweep(reaperCtx(reaperTenantA))

	moved := filepath.Join(rootA, "task-"+mine.String())
	if !exists(moved) {
		t.Error("a task workspace this tenant owns was not moved into its subtree")
	}
	if exists(flatMine) {
		t.Error("the flat copy was left behind, so the same task now has two checkouts")
	}
	if !exists(filepath.Join(moved, "src", "main.go")) {
		t.Error("the moved workspace lost its contents")
	}
	if !exists(flatTheirs) {
		t.Error("another tenant's flat workspace was taken")
	}
	if !exists(flatDeleted) {
		t.Error("a flat workspace nobody could be shown to own was deleted or moved")
	}
}
