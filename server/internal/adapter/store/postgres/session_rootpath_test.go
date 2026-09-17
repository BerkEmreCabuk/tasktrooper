package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// SessionPathSuite is RepositoryRootPathSuite for the session columns. Same
// two-host problem: sessions.workspace_dir and sessions.project_root are
// absolute paths belonging to whichever host wrote the row, and resuming a
// pod-written chat on the Mac used to reach os.MkdirAll("/data/workspaces/...")
// in ensureSessionWorkspace and fail the turn with
// "mkdir /data: read-only file system".
type SessionPathSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	// hostRoot stands in for this host's cfg.Storage.Sessions.WorkspaceRoot.
	hostRoot string
	store    *postgres.SessionStore
}

func TestSessionPathSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(SessionPathSuite))
}

// The startup budget is its OWN context, not the one the tests then run on.
//
// Both used to be a single 3-minute deadline started before postgres existed,
// and bringing an embedded postgres up is the slow, variable part: extracting
// the runtime on a cold machine and applying every migration can eat most of
// that budget, leaving the tests to run against a context already close to
// expiry. A deadline that fires between an INSERT's RETURNING row and its
// commit gives back a row whose id is real and whose table entry is not — which
// is how TestFindByTaskIsReanchored managed to hold a valid task id and still
// fail BindTask with a foreign-key violation on sessions.task_id, once, and
// pass on every rerun.
//
// So: a bounded context for the thing that can hang (starting postgres), and a
// plain cancellable one for the statements, whose own bound is the test
// binary's -timeout.
func (s *SessionPathSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	startupCtx, cancelStartup := context.WithTimeout(s.ctx, 3*time.Minute)
	defer cancelStartup()

	tmp := s.T().TempDir()
	pg, err := newTestDatabase(startupCtx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.hostRoot = filepath.Join(tmp, "local-runner", "data", "workspaces")
	s.store = postgres.NewSessionStore(s.db).SetHostRoots(s.hostRoot, nil)
}

func (s *SessionPathSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// skipIfDataExists keeps the foreign-path cases honest on a host that really
// does have /data (the pod running its own tests).
func (s *SessionPathSuite) skipIfDataExists(path string) {
	if _, err := os.Stat(path); err == nil {
		s.T().Skip("/data exists on this host; the foreign-path case cannot be simulated")
	}
}

// The production failure, on every read path a chat turn uses.
func (s *SessionPathSuite) TestCloudWrittenWorkspaceDirIsReanchoredOnEveryRead() {
	taskID := uuid.New()
	cloudPath := "/data/workspaces/task-" + taskID.String()
	s.skipIfDataExists("/data")
	want := filepath.Join(s.hostRoot, "task-"+taskID.String())

	created, err := s.store.Create(s.ctx, "pod chat", "sonnet", cloudPath, nil, nil, nil)
	s.Require().NoError(err)
	s.Equal(want, created.WorkspaceDir, "create's RETURNING read must be re-anchored too")

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(want, got.WorkspaceDir)

	listed, err := s.store.List(s.ctx, 100, 0)
	s.Require().NoError(err)
	found := false
	for _, sess := range listed {
		if sess.ID == created.ID {
			found = true
			s.Equal(want, sess.WorkspaceDir)
		}
	}
	s.True(found, "the session must appear in List")

	// The column itself is untouched: writes stay host-absolute and the
	// translation is a read-time concern, which is what keeps it symmetric.
	var stored string
	s.Require().NoError(s.db.QueryRow(s.ctx, `SELECT workspace_dir FROM sessions WHERE id = $1`, created.ID).Scan(&stored))
	s.Equal(cloudPath, stored)
}

// project_root travels the same way — it is what the index endpoints and the
// code tools scope themselves to.
func (s *SessionPathSuite) TestCloudWrittenProjectRootIsReanchored() {
	s.skipIfDataExists("/data")
	created, err := s.store.Create(s.ctx, "pod chat", "", "/data/workspaces/repos/acme-web", nil, nil, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.store.UpdateProjectRoot(s.ctx, created.ID, "/data/workspaces/repos/acme-web"))

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	want := filepath.Join(s.hostRoot, "repos", "acme-web")
	s.Equal(want, got.ProjectRoot)
	s.Equal(want, got.WorkspaceDir)
}

// A workspace this host can really use is left alone — the guarantee that a
// live local checkout is never traded for an empty new directory.
func (s *SessionPathSuite) TestLocallyValidPathIsUntouched() {
	local := filepath.Join(s.hostRoot, "task-"+uuid.NewString())
	s.Require().NoError(os.MkdirAll(local, 0o755))

	created, err := s.store.Create(s.ctx, "local chat", "", local, nil, nil, nil)
	s.Require().NoError(err)
	s.Equal(local, created.WorkspaceDir)

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(local, got.WorkspaceDir)
}

// Neither host owns the column, so the translation has to round-trip: a row
// written here reads as the pod's own path on the pod, and reads back as this
// host's original path when it is read here again.
func (s *SessionPathSuite) TestSymmetricAcrossHosts() {
	taskID := uuid.New()
	macPath := filepath.Join(s.hostRoot, "task-"+taskID.String())
	s.skipIfDataExists("/data")

	created, err := s.store.Create(s.ctx, "mac chat", "", macPath, nil, nil, nil)
	s.Require().NoError(err)

	// The same row read by a store configured as the pod.
	onPod := postgres.NewSessionStore(s.db).SetHostRoots("/data/workspaces", nil)
	podSess, err := onPod.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal("/data/workspaces/task-"+taskID.String(), podSess.WorkspaceDir)

	// Whatever the pod would then write back, read here, lands where it began.
	s.Require().NoError(s.store.UpdateWorkspaceDir(s.ctx, created.ID, podSess.WorkspaceDir))
	backOnMac, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(macPath, backOnMac.WorkspaceDir)
}

// FindByTask is the task chat's own read path — how a human reopens the chat
// about a card — and must translate like the rest.
func (s *SessionPathSuite) TestFindByTaskIsReanchored() {
	s.skipIfDataExists("/data")

	repo, err := postgres.NewRepositoryStore(s.db).Create(
		s.ctx, "findbytask-repo", "", filepath.Join(s.hostRoot, "repos", "findbytask-repo"), "", "")
	s.Require().NoError(err)
	tasks := postgres.NewBoardTaskStore(s.db)
	number, err := tasks.NextTaskNumber(s.ctx, domain.TaskTypeTask)
	s.Require().NoError(err)
	task, err := tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: repo.ID,
		TaskNumber:   number,
		Title:        "cross-host chat",
		TaskType:     domain.TaskTypeTask,
		Column:       domain.TaskColumnTodo,
		Priority:     domain.TaskPriorityMedium,
	})
	s.Require().NoError(err)
	taskID := task.ID
	// The fixture has to be COMMITTED before a row references it, and this is
	// where that is checked: sessions.task_id has a foreign key onto it, so a
	// task that was returned but not persisted surfaces two statements later as
	// an opaque constraint violation on the session. Reading it back names the
	// failure where it happened.
	s.Require().NotEqual(uuid.Nil, taskID)
	readBack, err := tasks.Get(s.ctx, repo.ID, taskID)
	s.Require().NoError(err, "the board task must be committed before a session can reference it")
	s.Require().Equal(taskID, readBack.ID)

	cloudPath := "/data/workspaces/task-" + taskID.String()

	created, err := s.store.Create(s.ctx, "pod task chat", "", cloudPath, nil, nil, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.store.BindTask(s.ctx, created.ID, taskID))

	got, ok, err := s.store.FindByTask(s.ctx, taskID)
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(filepath.Join(s.hostRoot, "task-"+taskID.String()), got.WorkspaceDir)
}
