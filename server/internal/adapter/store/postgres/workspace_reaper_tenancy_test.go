package postgres_test

import (
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	boardapp "github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The workspace reaper against the real thing: two tenants, one volume, one
// pool, and row-level security doing to the reaper's queries exactly what it
// does in production.
//
// A Go-level fake can only prove that the fake filters the way the test author
// thinks RLS does. The defect this covers was precisely a wrong belief about
// that: HasLiveRunForTask was assumed to fail loudly for a foreign task, and
// the reaper's "every uncertain answer is keep" rule was written on that
// assumption. It returns a confident (false, nil) instead, which is asserted
// below rather than described.

// TestHasLiveRunForTaskLiesAboutAnotherTenantsRun pins the fact the reaper
// cannot be built on. It is not a bug in the store — a tenant-scoped query
// answering only about that tenant's rows is the whole point — it is a bug in
// using the answer as if it were about the task.
func (s *TenantIsolationSuite) TestHasLiveRunForTaskLiesAboutAnotherTenantsRun() {
	a, b := uuid.New(), uuid.New()
	runs := postgres.NewTaskAgentRunStore(s.db)

	task := s.taskFor(b, "reaper-live")
	s.startRun(b, task.ID, task.RepositoryID)

	live, err := runs.HasLiveRunForTask(s.scoped(b), task.ID, time.Hour)
	s.Require().NoError(err)
	s.True(live, "the owning tenant must see its own running run")

	live, err = runs.HasLiveRunForTask(s.scoped(a), task.ID, time.Hour)
	s.Require().NoError(err,
		"a foreign task is NOT an error here — that is the whole problem")
	s.False(live,
		"another tenant's running task reads as idle, so no guard downstream may rely on this")
}

// TestReaperLeavesAnotherTenantsWorkspacesAlone is the end-to-end property, on
// a real database as the unprivileged NOBYPASSRLS role: tenant A's hourly tick
// must leave tenant B's checkouts byte-for-byte where they were.
//
// Every one of B's directories below is one the pre-fix reaper deleted: the
// listing enumerated the whole shared root, the keep set held only A's rows,
// and the two guards that were supposed to stop it both answered "go ahead".
func (s *TenantIsolationSuite) TestReaperLeavesAnotherTenantsWorkspacesAlone() {
	a, b := uuid.New(), uuid.New()
	shared := s.T().TempDir()

	tasks := postgres.NewBoardTaskStore(s.db)
	runs := postgres.NewTaskAgentRunStore(s.db)

	bReleased := s.taskFor(b, "reaper-released")
	s.releaseTask(b, bReleased)
	bRunning := s.taskFor(b, "reaper-running")
	s.startRun(b, bRunning.ID, bRunning.RepositoryID)

	aReleased := s.taskFor(a, "reaper-a-released")
	s.releaseTask(a, aReleased)

	rootA := s.tenantWorkspaceRoot(shared, a)
	rootB := s.tenantWorkspaceRoot(shared, b)

	aDir := s.mkWorkspace(rootA, aReleased.ID)
	bReleasedDir := s.mkWorkspace(rootB, bReleased.ID)
	bRunningDir := s.mkWorkspace(rootB, bRunning.ID)
	bOrphanDir := s.mkWorkspace(rootB, uuid.New())
	// A leftover at the SHARED root from the layout that predates the tenant
	// segment. It is tenant B's, so tenant A may neither reap it nor adopt it —
	// and it is the directory the pre-fix listing found first.
	bFlatDir := s.mkWorkspace(shared, bReleased.ID)

	reaper := boardapp.NewWorkspaceReaper(tasks, runs, shared, time.Minute)
	reaper.Sweep(s.scoped(a))

	s.NoDirExists(aDir, "tenant A's own finished workspace should have been reclaimed")
	for name, dir := range map[string]string{
		"released":           bReleasedDir,
		"held by a live run": bRunningDir,
		"orphaned":           bOrphanDir,
	} {
		s.DirExists(dir, "tenant A's sweep deleted tenant B's %s workspace", name)
		s.FileExists(filepath.Join(dir, "uncommitted.txt"),
			"tenant B's %s workspace lost its uncommitted work", name)
	}
	s.DirExists(bFlatDir, "tenant A's sweep deleted a shared-root directory it could not account for")
	s.NoDirExists(filepath.Join(rootA, filepath.Base(bFlatDir)),
		"tenant A adopted a shared-root directory belonging to tenant B")
}

// --- helpers ---------------------------------------------------------------

func (s *TenantIsolationSuite) tenantWorkspaceRoot(shared string, id uuid.UUID) string {
	s.T().Helper()
	root, err := workspace.TenantRoot(s.scoped(id), shared)
	s.Require().NoError(err)
	s.Require().NoError(os.MkdirAll(root, 0o755))
	return root
}

// mkWorkspace makes a checkout old enough to be past any grace window, holding
// a file that only exists there — the uncommitted work the grace window is for.
func (s *TenantIsolationSuite) mkWorkspace(root string, taskID uuid.UUID) string {
	s.T().Helper()
	dir := filepath.Join(root, "task-"+taskID.String())
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "uncommitted.txt"), []byte("work"), 0o644))
	old := time.Now().Add(-72 * time.Hour)
	s.Require().NoError(os.Chtimes(dir, old, old))
	return dir
}

// taskFor creates a repository and a task for one tenant, through the stores,
// so every row lands under that tenant's policy rather than by hand.
func (s *TenantIsolationSuite) taskFor(id uuid.UUID, label string) domain.BoardTask {
	s.T().Helper()
	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.scoped(id), label+"-"+id.String()[:8], "", "/nonexistent/"+uuid.NewString(), "", "")
	s.Require().NoError(err)

	tasks := postgres.NewBoardTaskStore(s.db)
	task, err := tasks.Create(s.scoped(id), domain.BoardTask{
		RepositoryID: repo.ID,
		TaskNumber:   int(time.Now().UnixNano() % 1_000_000),
		Title:        label,
		TaskType:     domain.TaskTypeTask,
		Column:       domain.TaskColumnInProgress,
		Priority:     domain.TaskPriorityMedium,
	})
	s.Require().NoError(err)
	return task
}

// releaseTask puts a task in released with an updated_at old enough to be past
// the reaper's grace window. The column move is done in SQL because the store's
// Update stamps updated_at with now(), which is the opposite of what is needed.
func (s *TenantIsolationSuite) releaseTask(id uuid.UUID, task domain.BoardTask) {
	s.T().Helper()
	_, err := s.db.Exec(s.scoped(id), `
		UPDATE board_tasks SET board_column = 'released', updated_at = now() - interval '72 hours'
		 WHERE id = $1`, task.ID)
	s.Require().NoError(err)
}

// startRun marks a task as being executed right now. Written in SQL because
// TaskAgentRunStore.Create demands a board event this test has no need of; what
// matters here is only that a 'running' row with a fresh heartbeat exists under
// that tenant's policy.
func (s *TenantIsolationSuite) startRun(id, taskID, repositoryID uuid.UUID) {
	s.T().Helper()
	var agentID uuid.UUID
	s.Require().NoError(s.db.QueryRow(s.scoped(id),
		`INSERT INTO agents (name) VALUES ($1) RETURNING id`,
		"reaper-agent-"+uuid.NewString()[:8]).Scan(&agentID))
	var eventID uuid.UUID
	s.Require().NoError(s.db.QueryRow(s.scoped(id), `
		INSERT INTO board_events (repository_id, task_id, event_type) VALUES ($1, $2, 'dispatch')
		RETURNING id`, repositoryID, taskID).Scan(&eventID))
	_, err := s.db.Exec(s.scoped(id),
		`INSERT INTO task_agent_runs (task_id, agent_id, board_event_id, status) VALUES ($1, $2, $3, 'running')`,
		taskID, agentID, eventID)
	s.Require().NoError(err)
}
