package storeops_test

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Fakes for the storeops package's collaborators. These are infrastructure
// for Task 6 and every task after it (signing, onboarding, the monitor) —
// keep them complete and reusable rather than growing a second set later.

// fakeCredentialStore is an in-memory port.StoreCredentialStore. It only
// ever sees the bytes the service hands it — the fake has no idea whether
// they are encrypted, and asserting that they are not plaintext is the
// caller's job.
type fakeCredentialStore struct {
	mu   sync.Mutex
	rows map[string]credentialRow
}

type credentialRow struct {
	data      []byte
	updatedAt time.Time
}

var _ port.StoreCredentialStore = (*fakeCredentialStore)(nil)

func newFakeCredentialStore() *fakeCredentialStore {
	return &fakeCredentialStore{rows: map[string]credentialRow{}}
}

func (f *fakeCredentialStore) Set(_ context.Context, provider string, encrypted []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[provider] = credentialRow{data: append([]byte(nil), encrypted...), updatedAt: time.Now()}
	return nil
}

func (f *fakeCredentialStore) Get(_ context.Context, provider string) ([]byte, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[provider]
	if !ok {
		return nil, time.Time{}, fmt.Errorf("get store credential: %w", port.ErrNotFound)
	}
	return row.data, row.updatedAt, nil
}

func (f *fakeCredentialStore) Delete(_ context.Context, provider string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, provider)
	return nil
}

func (f *fakeCredentialStore) List(_ context.Context) (map[string]time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]time.Time, len(f.rows))
	for provider, row := range f.rows {
		out[provider] = row.updatedAt
	}
	return out, nil
}

// count is a test convenience for asserting "nothing was persisted".
func (f *fakeCredentialStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// fakeMobileStoreAppStore is an in-memory port.MobileStoreAppStore, keyed on
// (repository_id, platform) exactly like the postgres store's unique
// constraint — Upsert overwrites in place instead of appending.
type fakeMobileStoreAppStore struct {
	mu   sync.Mutex
	rows map[mobileAppKey]domain.MobileStoreApp
}

type mobileAppKey struct {
	repositoryID uuid.UUID
	platform     string
}

var _ port.MobileStoreAppStore = (*fakeMobileStoreAppStore)(nil)

func newFakeMobileStoreAppStore() *fakeMobileStoreAppStore {
	return &fakeMobileStoreAppStore{rows: map[mobileAppKey]domain.MobileStoreApp{}}
}

func (f *fakeMobileStoreAppStore) Upsert(_ context.Context, app domain.MobileStoreApp) (domain.MobileStoreApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := mobileAppKey{app.RepositoryID, app.Platform}
	now := time.Now()
	if existing, ok := f.rows[key]; ok {
		app.ID = existing.ID
		app.CreatedAt = existing.CreatedAt
	} else {
		app.ID = uuid.New()
		app.CreatedAt = now
	}
	app.UpdatedAt = now
	f.rows[key] = app
	return app, nil
}

// SetTracks mirrors the postgres UPDATE exactly: it re-reads the stored row
// and writes only the two cache columns onto it, so a test that hands it a
// stale copy of the row proves the same thing the real column list does.
func (f *fakeMobileStoreAppStore) SetTracks(_ context.Context, repositoryID uuid.UUID, platform string, tracks domain.StoreTracks, syncedAt time.Time) (domain.MobileStoreApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := mobileAppKey{repositoryID, platform}
	app, ok := f.rows[key]
	if !ok {
		return domain.MobileStoreApp{}, fmt.Errorf("set mobile store app tracks: %w", port.ErrNotFound)
	}
	app.Tracks = tracks
	app.TracksSyncedAt = &syncedAt
	app.UpdatedAt = time.Now()
	f.rows[key] = app
	return app, nil
}

func (f *fakeMobileStoreAppStore) Get(_ context.Context, repositoryID uuid.UUID, platform string) (domain.MobileStoreApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	app, ok := f.rows[mobileAppKey{repositoryID, platform}]
	if !ok {
		return domain.MobileStoreApp{}, fmt.Errorf("get mobile store app: %w", port.ErrNotFound)
	}
	return app, nil
}

func (f *fakeMobileStoreAppStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.MobileStoreApp
	for key, app := range f.rows {
		if key.repositoryID == repositoryID {
			out = append(out, app)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Platform < out[j].Platform })
	return out, nil
}

func (f *fakeMobileStoreAppStore) ListAll(_ context.Context) ([]domain.MobileStoreApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.MobileStoreApp
	for _, app := range f.rows {
		out = append(out, app)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RepositoryID != out[j].RepositoryID {
			return out[i].RepositoryID.String() < out[j].RepositoryID.String()
		}
		return out[i].Platform < out[j].Platform
	})
	return out, nil
}

// fakeSigningAssetStore is an in-memory port.SigningAssetStore, keyed on
// (kind, identifier) like the postgres store's unique constraint. Task 6
// does not exercise signing, but Task 7 needs this fake ready to go.
type fakeSigningAssetStore struct {
	mu   sync.Mutex
	rows map[signingAssetKey]domain.SigningAsset
}

type signingAssetKey struct {
	kind       string
	identifier string
}

var _ port.SigningAssetStore = (*fakeSigningAssetStore)(nil)

func newFakeSigningAssetStore() *fakeSigningAssetStore {
	return &fakeSigningAssetStore{rows: map[signingAssetKey]domain.SigningAsset{}}
}

func (f *fakeSigningAssetStore) Upsert(_ context.Context, asset domain.SigningAsset) (domain.SigningAsset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := signingAssetKey{asset.Kind, asset.Identifier}
	now := time.Now()
	if existing, ok := f.rows[key]; ok {
		asset.ID = existing.ID
		asset.CreatedAt = existing.CreatedAt
	} else {
		asset.ID = uuid.New()
		asset.CreatedAt = now
	}
	asset.UpdatedAt = now
	f.rows[key] = asset
	return asset, nil
}

func (f *fakeSigningAssetStore) Get(_ context.Context, kind, identifier string) (domain.SigningAsset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	asset, ok := f.rows[signingAssetKey{kind, identifier}]
	if !ok {
		return domain.SigningAsset{}, fmt.Errorf("get signing asset: %w", port.ErrNotFound)
	}
	return asset, nil
}

func (f *fakeSigningAssetStore) ListExpiring(_ context.Context, before time.Time) ([]domain.SigningAsset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.SigningAsset
	for _, asset := range f.rows {
		if asset.ExpiresAt != nil && asset.ExpiresAt.Before(before) {
			out = append(out, asset)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresAt.Before(*out[j].ExpiresAt) })
	return out, nil
}

// fakeASC is a settable, call-recording port.AppStoreClient. Every method
// returns its canned "…Result"/"…Err" field (zero value by default) and
// appends its arguments to the matching "…Calls" slice, so a test can both
// script a scenario and assert exactly what the service asked for.
type fakeASC struct {
	mu sync.Mutex

	ValidateAuthErr error

	AppByBundleIDID    string
	AppByBundleIDFound bool
	AppByBundleIDErr   error

	EnsureBundleIDErr error

	CreateCertificateResult port.StoreCert
	CreateCertificateErr    error

	CreateProfileResult port.StoreProfile
	CreateProfileErr    error

	LatestVersionResult port.AppStoreVersionInfo
	LatestVersionErr    error

	SubmitForReviewErr error
	ReleaseVersionErr  error

	ListAppsResult []port.StoreAppRef
	ListAppsErr    error

	TracksResult domain.StoreTracks
	TracksErr    error

	PromoteChannelErr error

	ValidateAuthCalls      int
	AppByBundleIDCalls     []string
	EnsureBundleIDCalls    [][2]string // [bundleID, name]
	CreateCertificateCalls [][]byte
	CreateProfileCalls     [][3]string // [bundleID, certID, name]
	LatestVersionCalls     []string
	SubmitForReviewCalls   [][2]string // [appID, version]
	ReleaseVersionCalls    []string
	ListAppsCalls          int
	TracksCalls            []string
	PromoteChannelCalls    [][3]string // [appID, from, to]

	// submits/releases are Task 9's plain call counters — release_test.go
	// asserts against these directly ("submitted to Apple despite a failed
	// confirmation") rather than the fuller …Calls slices above, which stay
	// around for the signing/onboarding tests that already use them.
	submits  int
	releases int
}

var _ port.AppStoreClient = (*fakeASC)(nil)

func (f *fakeASC) ValidateAuth(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ValidateAuthCalls++
	return f.ValidateAuthErr
}

func (f *fakeASC) AppByBundleID(_ context.Context, bundleID string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AppByBundleIDCalls = append(f.AppByBundleIDCalls, bundleID)
	return f.AppByBundleIDID, f.AppByBundleIDFound, f.AppByBundleIDErr
}

func (f *fakeASC) EnsureBundleID(_ context.Context, bundleID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.EnsureBundleIDCalls = append(f.EnsureBundleIDCalls, [2]string{bundleID, name})
	return f.EnsureBundleIDErr
}

func (f *fakeASC) CreateCertificate(_ context.Context, csrPEM []byte) (port.StoreCert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CreateCertificateCalls = append(f.CreateCertificateCalls, append([]byte(nil), csrPEM...))
	return f.CreateCertificateResult, f.CreateCertificateErr
}

func (f *fakeASC) CreateProfile(_ context.Context, bundleID, certID, name string) (port.StoreProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CreateProfileCalls = append(f.CreateProfileCalls, [3]string{bundleID, certID, name})
	return f.CreateProfileResult, f.CreateProfileErr
}

func (f *fakeASC) LatestVersion(_ context.Context, appID string) (port.AppStoreVersionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LatestVersionCalls = append(f.LatestVersionCalls, appID)
	return f.LatestVersionResult, f.LatestVersionErr
}

func (f *fakeASC) SubmitForReview(_ context.Context, appID, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SubmitForReviewCalls = append(f.SubmitForReviewCalls, [2]string{appID, version})
	f.submits++
	return f.SubmitForReviewErr
}

func (f *fakeASC) ReleaseVersion(_ context.Context, appID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ReleaseVersionCalls = append(f.ReleaseVersionCalls, appID)
	f.releases++
	return f.ReleaseVersionErr
}

func (f *fakeASC) ListApps(context.Context) ([]port.StoreAppRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ListAppsCalls++
	return f.ListAppsResult, f.ListAppsErr
}

func (f *fakeASC) Tracks(_ context.Context, appID string) (domain.StoreTracks, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TracksCalls = append(f.TracksCalls, appID)
	return f.TracksResult, f.TracksErr
}

func (f *fakeASC) PromoteChannel(_ context.Context, appID, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PromoteChannelCalls = append(f.PromoteChannelCalls, [3]string{appID, from, to})
	return f.PromoteChannelErr
}

// newFakeASCFactory returns a Deps.NewASC-shaped factory that always hands
// back client, regardless of the credential it's called with — or err, when
// set, to simulate a credential that fails to even build a client (e.g. a
// malformed p8 key), as distinct from one that builds but fails ValidateAuth.
func newFakeASCFactory(client *fakeASC, err error) func(domain.StoreCredential) (port.AppStoreClient, error) {
	return func(domain.StoreCredential) (port.AppStoreClient, error) {
		if err != nil {
			return nil, err
		}
		return client, nil
	}
}

// fakePlay is a settable, call-recording port.GooglePlayClient — the Play
// counterpart of fakeASC.
type fakePlay struct {
	mu sync.Mutex

	ValidateAuthErr error

	AppExistsResult bool
	AppExistsErr    error

	TrackInfoResult port.PlayTrackInfo
	TrackInfoErr    error

	ListAppsResult []port.StoreAppRef
	ListAppsErr    error

	TracksResult domain.StoreTracks
	TracksErr    error
	// TracksHook runs inside Tracks, standing in for whatever else wrote the
	// row while this store call was in flight.
	TracksHook func()

	PromoteTrackErr       error
	SetRolloutFractionErr error
	HaltRolloutErr        error
	ResumeRolloutErr      error

	ValidateAuthCalls int
	AppExistsCalls    []string
	TrackInfoCalls    [][2]string // [packageName, track]
	ListAppsCalls     int
	TracksCalls       []string

	PromoteTrackCalls       []promoteTrackCall
	SetRolloutFractionCalls []setRolloutFractionCall
	HaltRolloutCalls        [][2]string // [packageName, track]
	ResumeRolloutCalls      [][2]string // [packageName, track]
}

type promoteTrackCall struct {
	PackageName  string
	FromTrack    string
	ToTrack      string
	UserFraction float64
}

type setRolloutFractionCall struct {
	PackageName  string
	Track        string
	UserFraction float64
}

var _ port.GooglePlayClient = (*fakePlay)(nil)

func (f *fakePlay) ValidateAuth(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ValidateAuthCalls++
	return f.ValidateAuthErr
}

func (f *fakePlay) AppExists(_ context.Context, packageName string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AppExistsCalls = append(f.AppExistsCalls, packageName)
	return f.AppExistsResult, f.AppExistsErr
}

func (f *fakePlay) TrackInfo(_ context.Context, packageName, track string) (port.PlayTrackInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TrackInfoCalls = append(f.TrackInfoCalls, [2]string{packageName, track})
	return f.TrackInfoResult, f.TrackInfoErr
}

func (f *fakePlay) PromoteTrack(_ context.Context, packageName, fromTrack, toTrack string, userFraction float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PromoteTrackCalls = append(f.PromoteTrackCalls, promoteTrackCall{
		PackageName: packageName, FromTrack: fromTrack, ToTrack: toTrack, UserFraction: userFraction,
	})
	return f.PromoteTrackErr
}

func (f *fakePlay) SetRolloutFraction(_ context.Context, packageName, track string, userFraction float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SetRolloutFractionCalls = append(f.SetRolloutFractionCalls, setRolloutFractionCall{
		PackageName: packageName, Track: track, UserFraction: userFraction,
	})
	return f.SetRolloutFractionErr
}

func (f *fakePlay) HaltRollout(_ context.Context, packageName, track string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.HaltRolloutCalls = append(f.HaltRolloutCalls, [2]string{packageName, track})
	return f.HaltRolloutErr
}

func (f *fakePlay) ResumeRollout(_ context.Context, packageName, track string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ResumeRolloutCalls = append(f.ResumeRolloutCalls, [2]string{packageName, track})
	return f.ResumeRolloutErr
}

func (f *fakePlay) ListApps(context.Context) ([]port.StoreAppRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ListAppsCalls++
	return f.ListAppsResult, f.ListAppsErr
}

func (f *fakePlay) Tracks(_ context.Context, packageName string) (domain.StoreTracks, error) {
	// Run before the lock and before the answer: it stands in for whatever
	// else touched the row while this (real, slow) store call was in flight,
	// which is the collision the narrow SetTracks writer exists for.
	if f.TracksHook != nil {
		f.TracksHook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.TracksCalls = append(f.TracksCalls, packageName)
	return f.TracksResult, f.TracksErr
}

// newFakePlayFactory is the fakePlay counterpart of newFakeASCFactory.
func newFakePlayFactory(client *fakePlay, err error) func(domain.StoreCredential) (port.GooglePlayClient, error) {
	return func(domain.StoreCredential) (port.GooglePlayClient, error) {
		if err != nil {
			return nil, err
		}
		return client, nil
	}
}

// fakePushSecret is a recording Deps.PushSecret: it never talks to GitHub,
// just remembers every call so a test can assert what would have been
// pushed.
type fakePushSecret struct {
	mu  sync.Mutex
	Err error
	// ErrFor fails the push for specific repositories only, leaving the
	// others working — how a test proves one repo's failure cannot abort a
	// sweep over many.
	ErrFor map[uuid.UUID]error
	Calls  []pushSecretCall
}

type pushSecretCall struct {
	RepositoryID uuid.UUID
	Name         string
	Value        string
}

func (f *fakePushSecret) Push(_ context.Context, repositoryID uuid.UUID, name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, pushSecretCall{RepositoryID: repositoryID, Name: name, Value: value})
	if err, ok := f.ErrFor[repositoryID]; ok {
		return err
	}
	return f.Err
}

// fakeTaskCreator is a recording storeops.TaskCreator: it never talks to the
// board, just remembers every CreateTask call and hands back a task carrying
// a fresh ID and the request's title/description/column/priority — enough
// for a test to both script a failure and assert exactly what onboarding
// asked the board to open.
type fakeTaskCreator struct {
	mu    sync.Mutex
	Err   error
	Calls []taskCreatorCall
}

type taskCreatorCall struct {
	RepositoryID uuid.UUID
	Request      domain.CreateBoardTaskRequest
}

var _ storeops.TaskCreator = (*fakeTaskCreator)(nil)

func newFakeTaskCreator() *fakeTaskCreator {
	return &fakeTaskCreator{}
}

func (f *fakeTaskCreator) CreateTask(_ context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, taskCreatorCall{RepositoryID: repositoryID, Request: req})
	if f.Err != nil {
		return domain.BoardTask{}, f.Err
	}
	return domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		Title:        req.Title,
		TaskType:     req.TaskType,
		Description:  req.Description,
		Column:       req.Column,
		Priority:     req.Priority,
		CreatedBy:    req.CreatedBy,
	}, nil
}

// count is a test convenience for asserting "exactly one task was opened",
// the idempotency contract Onboard must uphold across repeated calls.
func (f *fakeTaskCreator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// fakeRepositoryResolver is a settable storeops.RepositoryResolver keyed by
// repository ID — the fallback path Onboard uses to name a store app record
// when the caller supplies no explicit name.
type fakeRepositoryResolver struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.Repository
}

var _ storeops.RepositoryResolver = (*fakeRepositoryResolver)(nil)

func newFakeRepositoryResolver() *fakeRepositoryResolver {
	return &fakeRepositoryResolver{rows: map[uuid.UUID]domain.Repository{}}
}

func (f *fakeRepositoryResolver) set(repo domain.Repository) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[repo.ID] = repo
}

func (f *fakeRepositoryResolver) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	repo, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("get repository: %w", port.ErrNotFound)
	}
	return repo, nil
}

// List returns every repository set on the fixture, sorted by ID for a
// deterministic AllApps join in tests — Task 9's addition to
// storeops.RepositoryResolver.
func (f *fakeRepositoryResolver) List(_ context.Context) ([]domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Repository, 0, len(f.rows))
	for _, repo := range f.rows {
		out = append(out, repo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

// fakeCommenter is a recording storeops.Commenter — the Task 8 counterpart
// of fakePushSecret, so a test can assert exactly which system comments
// verification posted (or script a failure without touching the board).
type fakeCommenter struct {
	mu    sync.Mutex
	Err   error
	Calls []commenterCall
}

type commenterCall struct {
	RepositoryID uuid.UUID
	TaskID       uuid.UUID
	Request      domain.CreateTaskCommentRequest
}

var _ storeops.Commenter = (*fakeCommenter)(nil)

func newFakeCommenter() *fakeCommenter {
	return &fakeCommenter{}
}

func (f *fakeCommenter) AddComment(_ context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, commenterCall{RepositoryID: repositoryID, TaskID: taskID, Request: req})
	if f.Err != nil {
		return domain.TaskComment{}, f.Err
	}
	return domain.TaskComment{
		ID:         uuid.New(),
		TaskID:     taskID,
		AuthorType: req.AuthorType,
		AuthorID:   req.AuthorID,
		Content:    req.Content,
	}, nil
}

// fakeIncidentIngester is a recording storeops.IncidentIngester — the Task 9
// counterpart of fakeCommenter, so a test can assert exactly which incidents
// the monitor ingested (or script a failure without touching the database).
type fakeIncidentIngester struct {
	mu    sync.Mutex
	Err   error
	Calls []domain.IncidentInput

	// arrived/gate let a test hold an in-flight ingest open so a second,
	// concurrent Sweep is guaranteed to overlap with it — the only way to
	// exercise the monitor's fingerprint claim deterministically. arrived is
	// buffered so an unexpected extra arrival (i.e. the bug) never deadlocks
	// the test, it just shows up as a second recorded call.
	arrived chan struct{}
	gate    chan struct{}
}

var _ storeops.IncidentIngester = (*fakeIncidentIngester)(nil)

func newFakeIncidentIngester() *fakeIncidentIngester {
	return &fakeIncidentIngester{}
}

// block installs the arrived/gate pair and returns them. Callers must close
// the gate to let every blocked ingest through.
func (f *fakeIncidentIngester) block() (arrived <-chan struct{}, release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.arrived = make(chan struct{}, 8)
	f.gate = make(chan struct{})
	gate := f.gate
	return f.arrived, func() { close(gate) }
}

func (f *fakeIncidentIngester) Ingest(_ context.Context, in domain.IncidentInput) (domain.Incident, error) {
	f.mu.Lock()
	arrived, gate := f.arrived, f.gate
	f.mu.Unlock()
	if arrived != nil {
		arrived <- struct{}{}
		<-gate
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, in)
	if f.Err != nil {
		return domain.Incident{}, f.Err
	}
	return domain.Incident{ID: uuid.New(), Fingerprint: in.Fingerprint}, nil
}

// fakeOpsAuditStore is a recording port.OpsAuditStore — Task 9's fixture for
// asserting that every store release action attempt, refused or not, writes
// exactly one ops_audit_log row.
type fakeOpsAuditStore struct {
	mu      sync.Mutex
	Err     error
	entries []domain.OpsAuditEntry
}

var _ port.OpsAuditStore = (*fakeOpsAuditStore)(nil)

func newFakeOpsAuditStore() *fakeOpsAuditStore {
	return &fakeOpsAuditStore{}
}

func (f *fakeOpsAuditStore) Log(_ context.Context, entry domain.OpsAuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	entry.ID = uuid.New()
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeOpsAuditStore) List(_ context.Context, repositoryID *uuid.UUID, limit int) ([]domain.OpsAuditEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.OpsAuditEntry
	for i := len(f.entries) - 1; i >= 0; i-- {
		e := f.entries[i]
		if repositoryID != nil && (e.RepositoryID == nil || *e.RepositoryID != *repositoryID) {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// fakeReleaseParker is a recording storeops.ReleaseParker — the fixture that
// proves a release with no available engine actually parks the card instead of
// only failing an API call.
type fakeReleaseParker struct {
	mu    sync.Mutex
	Err   error
	Calls []releaseParkCall
}

type releaseParkCall struct {
	RepositoryID uuid.UUID
	TaskID       uuid.UUID
	Resource     string
	Detail       string
}

var _ storeops.ReleaseParker = (*fakeReleaseParker)(nil)

func (f *fakeReleaseParker) BlockOnResource(_ context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, releaseParkCall{RepositoryID: repositoryID, TaskID: taskID, Resource: resource, Detail: detail})
	if f.Err != nil {
		return "", f.Err
	}
	return domain.TaskColumnBlocked, nil
}
