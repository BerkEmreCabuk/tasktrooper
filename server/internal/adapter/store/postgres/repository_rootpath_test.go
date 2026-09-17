package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// RepositoryRootPathSuite covers what happens when one database is served by
// two hosts: the cloud pod (PVC at /data) and the user's Mac behind a
// reverse tunnel. Rows written by one carry an absolute path the other cannot
// use — the production failure was a board run on the Mac calling git clone
// into "/data/..." and getting "mkdir /data: read-only file system".
type RepositoryRootPathSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	// hostRoot stands in for this host's cfg.Storage.Sessions.WorkspaceRoot.
	hostRoot string
	store    *postgres.RepositoryStore
}

func TestRepositoryRootPathSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RepositoryRootPathSuite))
}

func (s *RepositoryRootPathSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	tmp := s.T().TempDir()
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.hostRoot = filepath.Join(tmp, "local-runner", "data", "workspaces")
	s.store = postgres.NewRepositoryStore(s.db).SetHostRoots(s.hostRoot, nil)
}

func (s *RepositoryRootPathSuite) TearDownSuite() {
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

// A row written by the pod is readable here as a path this host can create.
func (s *RepositoryRootPathSuite) TestCloudWrittenPathIsReanchoredOnEveryRead() {
	const cloudPath = "/data/workspaces/repos/acme-web"
	if _, err := os.Stat(cloudPath); err == nil {
		s.T().Skip("/data exists on this host; the foreign-path case cannot be simulated")
	}
	want := filepath.Join(s.hostRoot, "repos", "acme-web")

	created, err := s.store.Create(s.ctx, "acme-web", "", cloudPath, "https://github.com/makifbaysal/acme-web.git", "")
	s.Require().NoError(err)
	s.Equal(want, created.RootPath, "create's RETURNING read must be re-anchored too")

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(want, got.RootPath)

	listed, err := s.store.List(s.ctx)
	s.Require().NoError(err)
	found := false
	for _, r := range listed {
		if r.ID == created.ID {
			found = true
			s.Equal(want, r.RootPath)
		}
	}
	s.True(found, "the repository must appear in List")

	// The column itself is untouched: writes stay host-absolute and the
	// translation is a read-time concern, which is what keeps it symmetric.
	var stored string
	s.Require().NoError(s.db.QueryRow(s.ctx, `SELECT root_path FROM repositories WHERE id = $1`, created.ID).Scan(&stored))
	s.Equal(cloudPath, stored)
}

// The exact lookup still works, and so does a lookup by this host's own path
// for a row that stores the other host's — otherwise re-opening the repository
// here would insert a duplicate row for the same remote.
func (s *RepositoryRootPathSuite) TestGetByRootPathResolvesAcrossHosts() {
	const cloudPath = "/data/workspaces/repos/tunnel-repo"
	if _, err := os.Stat(cloudPath); err == nil {
		s.T().Skip("/data exists on this host; the foreign-path case cannot be simulated")
	}
	created, err := s.store.Create(s.ctx, "tunnel-repo", "", cloudPath, "", "")
	s.Require().NoError(err)

	exact, err := s.store.GetByRootPath(s.ctx, cloudPath)
	s.Require().NoError(err)
	s.Equal(created.ID, exact.ID)
	s.Equal(filepath.Join(s.hostRoot, "repos", "tunnel-repo"), exact.RootPath)

	// What repository.Service.Open passes on this host: its own absolute path,
	// which no row contains.
	local, err := s.store.GetByRootPath(s.ctx, filepath.Join(s.hostRoot, "repos", "tunnel-repo"))
	s.Require().NoError(err)
	s.Equal(created.ID, local.ID)
}

// A row that names a path this host can really use is a different repository
// that merely shares a directory name, and must not be folded into the lookup.
func (s *RepositoryRootPathSuite) TestGetByRootPathDoesNotMatchALocalRowByNameAlone() {
	localPath := filepath.Join(s.hostRoot, "repos", "samename")
	s.Require().NoError(os.MkdirAll(localPath, 0o755))
	_, err := s.store.Create(s.ctx, "samename", "", localPath, "", "")
	s.Require().NoError(err)

	_, err = s.store.GetByRootPath(s.ctx, filepath.Join(s.T().TempDir(), "elsewhere", "samename"))
	s.Require().Error(err, "a usable local row must not be matched by directory name")
}
