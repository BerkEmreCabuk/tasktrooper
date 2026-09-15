// Package storeops owns the store console credential vault and the per-repo
// mobile app registry — the shared foundation signing (Task 7), onboarding
// (Task 8) and the release monitor (Task 9) build on.
package storeops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TaskCreator opens the board task that carries store onboarding / signing
// work. Same shape as deploy.TaskCreator, declared locally so this package
// doesn't import application/deploy.
type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
}

// RepositoryResolver reads the repository a store app belongs to (Get, same
// shape as deploy.RepositoryResolver) and lists every repository (List) —
// the join AllApps (Task 9) needs to name each cross-repository app row
// without a per-row lookup.
type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	List(ctx context.Context) ([]domain.Repository, error)
}

// Commenter posts a system comment on a board task. Same shape prodops uses
// for AddComment.
type Commenter interface {
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

// Deps wires the collaborators of the store operations service.
type Deps struct {
	Credentials port.StoreCredentialStore
	Apps        port.MobileStoreAppStore
	Signing     port.SigningAssetStore
	Cipher      *secrets.Cipher
	// Client factories — swap for fakes in tests.
	NewASC  func(domain.StoreCredential) (port.AppStoreClient, error)
	NewPlay func(domain.StoreCredential) (port.GooglePlayClient, error)
	// PushSecret writes one GitHub Actions repo secret (Task 3 wired in runtime).
	PushSecret func(ctx context.Context, repositoryID uuid.UUID, name, value string) error
	Repos      RepositoryResolver
	Tasks      TaskCreator
	Comments   Commenter
}

// Service owns the encrypted store console credentials and the mobile app
// registry. Signing, onboarding and monitoring (Tasks 7-9) are built on top
// of it in this same package.
type Service struct {
	credentials port.StoreCredentialStore
	apps        port.MobileStoreAppStore
	signing     port.SigningAssetStore
	cipher      *secrets.Cipher
	newASC      func(domain.StoreCredential) (port.AppStoreClient, error)
	newPlay     func(domain.StoreCredential) (port.GooglePlayClient, error)
	pushSecret  func(ctx context.Context, repositoryID uuid.UUID, name, value string) error
	repos       RepositoryResolver
	tasks       TaskCreator
	comments    Commenter
	// audit is late-set via SetAuditor (release.go) — the ops_audit_log
	// sink for store release actions (submit, release, promote, rollout,
	// halt, resume). Nil (the pre-wiring default) makes recording an audit
	// entry a no-op rather than a hard failure.
	audit port.OpsAuditStore
	// The release-engine collaborators, all late-set via SetEngineProbes /
	// SetReleaseStarter / SetReleaseParker (engine.go). Nil is a working
	// deployment that simply cannot start a release: ResolveEngine reports
	// the engine unavailable instead of panicking.
	actionsProbe ActionsProbe
	localProbe   LocalRunnerProbe
	startRelease ReleaseStarter
	parker       ReleaseParker

	mu sync.Mutex
	// pendingPush holds the renewal targets whose signing assets were minted
	// but whose GitHub secret push has not succeeded yet. See
	// markPushPending in signing.go for why this exists and why it is
	// in-memory.
	pendingPush map[renewTarget]bool
}

func NewService(d Deps) *Service {
	return &Service{
		credentials: d.Credentials,
		apps:        d.Apps,
		signing:     d.Signing,
		cipher:      d.Cipher,
		newASC:      d.NewASC,
		newPlay:     d.NewPlay,
		pushSecret:  d.PushSecret,
		repos:       d.Repos,
		tasks:       d.Tasks,
		comments:    d.Comments,
	}
}

// CredentialView is the safe-to-list projection of a stored credential:
// never the payload (key material, service account JSON), only whether a
// provider is configured and when it was last written. UpdatedAt is the zero
// time (never omitted — a time.Time never omits under omitempty anyway) for
// a known provider that has no stored row yet.
type CredentialView struct {
	Provider   string    `json:"provider"`
	Configured bool      `json:"configured"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// knownCredentialProviders is every store console this system can save a
// credential for. Credentials() always reports one view per entry here — a
// declared-but-unsaved provider must be distinguishable from an unknown one.
var knownCredentialProviders = []string{domain.StoreCredentialASC, domain.StoreCredentialGooglePlay}

// ErrInvalidCredential wraps every SaveCredential/DeleteCredential failure
// caused by the caller's own input — an unknown provider, or a credential
// the store console itself rejects (bad key material, ValidateAuth
// failure) — as opposed to an infra/config failure (cipher not configured,
// client factory not wired, encrypt/persist error). The HTTP layer checks
// errors.Is(err, ErrInvalidCredential) to choose 400 vs 500, the same
// sentinel-dispatch idiom handler_mcp.go already uses for domain.ErrMCP*.
var ErrInvalidCredential = errors.New("storeops: invalid credential")

// ErrInvalidPlatform marks a caller-supplied mobile store platform that is
// not ios or android — a 400, not a 500, at the HTTP edge.
var ErrInvalidPlatform = errors.New("storeops: unsupported mobile store platform")

// ErrIdentifierLocked marks an attempt to re-point an already-live store app
// at a different bundle ID / package name. That is a different app, not an
// edit, and the lifecycle has no way back out of live — so the save is
// refused rather than leaving a `live` row pointing somewhere unpublished.
var ErrIdentifierLocked = errors.New("storeops: a live store app's identifier cannot be changed")

// validProvider reports whether provider is a known store credential
// provider (domain.StoreCredentialASC / domain.StoreCredentialGooglePlay).
func validProvider(provider string) bool {
	return provider == domain.StoreCredentialASC || provider == domain.StoreCredentialGooglePlay
}

// validPlatform reports whether platform is a mobile store platform this
// system tracks (domain.MobileStorePlatformIOS / ...Android).
func validPlatform(platform string) bool {
	return platform == domain.MobileStorePlatformIOS || platform == domain.MobileStorePlatformAndroid
}

// SaveCredential validates data against the store console it authenticates
// (App Store Connect or Google Play) via the client's ValidateAuth before
// persisting anything. An invalid credential returns an error and nothing is
// stored — the vault never holds a credential that hasn't been confirmed to
// actually work. The payload is encrypted before it touches the store; never
// logged, here or anywhere data flows through this method.
func (s *Service) SaveCredential(ctx context.Context, provider string, data map[string]string) error {
	if !validProvider(provider) {
		return fmt.Errorf("storeops: unknown credential provider %q: %w", provider, ErrInvalidCredential)
	}
	if s.cipher == nil {
		return errors.New("storeops: secrets cipher not configured")
	}
	cred := domain.StoreCredential{Provider: provider, Data: data}

	switch provider {
	case domain.StoreCredentialASC:
		if s.newASC == nil {
			return errors.New("storeops: App Store Connect client factory not configured")
		}
		client, err := s.newASC(cred)
		if err != nil {
			return fmt.Errorf("storeops: building App Store Connect client: %w: %w", err, ErrInvalidCredential)
		}
		if err := client.ValidateAuth(ctx); err != nil {
			return fmt.Errorf("storeops: App Store Connect credential failed validation: %w: %w", err, ErrInvalidCredential)
		}
	case domain.StoreCredentialGooglePlay:
		if s.newPlay == nil {
			return errors.New("storeops: Google Play client factory not configured")
		}
		client, err := s.newPlay(cred)
		if err != nil {
			return fmt.Errorf("storeops: building Google Play client: %w: %w", err, ErrInvalidCredential)
		}
		if err := client.ValidateAuth(ctx); err != nil {
			return fmt.Errorf("storeops: Google Play credential failed validation: %w: %w", err, ErrInvalidCredential)
		}
	}

	plaintext, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("storeops: encoding credential: %w", err)
	}
	encrypted, err := s.cipher.Encrypt(string(plaintext))
	if err != nil {
		return fmt.Errorf("storeops: encrypting credential: %w", err)
	}
	if err := s.credentials.Set(ctx, provider, encrypted); err != nil {
		return fmt.Errorf("storeops: persisting credential: %w", err)
	}
	return nil
}

// Credentials lists one view per KNOWN provider — never only the ones with a
// stored row — without ever returning a payload: safe to hand straight to an
// API response. A provider with no stored row comes back Configured:false
// with a zero UpdatedAt, so the caller can tell "declared but not yet saved"
// apart from "saved".
func (s *Service) Credentials(ctx context.Context) ([]CredentialView, error) {
	listed, err := s.credentials.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing credentials: %w", err)
	}
	views := make([]CredentialView, 0, len(knownCredentialProviders))
	for _, provider := range knownCredentialProviders {
		updatedAt, configured := listed[provider]
		views = append(views, CredentialView{Provider: provider, Configured: configured, UpdatedAt: updatedAt})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Provider < views[j].Provider })
	return views, nil
}

// DeleteCredential removes a provider's stored credential.
func (s *Service) DeleteCredential(ctx context.Context, provider string) error {
	if !validProvider(provider) {
		return fmt.Errorf("storeops: unknown credential provider %q: %w", provider, ErrInvalidCredential)
	}
	if err := s.credentials.Delete(ctx, provider); err != nil {
		return fmt.Errorf("storeops: deleting %s credential: %w", provider, err)
	}
	return nil
}

// ErrProviderNotConnected means nobody has saved a credential for this
// provider yet. It is the first state every install is in, not a failure, and
// it is answered differently from ErrAppListingUnavailable: that one means the
// credential is there but the store will not enumerate, which is what opens
// the manual identifier field. Typing an identifier by hand is useless with no
// credential to verify it against, so the two must not collapse into one
// "listing failed" screen.
var ErrProviderNotConnected = errors.New("storeops: this provider is not connected yet")

// notConnected re-labels the not-found a missing credential row surfaces as.
// Anything else is passed through untouched — a cipher that is not configured
// and a decrypt that fails are real faults and have to stay 500s.
func notConnected(provider string, err error) error {
	if errors.Is(err, port.ErrNotFound) {
		return fmt.Errorf("storeops: %s: %w", provider, ErrProviderNotConnected)
	}
	return err
}

// credential loads and decrypts the stored credential for provider. It never
// logs the decrypted payload — callers must not either.
func (s *Service) credential(ctx context.Context, provider string) (domain.StoreCredential, error) {
	if s.cipher == nil {
		return domain.StoreCredential{}, errors.New("storeops: secrets cipher not configured")
	}
	encrypted, updatedAt, err := s.credentials.Get(ctx, provider)
	if err != nil {
		return domain.StoreCredential{}, fmt.Errorf("storeops: loading %s credential: %w", provider, err)
	}
	plaintext, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return domain.StoreCredential{}, fmt.Errorf("storeops: decrypting %s credential: %w", provider, err)
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(plaintext), &data); err != nil {
		return domain.StoreCredential{}, fmt.Errorf("storeops: decoding %s credential: %w", provider, err)
	}
	return domain.StoreCredential{Provider: provider, Data: data, UpdatedAt: updatedAt}, nil
}

// asc builds an App Store Connect client from the stored ASC credential —
// the entry point signing and monitoring (Tasks 7, 9) use to reach ASC.
func (s *Service) asc(ctx context.Context) (port.AppStoreClient, error) {
	cred, err := s.credential(ctx, domain.StoreCredentialASC)
	if err != nil {
		return nil, err
	}
	if s.newASC == nil {
		return nil, errors.New("storeops: App Store Connect client factory not configured")
	}
	return s.newASC(cred)
}

// play builds a Google Play client from the stored Google Play credential —
// the entry point signing and monitoring (Tasks 7, 9) use to reach Play.
func (s *Service) play(ctx context.Context) (port.GooglePlayClient, error) {
	cred, err := s.credential(ctx, domain.StoreCredentialGooglePlay)
	if err != nil {
		return nil, err
	}
	if s.newPlay == nil {
		return nil, errors.New("storeops: Google Play client factory not configured")
	}
	return s.newPlay(cred)
}

// AppsByRepository returns the mobile store registry rows for a repository
// (one per platform it ships to).
func (s *Service) AppsByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error) {
	apps, err := s.apps.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing store apps: %w", err)
	}
	return apps, nil
}

// ErrAppNotInStore marks a link request naming an app the store console does
// not have. It is the caller's own mistake — a typo'd bundle ID, an app that
// was deleted between listing and picking — so it is a 400, and the binding is
// refused rather than written and left to fail at the first upload.
var ErrAppNotInStore = errors.New("storeops: the store console has no app with this identifier")

// ListStoreApps enumerates the apps provider's saved credential can see, for
// the picker that binds a repository to one of them.
//
// A wrapped port.ErrAppListingUnavailable is passed straight through and stays
// matchable: it is an ANSWER, not a failure — the Play Developer API has no
// listing endpoint and a service account may not reach the Reporting API that
// does. The HTTP layer turns it into a 200 saying so, because the fallback is
// for the operator to type the identifier by hand, and a 5xx would tell them
// to retry something that will never start working on its own.
func (s *Service) ListStoreApps(ctx context.Context, provider string) ([]port.StoreAppRef, error) {
	if !validProvider(provider) {
		return nil, fmt.Errorf("storeops: unknown credential provider %q: %w", provider, ErrInvalidCredential)
	}
	var apps []port.StoreAppRef
	switch provider {
	case domain.StoreCredentialASC:
		client, err := s.ascClient(ctx)
		if err != nil {
			return nil, notConnected(provider, err)
		}
		apps, err = client.ListApps(ctx)
		if err != nil {
			return nil, fmt.Errorf("storeops: listing App Store Connect apps: %w", err)
		}
	default:
		client, err := s.playClient(ctx)
		if err != nil {
			return nil, notConnected(provider, err)
		}
		apps, err = client.ListApps(ctx)
		if err != nil {
			return nil, fmt.Errorf("storeops: listing Google Play apps: %w", err)
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Identifier < apps[j].Identifier })
	return apps, nil
}

// LinkStoreApp binds a repository's platform row to one app in the store
// console, writing the identifier, the store's own app id and its display
// name. The app is confirmed to exist first (AppByBundleID / AppExists): a
// binding the store cannot resolve is a binding every later upload fails on.
//
// It deliberately does NOT touch State. Binding is an identity statement;
// whether the app is onboarded, test-ready or live is what Onboard and the
// monitor decide from the store itself, and a link that moved the row forward
// would let mobileStoreGate green-light a deploy nobody verified.
func (s *Service) LinkStoreApp(ctx context.Context, repositoryID uuid.UUID, platform string, ref port.StoreAppRef) (domain.MobileStoreApp, error) {
	if !validPlatform(platform) {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: linking a store app: unsupported platform %q: %w", platform, ErrInvalidPlatform)
	}
	identifier := strings.TrimSpace(ref.Identifier)
	if identifier == "" {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: linking a store app needs an identifier: %w", ErrAppNotInStore)
	}

	app, err := s.apps.Get(ctx, repositoryID, platform)
	switch {
	case err == nil:
	case errors.Is(err, port.ErrNotFound):
		app = domain.MobileStoreApp{
			RepositoryID: repositoryID,
			Platform:     platform,
			State:        domain.MobileStoreStateUnregistered,
		}
	default:
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: loading store app: %w", err)
	}
	if err := checkIdentifierChange(app, platform, identifier); err != nil {
		return domain.MobileStoreApp{}, err
	}

	storeAppID := strings.TrimSpace(ref.StoreAppID)
	switch platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.ascClient(ctx)
		if err != nil {
			return domain.MobileStoreApp{}, err
		}
		confirmedID, found, err := client.AppByBundleID(ctx, identifier)
		if err != nil {
			return domain.MobileStoreApp{}, fmt.Errorf("storeops: looking up %s in App Store Connect: %w", identifier, err)
		}
		if !found {
			return domain.MobileStoreApp{}, fmt.Errorf("storeops: App Store Connect has no app for %s: %w", identifier, ErrAppNotInStore)
		}
		// ASC's own answer wins over whatever the picker sent: the resource id
		// is what every later call is addressed with, and a stale one from a
		// cached listing would address the wrong app.
		storeAppID = confirmedID
	case domain.MobileStorePlatformAndroid:
		client, err := s.playClient(ctx)
		if err != nil {
			return domain.MobileStoreApp{}, err
		}
		exists, err := client.AppExists(ctx, identifier)
		if err != nil {
			return domain.MobileStoreApp{}, fmt.Errorf("storeops: looking up %s in Google Play: %w", identifier, err)
		}
		if !exists {
			return domain.MobileStoreApp{}, fmt.Errorf("storeops: Google Play has no app for %s: %w", identifier, ErrAppNotInStore)
		}
	}

	app.Identifier = identifier
	app.StoreAppID = storeAppID
	if name := strings.TrimSpace(ref.Name); name != "" {
		// An empty name leaves the recorded one alone: the picker's listing is
		// the only source for it, and a hand-typed identifier carries none.
		app.AppName = name
	}

	stored, err := s.apps.Upsert(ctx, app)
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: persisting the store app link: %w", err)
	}
	return stored, nil
}

// Tracks reads the app's three channels from the store console and refreshes
// the row's cache (Tracks / TracksSyncedAt) with what came back. The read is
// live because a channel view that lied about where a build sits is worse than
// a slow one; the cache exists so the panel can render without waiting for it.
//
// The cache write is the narrow SetTracks rather than an Upsert of the row
// this method loaded: the store round-trip in between is long enough for a
// prod deploy or the monitor to have advanced the lifecycle, and writing the
// whole pre-read copy back would undo it (see
// port.MobileStoreAppStore.SetTracks).
func (s *Service) Tracks(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.StoreTracks, error) {
	if !validPlatform(platform) {
		return domain.StoreTracks{}, fmt.Errorf("storeops: reading store tracks: unsupported platform %q: %w", platform, ErrInvalidPlatform)
	}
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		return domain.StoreTracks{}, fmt.Errorf("storeops: loading store app: %w", err)
	}
	tracks, err := s.storeTracks(ctx, app)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	if _, err := s.apps.SetTracks(ctx, repositoryID, platform, tracks, time.Now()); err != nil {
		return domain.StoreTracks{}, fmt.Errorf("storeops: caching store tracks: %w", err)
	}
	return tracks, nil
}

// storeTracks is the client-side half of Tracks, shared with the monitor's
// per-sweep refresh. iOS addresses the app by its ASC resource id, Android by
// the package name — the one place that difference lives.
func (s *Service) storeTracks(ctx context.Context, app domain.MobileStoreApp) (domain.StoreTracks, error) {
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.ascClient(ctx)
		if err != nil {
			return domain.StoreTracks{}, err
		}
		tracks, err := client.Tracks(ctx, app.StoreAppID)
		if err != nil {
			return domain.StoreTracks{}, fmt.Errorf("storeops: reading TestFlight and App Store channels for %s: %w", app.Identifier, err)
		}
		return tracks, nil
	case domain.MobileStorePlatformAndroid:
		client, err := s.playClient(ctx)
		if err != nil {
			return domain.StoreTracks{}, err
		}
		tracks, err := client.Tracks(ctx, app.Identifier)
		if err != nil {
			return domain.StoreTracks{}, fmt.Errorf("storeops: reading Play tracks for %s: %w", app.Identifier, err)
		}
		return tracks, nil
	}
	return domain.StoreTracks{}, fmt.Errorf("storeops: reading store tracks: unsupported platform %q: %w", app.Platform, ErrInvalidPlatform)
}
