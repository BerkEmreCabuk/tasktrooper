package postgres_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// MobileStoreAppStoreSuite covers the not-found contract every storeops
// caller depends on: Onboard branches on errors.Is(err, port.ErrNotFound) to
// tell "this repository has no store app yet" from "the lookup failed", and
// mobileStoreGate does the same to tell "not onboarded" from an infra error
// it must never swallow. Both live in this store's SQL + error wrapping, so
// they are verified against a real postgres rather than a fake.
type MobileStoreAppStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.MobileStoreAppStore
	repoID uuid.UUID
}

func TestMobileStoreAppStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(MobileStoreAppStoreSuite))
}

func (s *MobileStoreAppStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	// Its own runtime path, for the same reason IncidentStoreSuite has one:
	// a shared binaries cache makes concurrent initdb runs fight.
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: sharedPGRuntimeDir,
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewMobileStoreAppStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "mobile-store-test", "", "/tmp/mobile-store-test", "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *MobileStoreAppStoreSuite) TearDownSuite() {
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

// A repository that has never been onboarded, and a repository onboarded to
// the other platform, must both report ErrNotFound rather than a bare driver
// error — Onboard would otherwise abort instead of starting a fresh row.
func (s *MobileStoreAppStoreSuite) TestGetReportsNotFoundForAnUnknownRow() {
	_, err := s.store.Get(s.ctx, uuid.New(), domain.MobileStorePlatformIOS)
	s.Require().Error(err)
	s.ErrorIs(err, port.ErrNotFound)

	_, err = s.store.Get(s.ctx, s.repoID, domain.MobileStorePlatformAndroid)
	s.Require().Error(err)
	s.ErrorIs(err, port.ErrNotFound, "an unrelated platform's row must not satisfy this lookup")
}

// The other half of the contract: once a row exists, Get finds it by
// (repository_id, platform) and Upsert updates in place rather than
// appending a second row for the same pair.
func (s *MobileStoreAppStoreSuite) TestUpsertThenGetRoundTripsOnRepositoryAndPlatform() {
	stored, err := s.store.Upsert(s.ctx, domain.MobileStoreApp{
		RepositoryID: s.repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.roundtrip",
		State:        domain.MobileStoreStateOnboarding,
		Checklist: []domain.ChecklistItem{
			{Key: "ios_app_record", Title: "create the app record"},
		},
	})
	s.Require().NoError(err)
	s.Require().NotEqual(uuid.Nil, stored.ID)

	got, err := s.store.Get(s.ctx, s.repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(stored.ID, got.ID)
	s.Equal("com.example.roundtrip", got.Identifier)
	s.Require().Len(got.Checklist, 1)
	s.Equal("ios_app_record", got.Checklist[0].Key)

	updated, err := s.store.Upsert(s.ctx, domain.MobileStoreApp{
		RepositoryID: s.repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.roundtrip",
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)
	s.Equal(stored.ID, updated.ID, "upsert is keyed on (repository_id, platform)")

	rows, err := s.store.ListByRepository(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Len(rows, 1)
	s.Equal(domain.MobileStoreStateTestReady, rows[0].State)
}

// A freshly onboarded app has never synced its tracks: the JSONB column holds
// its '{}' default, which must decode to a zero domain.StoreTracks rather
// than error, and tracks_synced_at — "never", a state distinct from any
// timestamp — must stay nil rather than round-tripping to some other
// sentinel.
func (s *MobileStoreAppStoreSuite) TestUpsertLeavesUnsyncedTracksAtZeroValue() {
	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "mobile-store-test-unsynced", "", "/tmp/mobile-store-test-unsynced", "", "")
	s.Require().NoError(err)

	stored, err := s.store.Upsert(s.ctx, domain.MobileStoreApp{
		RepositoryID: repo.ID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.unsynced",
		State:        domain.MobileStoreStateOnboarding,
	})
	s.Require().NoError(err)
	s.Equal(domain.StoreTracks{}, stored.Tracks)
	s.Nil(stored.TracksSyncedAt)

	got, err := s.store.Get(s.ctx, repo.ID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal(domain.StoreTracks{}, got.Tracks)
	s.Nil(got.TracksSyncedAt)
}

// Once a store sync happens, the display name, the track cache and its sync
// timestamp all round-trip through Upsert/Get exactly as the checklist
// already does.
func (s *MobileStoreAppStoreSuite) TestUpsertRoundTripsAppNameTracksAndSyncedAt() {
	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "mobile-store-test-tracks", "", "/tmp/mobile-store-test-tracks", "", "")
	s.Require().NoError(err)

	syncedAt := time.Now().UTC().Truncate(time.Second)
	tracks := domain.StoreTracks{
		Internal:   domain.TrackRelease{HasRelease: true, Version: "1.2.3", Build: "45", Status: domain.TrackStatusLive},
		Production: domain.TrackRelease{HasRelease: false},
	}
	stored, err := s.store.Upsert(s.ctx, domain.MobileStoreApp{
		RepositoryID:   repo.ID,
		Platform:       domain.MobileStorePlatformIOS,
		Identifier:     "com.example.tracks",
		AppName:        "Tracks Test App",
		State:          domain.MobileStoreStateTestReady,
		Tracks:         tracks,
		TracksSyncedAt: &syncedAt,
	})
	s.Require().NoError(err)
	s.Equal("Tracks Test App", stored.AppName)
	s.Equal(tracks, stored.Tracks)
	s.Require().NotNil(stored.TracksSyncedAt)
	s.True(syncedAt.Equal(stored.TracksSyncedAt.UTC()), "want %s, got %s", syncedAt, stored.TracksSyncedAt.UTC())

	got, err := s.store.Get(s.ctx, repo.ID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal("Tracks Test App", got.AppName)
	s.Equal(tracks, got.Tracks)
	s.Require().NotNil(got.TracksSyncedAt)
	s.True(syncedAt.Equal(got.TracksSyncedAt.UTC()), "want %s, got %s", syncedAt, got.TracksSyncedAt.UTC())
}

// SetTracks writes the channel cache and NOTHING else. This is the whole
// reason it exists rather than a second Upsert: a sweep reads a row, spends
// seconds on the store's API, and lands its write long after a prod deploy
// may have recorded a submit. Verified against real SQL because the guarantee
// is the UPDATE's column list, which no fake can stand in for.
func (s *MobileStoreAppStoreSuite) TestSetTracksLeavesEveryOtherColumnAlone() {
	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "mobile-store-test-settracks", "", "/tmp/mobile-store-test-settracks", "", "")
	s.Require().NoError(err)

	firstPublished := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)
	stored, err := s.store.Upsert(s.ctx, domain.MobileStoreApp{
		RepositoryID:     repo.ID,
		Platform:         domain.MobileStorePlatformAndroid,
		Identifier:       "com.example.settracks",
		AppName:          "SetTracks Test App",
		State:            domain.MobileStoreStateLive,
		ReviewState:      domain.ReviewStateWaiting,
		FirstPublishedAt: &firstPublished,
		Checklist:        []domain.ChecklistItem{{Key: "play_first_upload", Title: "upload the first bundle"}},
	})
	s.Require().NoError(err)

	syncedAt := time.Now().UTC().Truncate(time.Second)
	tracks := domain.StoreTracks{Internal: domain.TrackRelease{HasRelease: true, Version: "9.9.9"}}
	updated, err := s.store.SetTracks(s.ctx, repo.ID, domain.MobileStorePlatformAndroid, tracks, syncedAt)
	s.Require().NoError(err)

	s.Equal(stored.ID, updated.ID)
	s.Equal(tracks, updated.Tracks)
	s.Require().NotNil(updated.TracksSyncedAt)
	s.True(syncedAt.Equal(updated.TracksSyncedAt.UTC()))

	// The columns a sweep must never roll back. review_state is the one the
	// monitor's poll gate turns on: lose it and that release's store verdict
	// is never seen again.
	s.Equal(domain.ReviewStateWaiting, updated.ReviewState)
	s.Equal(domain.MobileStoreStateLive, updated.State)
	s.Equal("SetTracks Test App", updated.AppName)
	s.Require().NotNil(updated.FirstPublishedAt)
	s.True(firstPublished.Equal(updated.FirstPublishedAt.UTC()))
	s.Require().Len(updated.Checklist, 1)
	s.Equal("play_first_upload", updated.Checklist[0].Key)
}

// A row that is not there is ErrNotFound, not a silent no-op: the monitor
// carries on with the caller's own copy on failure, and "the row vanished"
// has to be distinguishable from "the write landed".
func (s *MobileStoreAppStoreSuite) TestSetTracksReportsNotFoundForAnUnknownRow() {
	_, err := s.store.SetTracks(s.ctx, uuid.New(), domain.MobileStorePlatformIOS, domain.StoreTracks{}, time.Now())
	s.Require().Error(err)
	s.ErrorIs(err, port.ErrNotFound)
}
