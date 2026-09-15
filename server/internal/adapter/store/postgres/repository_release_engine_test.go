package postgres_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// RepositoryReleaseEngineSuite covers release_engine's persistence: it must
// default to domain.ReleaseEngineAuto (the column default, migration 126) on
// a freshly created repository, and round-trip through UpdateReleaseEngine the
// same way mobile_platform round-trips through UpdateMobilePlatform.
type RepositoryReleaseEngineSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	repos  *postgres.RepositoryStore
}

func TestRepositoryReleaseEngineSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RepositoryReleaseEngineSuite))
}

func (s *RepositoryReleaseEngineSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.repos = postgres.NewRepositoryStore(s.db)
}

func (s *RepositoryReleaseEngineSuite) TearDownSuite() {
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

func (s *RepositoryReleaseEngineSuite) TestCreateDefaultsReleaseEngineToAuto() {
	repo, err := s.repos.Create(s.ctx, "release-engine-default", "", "/tmp/release-engine-default", "", "")
	s.Require().NoError(err)
	s.Equal(domain.ReleaseEngineAuto, repo.ReleaseEngine)

	got, err := s.repos.Get(s.ctx, repo.ID)
	s.Require().NoError(err)
	s.Equal(domain.ReleaseEngineAuto, got.ReleaseEngine)
}

func (s *RepositoryReleaseEngineSuite) TestUpdateReleaseEngineRoundTrips() {
	repo, err := s.repos.Create(s.ctx, "release-engine-update", "", "/tmp/release-engine-update", "", "")
	s.Require().NoError(err)

	updated, err := s.repos.UpdateReleaseEngine(s.ctx, repo.ID, domain.ReleaseEngineLocal)
	s.Require().NoError(err)
	s.Equal(domain.ReleaseEngineLocal, updated.ReleaseEngine)

	got, err := s.repos.Get(s.ctx, repo.ID)
	s.Require().NoError(err)
	s.Equal(domain.ReleaseEngineLocal, got.ReleaseEngine)
}
