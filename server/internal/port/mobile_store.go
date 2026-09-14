package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrNotFound is returned, wrapped, by Get on StoreCredentialStore,
// MobileStoreAppStore, and SigningAssetStore when the requested row does
// not exist. It is the not-found sentinel the application layer checks
// with errors.Is(err, port.ErrNotFound) — so storeops and its callers never
// need to import the database driver just to tell "absent" apart from
// "failed". Each postgres implementation is responsible for wrapping this
// sentinel around whatever its driver actually reports (e.g. pgx.ErrNoRows).
var ErrNotFound = errors.New("not found")

// ErrAppListingUnavailable is returned, wrapped, by ListApps when the
// credential is valid but the store will not enumerate the account's apps.
// It is a real, expected answer rather than a failure: the App Store Connect
// API lists apps directly, but the Play Developer API has no "list my apps"
// endpoint at all — that list comes only from the separate Play Developer
// Reporting API, which a service account may not have been granted. Callers
// fall back to asking for the identifier by hand; they must not present an
// empty list as "this account has no apps".
var ErrAppListingUnavailable = errors.New("store app listing is unavailable for this credential")

// StoreAppRef is one app as the store console knows it — the account-level
// listing a person picks from when a repository is bound to an app. It is
// deliberately not domain.MobileStoreApp: that row is OUR lifecycle state for
// a repository we ship, while this is a remote record we neither own nor
// persist wholesale.
// The json tags are load-bearing: the picker endpoint serialises this type
// straight onto the wire, and the rest of the API is snake_case.
type StoreAppRef struct {
	StoreAppID string `json:"store_app_id"` // ASC app resource id / "" for Play, which keys on the package name
	Identifier string `json:"identifier"`   // bundle ID / package name
	Name       string `json:"name"`         // display name as the console shows it
	// State is the store's own word for how far along the app record is
	// (ASC appStoreState, Play's app status), passed through for display.
	// "" when the store reports none.
	State string `json:"state,omitempty"`
}

// StoreCredentialStore persists encrypted store console credentials.
type StoreCredentialStore interface {
	Set(ctx context.Context, provider string, encrypted []byte) error
	// Get returns an error wrapping ErrNotFound when no credential is
	// stored for provider.
	Get(ctx context.Context, provider string) ([]byte, time.Time, error)
	Delete(ctx context.Context, provider string) error
	// List returns provider -> updated_at; payloads are never listed.
	List(ctx context.Context) (map[string]time.Time, error)
}

// MobileStoreAppStore persists the per-(repository, platform) store lifecycle row.
type MobileStoreAppStore interface {
	// Upsert inserts or updates on (repository_id, platform). It writes the
	// WHOLE row from app, so it belongs to the paths that own the lifecycle
	// (onboarding, go-live, a review verdict, a store binding) and to nothing
	// else — see SetTracks for why.
	Upsert(ctx context.Context, app domain.MobileStoreApp) (domain.MobileStoreApp, error)
	// SetTracks writes ONLY the channel cache (tracks, tracks_synced_at) for
	// one row and returns the row as it stands afterwards.
	//
	// It exists because a track refresh is a cache write that rides a network
	// read: the copy it started from is seconds old by the time it lands, and
	// an Upsert would put every other column of that stale copy back. The
	// collision is real and silent — a store submit recorded by
	// storeops.Service.MarkSubmitted between a sweep's ListAll and its track
	// write would have its review_state erased, and the monitor only polls
	// rows whose review_state is still open, so that release's Apple verdict
	// (approval AND rejection incident) would never be seen again.
	//
	// Returns an error wrapping ErrNotFound when no row exists.
	SetTracks(ctx context.Context, repositoryID uuid.UUID, platform string, tracks domain.StoreTracks, syncedAt time.Time) (domain.MobileStoreApp, error)
	// Get returns an error wrapping ErrNotFound when no row exists for
	// (repositoryID, platform).
	Get(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.MobileStoreApp, error)
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error)
	// ListAll feeds storeops.Monitor.
	ListAll(ctx context.Context) ([]domain.MobileStoreApp, error)
}

// SigningAssetStore persists encrypted signing artifacts.
type SigningAssetStore interface {
	// Upsert inserts or updates on (kind, identifier).
	Upsert(ctx context.Context, asset domain.SigningAsset) (domain.SigningAsset, error)
	// Get returns an error wrapping ErrNotFound when no asset exists for
	// (kind, identifier).
	Get(ctx context.Context, kind, identifier string) (domain.SigningAsset, error)
	ListExpiring(ctx context.Context, before time.Time) ([]domain.SigningAsset, error)
}

// StoreCert is a created distribution certificate.
type StoreCert struct {
	ID        string
	Serial    string
	DER       []byte
	ExpiresAt time.Time
}

// StoreProfile is a created provisioning profile.
type StoreProfile struct {
	ID        string
	Name      string
	Content   []byte // decoded .mobileprovision
	ExpiresAt time.Time
}

// AppStoreVersionInfo is the latest App Store version's review status.
type AppStoreVersionInfo struct {
	Version string
	State   string // raw ASC state e.g. READY_FOR_SALE, WAITING_FOR_REVIEW, IN_REVIEW, REJECTED, PENDING_DEVELOPER_RELEASE
}

// AppStoreClient is what storeops needs from App Store Connect.
type AppStoreClient interface {
	ValidateAuth(ctx context.Context) error
	AppByBundleID(ctx context.Context, bundleID string) (appID string, found bool, err error)
	EnsureBundleID(ctx context.Context, bundleID, name string) error // register if absent, idempotent
	CreateCertificate(ctx context.Context, csrPEM []byte) (StoreCert, error)
	CreateProfile(ctx context.Context, bundleID, certID, name string) (StoreProfile, error)
	LatestVersion(ctx context.Context, appID string) (AppStoreVersionInfo, error)
	// SubmitForReview creates (or reuses an existing editable) App Store
	// version carrying versionString == version and submits it for review.
	SubmitForReview(ctx context.Context, appID, version string) error
	// ReleaseVersion releases the version currently held in
	// PENDING_DEVELOPER_RELEASE. Returns an error if no version is in that
	// state — there is nothing Apple-approved waiting to be released.
	ReleaseVersion(ctx context.Context, appID string) error
	// ListApps enumerates every app the API key can see, for the picker that
	// binds a repository to one of them.
	ListApps(ctx context.Context) ([]StoreAppRef, error)
	// Tracks reads the three channels: TestFlight internal groups, TestFlight
	// external groups (which pass Beta App Review), and the App Store version.
	Tracks(ctx context.Context, appID string) (domain.StoreTracks, error)
	// PromoteChannel moves the build currently on `from` onto `to`. On iOS
	// internal -> external means adding the build to the external beta groups
	// (which queues Beta App Review), and external -> production means
	// submitting that build's version for App Store review.
	PromoteChannel(ctx context.Context, appID, from, to string) error
}

// Deliberately absent: HasTestFlightBuild. The iOS analogue of Play's
// play_first_upload checklist item would be "a build exists in TestFlight" —
// but on iOS that build is produced BY the stage deploy that reaching
// test_ready unlocks, so gating test_ready on it would deadlock onboarding.
// Play needs its manual first upload because the Developer API refuses to
// push to a track before one exists; App Store Connect has no such rule.
// With no honest application-layer use, the method is not in the interface.

// PlayTrackInfo is one track's newest release.
type PlayTrackInfo struct {
	HasRelease   bool
	VersionName  string
	Status       string // completed | inProgress | halted | draft
	UserFraction float64
}

// GooglePlayClient is what storeops needs from the Play Developer API.
type GooglePlayClient interface {
	ValidateAuth(ctx context.Context) error // token exchange only
	AppExists(ctx context.Context, packageName string) (bool, error)
	TrackInfo(ctx context.Context, packageName, track string) (PlayTrackInfo, error) // track: "internal" | "production"
	// PromoteTrack copies fromTrack's current release onto toTrack, staged at
	// userFraction. Errors if fromTrack has no release to promote.
	PromoteTrack(ctx context.Context, packageName, fromTrack, toTrack string, userFraction float64) error
	// SetRolloutFraction dials track's in-flight release to userFraction. A
	// fraction of 1 (or above) completes the rollout to 100% of users.
	SetRolloutFraction(ctx context.Context, packageName, track string, userFraction float64) error
	// HaltRollout stops track's release in place, preserving its current
	// userFraction so ResumeRollout can restore exactly what was rolling out.
	HaltRollout(ctx context.Context, packageName, track string) error
	// ResumeRollout un-halts track's release at the fraction it was halted at.
	ResumeRollout(ctx context.Context, packageName, track string) error
	// ListApps enumerates the apps the service account can see. The Play
	// Developer API cannot do this at all — the implementation goes to the
	// Play Developer Reporting API, and returns ErrAppListingUnavailable when
	// that API is not enabled or the service account lacks access.
	ListApps(ctx context.Context) ([]StoreAppRef, error)
	// Tracks reads the three channels. External folds Play's two testing
	// tracks together: `alpha` (closed testing) and `beta` (open testing) are
	// one product channel here, and the newer of the two is what it reports.
	Tracks(ctx context.Context, packageName string) (domain.StoreTracks, error)
}
