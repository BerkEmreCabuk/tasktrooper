package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// DeploymentRunStoreSuite covers the local mirror of GitHub Actions deploy
// runs: the attribution-preserving upsert (a later poll must never blank out
// what the dispatch reconciler stamped) and the rollback lookup's not-found
// contract, which the service turns into a 409 rather than a 500.
type DeploymentRunStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.DeploymentRunStore
	repoID uuid.UUID
}

func TestDeploymentRunStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(DeploymentRunStoreSuite))
}

func (s *DeploymentRunStoreSuite) SetupSuite() {
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
	// Stores take the tenant-scoped handle now, and every statement it issues
	// reads app.tenant_id off the context - so the suite has to BE a tenant.
	// A fresh uuid per suite means two suites sharing an embedded Postgres
	// cannot see each other's rows, which is the property under test anyway.
	s.db = postgres.NewDB(pool)
	s.ctx = tenant.With(s.ctx, tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner})
	s.store = postgres.NewDeploymentRunStore(s.db)
}

func (s *DeploymentRunStoreSuite) TearDownSuite() {
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

func (s *DeploymentRunStoreSuite) SetupTest() {
	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "deployment-run-test", "", "/tmp/deployment-run-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

// Upsert must not clobber attribution the monitor never knows about: the
// dispatch reconciler stamps trigger_source/triggered_by, and the very next
// poll re-upserts the same run with those fields empty.
func (s *DeploymentRunStoreSuite) TestUpsertPreservesAttribution() {
	base := domain.DeploymentRun{
		RepositoryID: s.repoID, Env: "prod", RunID: 42,
		WorkflowFile: "prod.yml", HeadSHA: "abc123", HeadRef: "main",
		Status: domain.RunStatusInProgress,
	}
	_, err := s.store.Upsert(s.ctx, base)
	s.Require().NoError(err)

	s.Require().NoError(s.store.Stamp(s.ctx, s.repoID, 42, domain.TriggerSourceUI, "akif", ""))

	base.Status = domain.RunStatusCompleted
	base.Conclusion = domain.RunConclusionSuccess
	got, err := s.store.Upsert(s.ctx, base)
	s.Require().NoError(err)
	s.Equal(domain.TriggerSourceUI, got.TriggerSource)
	s.Equal("akif", got.TriggeredBy)
	s.Equal(domain.RunConclusionSuccess, got.Conclusion)
}

// LastSuccessfulBefore is rollback's only lookup: newest success that is not
// the SHA currently deployed.
func (s *DeploymentRunStoreSuite) TestLastSuccessfulBeforeSkipsCurrentSHA() {
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	for _, r := range []domain.DeploymentRun{
		{RepositoryID: s.repoID, Env: "prod", RunID: 1, HeadSHA: "good1", Status: domain.RunStatusCompleted, Conclusion: domain.RunConclusionSuccess, StartedAt: &older, CompletedAt: &older},
		{RepositoryID: s.repoID, Env: "prod", RunID: 2, HeadSHA: "bad", Status: domain.RunStatusCompleted, Conclusion: domain.RunConclusionFailure, StartedAt: &newer, CompletedAt: &newer},
	} {
		_, err := s.store.Upsert(s.ctx, r)
		s.Require().NoError(err)
	}
	got, err := s.store.LastSuccessfulBefore(s.ctx, s.repoID, "prod", "bad")
	s.Require().NoError(err)
	s.Equal("good1", got.HeadSHA)
}

// No prior success at all must be distinguishable from a failed lookup —
// the service turns exactly this into a 409, not a 500.
func (s *DeploymentRunStoreSuite) TestLastSuccessfulBeforeNotFound() {
	_, err := s.store.LastSuccessfulBefore(s.ctx, s.repoID, "prod", "anything")
	s.Require().Error(err)
	s.True(errors.Is(err, port.ErrNotFound))
}

// Latest is the status matrix's cell lookup: newest run for (repository,
// env) by started_at, and ErrNotFound — not a bare empty result — for an env
// that has never deployed, so the service can tell "unconfigured/never run"
// from an infra failure.
func (s *DeploymentRunStoreSuite) TestLatestReturnsNewestAndNotFoundForUndeployedEnv() {
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	for _, r := range []domain.DeploymentRun{
		{RepositoryID: s.repoID, Env: "prod", RunID: 401, HeadSHA: "old", Status: domain.RunStatusCompleted, StartedAt: &older},
		{RepositoryID: s.repoID, Env: "prod", RunID: 402, HeadSHA: "new", Status: domain.RunStatusCompleted, StartedAt: &newer},
	} {
		_, err := s.store.Upsert(s.ctx, r)
		s.Require().NoError(err)
	}

	got, err := s.store.Latest(s.ctx, s.repoID, "prod")
	s.Require().NoError(err)
	s.Equal("new", got.HeadSHA, "Latest must return the run with the newest started_at, not just any row")

	_, err = s.store.Latest(s.ctx, s.repoID, "stage")
	s.Require().Error(err)
	s.True(errors.Is(err, port.ErrNotFound), "an env with no runs must report ErrNotFound")
}

// LatestAll backs the cross-repo status matrix's DISTINCT ON (repository_id,
// env): exactly one row per (repository, env) across every repository, and
// that row must be the newest one, not merely a row.
func (s *DeploymentRunStoreSuite) TestLatestAllOneNewestRowPerRepositoryEnv() {
	repos := postgres.NewRepositoryStore(s.db)
	repoA := s.repoID
	repoB, err := repos.Create(s.ctx, "deployment-run-test-b", "", "/tmp/deployment-run-test-b-"+uuid.New().String(), "", "")
	s.Require().NoError(err)

	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)

	for _, r := range []domain.DeploymentRun{
		{RepositoryID: repoA, Env: "prod", RunID: 501, HeadSHA: "a-prod-old", Status: domain.RunStatusCompleted, StartedAt: &older},
		{RepositoryID: repoA, Env: "prod", RunID: 502, HeadSHA: "a-prod-new", Status: domain.RunStatusCompleted, StartedAt: &newer},
		{RepositoryID: repoA, Env: "stage", RunID: 503, HeadSHA: "a-stage", Status: domain.RunStatusCompleted, StartedAt: &newer},
		{RepositoryID: repoB.ID, Env: "prod", RunID: 601, HeadSHA: "b-prod", Status: domain.RunStatusCompleted, StartedAt: &newer},
	} {
		_, err := s.store.Upsert(s.ctx, r)
		s.Require().NoError(err)
	}

	all, err := s.store.LatestAll(s.ctx)
	s.Require().NoError(err)

	// Other tests in this suite create their own repositories, so filter to
	// just the two seeded here and assert on those — but still prove
	// uniqueness by checking no (repository, env) pair repeats.
	byKey := map[string]domain.DeploymentRun{}
	for _, r := range all {
		key := r.RepositoryID.String() + "/" + r.Env
		_, dup := byKey[key]
		s.Require().False(dup, "LatestAll must return at most one row per (repository, env), got a duplicate for %s", key)
		byKey[key] = r
	}

	s.Require().Contains(byKey, repoA.String()+"/prod")
	s.Require().Contains(byKey, repoA.String()+"/stage")
	s.Require().Contains(byKey, repoB.ID.String()+"/prod")
	s.Equal("a-prod-new", byKey[repoA.String()+"/prod"].HeadSHA, "must be the newest prod run for repoA, not the older one")
	s.Equal("a-stage", byKey[repoA.String()+"/stage"].HeadSHA)
	s.Equal("b-prod", byKey[repoB.ID.String()+"/prod"].HeadSHA)
}

// ListByEnv backs the detail drawer's run history: newest-first, capped at
// limit.
func (s *DeploymentRunStoreSuite) TestListByEnvNewestFirstAndHonoursLimit() {
	base := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 3; i++ {
		startedAt := base.Add(time.Duration(i) * time.Minute) // strictly increasing: run 2 is newest
		_, err := s.store.Upsert(s.ctx, domain.DeploymentRun{
			RepositoryID: s.repoID, Env: "prod", RunID: int64(700 + i),
			HeadSHA: fmt.Sprintf("sha-%d", i), Status: domain.RunStatusCompleted, StartedAt: &startedAt,
		})
		s.Require().NoError(err)
	}

	got, err := s.store.ListByEnv(s.ctx, s.repoID, "prod", 2)
	s.Require().NoError(err)
	s.Require().Len(got, 2, "limit must be honoured even though 3 runs exist")
	s.Equal("sha-2", got[0].HeadSHA, "newest first")
	s.Equal("sha-1", got[1].HeadSHA)
}

// DeployDispatchStoreSuite covers dispatch intent recorded ahead of GitHub's
// 204-with-no-run-id response, and the monitor's reconciliation lifecycle:
// pending -> matched (or abandoned).
type DeployDispatchStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.DeployDispatchStore
	repoID uuid.UUID
}

func TestDeployDispatchStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(DeployDispatchStoreSuite))
}

func (s *DeployDispatchStoreSuite) SetupSuite() {
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
	// Stores take the tenant-scoped handle now, and every statement it issues
	// reads app.tenant_id off the context - so the suite has to BE a tenant.
	// A fresh uuid per suite means two suites sharing an embedded Postgres
	// cannot see each other's rows, which is the property under test anyway.
	s.db = postgres.NewDB(pool)
	s.ctx = tenant.With(s.ctx, tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner})
	s.store = postgres.NewDeployDispatchStore(s.db)
}

func (s *DeployDispatchStoreSuite) TearDownSuite() {
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

func (s *DeployDispatchStoreSuite) SetupTest() {
	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "deploy-dispatch-test", "", "/tmp/deploy-dispatch-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

// Create records the intent; ListPending surfaces it until Resolve moves it
// out of the pending state, at which point ListPending stops returning it.
func (s *DeployDispatchStoreSuite) TestCreateListPendingResolve() {
	created, err := s.store.Create(s.ctx, domain.DeployDispatch{
		RepositoryID: s.repoID, Env: "prod", WorkflowFile: "prod.yml",
		Ref: "main", Kind: domain.DispatchKindDeploy, Actor: "akif",
	})
	s.Require().NoError(err)
	s.Equal(domain.DispatchStatePending, created.State)

	pending, err := s.store.ListPending(s.ctx)
	s.Require().NoError(err)
	found := false
	for _, d := range pending {
		if d.ID == created.ID {
			found = true
		}
	}
	s.True(found, "a freshly created dispatch must appear in ListPending")

	runID := int64(99)
	s.Require().NoError(s.store.Resolve(s.ctx, created.ID, domain.DispatchStateMatched, &runID))

	pending, err = s.store.ListPending(s.ctx)
	s.Require().NoError(err)
	for _, d := range pending {
		s.NotEqual(created.ID, d.ID, "a resolved dispatch must no longer be pending")
	}
}

// OpsAuditStoreSuite covers the console's action log: every attempt is
// recorded whether it succeeds or fails, newest first, optionally filtered
// to one repository.
type OpsAuditStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.OpsAuditStore
	repoID uuid.UUID
}

func TestOpsAuditStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(OpsAuditStoreSuite))
}

func (s *OpsAuditStoreSuite) SetupSuite() {
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
	// Stores take the tenant-scoped handle now, and every statement it issues
	// reads app.tenant_id off the context - so the suite has to BE a tenant.
	// A fresh uuid per suite means two suites sharing an embedded Postgres
	// cannot see each other's rows, which is the property under test anyway.
	s.db = postgres.NewDB(pool)
	s.ctx = tenant.With(s.ctx, tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner})
	s.store = postgres.NewOpsAuditStore(s.db)
}

func (s *OpsAuditStoreSuite) TearDownSuite() {
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

func (s *OpsAuditStoreSuite) SetupTest() {
	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "ops-audit-test", "", "/tmp/ops-audit-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

// Log records an ok and an error attempt; List returns both, newest first,
// with and without a repository filter.
func (s *OpsAuditStoreSuite) TestLogAndList() {
	repoID := s.repoID
	s.Require().NoError(s.store.Log(s.ctx, domain.OpsAuditEntry{
		RepositoryID: &repoID, Action: domain.OpsActionDeploy, Target: "prod",
		Actor: "akif", Outcome: domain.OpsOutcomeOK,
	}))
	time.Sleep(10 * time.Millisecond)
	s.Require().NoError(s.store.Log(s.ctx, domain.OpsAuditEntry{
		RepositoryID: &repoID, Action: domain.OpsActionRollback, Target: "prod",
		Actor: "akif", Outcome: domain.OpsOutcomeError, Error: "github: 502",
	}))

	filtered, err := s.store.List(s.ctx, &repoID, 10)
	s.Require().NoError(err)
	s.Require().Len(filtered, 2)
	s.Equal(domain.OpsActionRollback, filtered[0].Action, "newest first")
	s.Equal(domain.OpsOutcomeError, filtered[0].Outcome)
	s.Equal("github: 502", filtered[0].Error)
	s.Equal(domain.OpsActionDeploy, filtered[1].Action)
	s.Equal(domain.OpsOutcomeOK, filtered[1].Outcome)

	all, err := s.store.List(s.ctx, nil, 10)
	s.Require().NoError(err)
	s.GreaterOrEqual(len(all), 2, "an unfiltered List must include every repository")
}
