package storeops

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	// defaultMonitorInterval is Start's fallback when the caller passes a
	// non-positive interval — same guard prodops.Monitor.Start applies.
	defaultMonitorInterval = time.Minute
	// signingRenewalWindow is how far ahead of expiry every sweep renews
	// signing assets — the 30-day horizon Task 7's RenewExpiringSigning is
	// built to be called with.
	signingRenewalWindow = 30 * 24 * time.Hour
)

// IncidentIngester is the Ingest side of the incident service, injected so
// the monitor can be tested without a database. Same narrow shape as
// prodops.IncidentIngester, copied locally so this package doesn't import
// application/prodops.
type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

// Monitor polls every registered mobile store app: re-verifying an
// onboarding checklist still in progress, detecting first go-live, polling
// a live app's pending store review, and renewing signing assets ahead of
// expiry. It is storeops' counterpart to prodops.Monitor.
type Monitor struct {
	svc      *Service
	apps     port.MobileStoreAppStore
	ingester IncidentIngester

	mu sync.Mutex
	// notified dedupes halted-rollout incidents. Unlike a rejected review,
	// a halted rollout has no persisted field on MobileStoreApp that flips
	// it out of the poll gate, so — per the Task 9 controller ruling — the
	// fallback is this in-memory fingerprint set. It resets on restart,
	// meaning a halted rollout still active across a restart re-ingests
	// once more; that is judged preferable to a silent, permanent gap.
	notified map[string]bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewMonitor(svc *Service, apps port.MobileStoreAppStore, ingester IncidentIngester) *Monitor {
	return &Monitor{
		svc:      svc,
		apps:     apps,
		ingester: ingester,
		notified: map[string]bool{},
	}
}

// Start runs a sweep every interval until the context is cancelled. Calling
// Start on a running monitor is a no-op. Mirrors prodops.Monitor.Start.
func (m *Monitor) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultMonitorInterval
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// Sweep immediately — waiting a full interval before the first pass
		// leaves a fresh submission or a soon-expiring asset unwatched for
		// no reason.
		m.Sweep(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Sweep(ctx)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("store monitor started")
}

func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

// Sweep runs one pass over every registered mobile store app plus the
// standing signing renewal. It is best-effort: any row's failure is logged
// and the sweep moves on rather than aborting the rest.
func (m *Monitor) Sweep(ctx context.Context) {
	apps, err := m.apps.ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("store monitor: list mobile store apps failed")
	} else {
		for _, app := range apps {
			select {
			case <-ctx.Done():
				return
			default:
			}
			m.sweepApp(ctx, app)
		}
	}

	if err := m.svc.RenewExpiringSigning(ctx, time.Now().Add(signingRenewalWindow)); err != nil {
		log.Warn().Err(err).Msg("store monitor: renew expiring signing assets failed")
	}
}

// sweepApp dispatches one row by its current lifecycle state. A row in
// unregistered has nothing to poll yet and is skipped.
func (m *Monitor) sweepApp(ctx context.Context, app domain.MobileStoreApp) {
	switch app.State {
	case domain.MobileStoreStateOnboarding:
		m.sweepOnboarding(ctx, app)
	case domain.MobileStoreStateTestReady:
		m.sweepTestReady(ctx, m.syncTracks(ctx, app))
	case domain.MobileStoreStateLive:
		m.sweepLive(ctx, m.syncTracks(ctx, app))
	}
}

// syncTracks refreshes the row's channel cache from the store console and
// returns the row to carry on with — the freshly stored one on success, the
// caller's own copy on any failure. It rides this sweep rather than opening a
// loop of its own: the sweep already holds the row and already talks to both
// consoles, and a second timer would double the store API traffic to answer
// the same question.
//
// It runs BEFORE the state-specific sweep, not after: those write the row
// themselves (go-live, review verdict), and a track write landing afterwards
// with a pre-sweep copy would put back the state they had just advanced.
// Ordering only settles the collision INSIDE one sweep, though — the writers
// outside it (Service.MarkSubmitted, Service.Tracks, LinkStoreApp) keep no
// such order, which is why the write below is the narrow
// port.MobileStoreAppStore.SetTracks and not an Upsert of this whole,
// seconds-old copy.
//
// An onboarding row is deliberately not synced — it has no confirmed store app
// to read channels off yet, and every attempt would just log a failure.
func (m *Monitor) syncTracks(ctx context.Context, app domain.MobileStoreApp) domain.MobileStoreApp {
	tracks, err := m.svc.storeTracks(ctx, app)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).Str("platform", app.Platform).
			Msg("store monitor: reading store channels failed")
		return app
	}
	stored, err := m.apps.SetTracks(ctx, app.RepositoryID, app.Platform, tracks, time.Now())
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: caching store channels failed")
		return app
	}
	return stored
}

// sweepOnboarding re-runs the checklist verification Task 8 exposes; that
// method owns advancing the row to test_ready and posting its own
// verification comments once the checklist completes.
func (m *Monitor) sweepOnboarding(ctx context.Context, app domain.MobileStoreApp) {
	if _, err := m.svc.VerifyOnboarding(ctx, app.RepositoryID, app.Platform); err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).Str("platform", app.Platform).
			Msg("store monitor: verify onboarding failed")
	}
}

// sweepTestReady checks whether a test_ready app has gone live in its store
// (iOS: the latest version is READY_FOR_SALE; Android: the production track
// carries a release) and, if so, advances it and comments on the onboarding
// task when one is on file.
func (m *Monitor) sweepTestReady(ctx context.Context, app domain.MobileStoreApp) {
	live, err := m.isLiveInStore(ctx, app)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: check store go-live failed")
		return
	}
	if !live || !app.CanTransition(domain.MobileStoreStateLive) {
		return
	}

	now := time.Now()
	app.State = domain.MobileStoreStateLive
	app.FirstPublishedAt = &now
	app.ReviewState = ""

	stored, err := m.apps.Upsert(ctx, app)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: persisting go-live failed")
		return
	}
	m.commentOnboarding(ctx, stored, fmt.Sprintf("%s is now live in the %s store.", stored.Identifier, stored.Platform))
}

func (m *Monitor) isLiveInStore(ctx context.Context, app domain.MobileStoreApp) (bool, error) {
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		client, err := m.svc.asc(ctx)
		if err != nil {
			return false, err
		}
		info, err := client.LatestVersion(ctx, app.StoreAppID)
		if err != nil {
			return false, err
		}
		return info.State == "READY_FOR_SALE", nil
	case domain.MobileStorePlatformAndroid:
		client, err := m.svc.play(ctx)
		if err != nil {
			return false, err
		}
		track, err := client.TrackInfo(ctx, app.Identifier, "production")
		if err != nil {
			return false, err
		}
		return track.HasRelease, nil
	default:
		return false, nil
	}
}

// MarkSubmitted records that a prod deploy has just handed a build to
// platform's store for review. It is the submit side of the review
// lifecycle: sweepLive only polls a row whose ReviewState is still open, so
// this write is what makes every downstream piece — pollIOSReview,
// pollAndroidRollout, the rejection incident, LastReleasedVersion — reachable
// at all. Nothing else in the system opens that gate.
//
// version is the marketing version when the caller knows it. The deploy
// pipeline does not: the workflow reads it out of the project
// (xcodebuild -showBuildSettings, pubspec.yaml) long after dispatch, so it
// passes "" and any previously recorded version is left intact. The store's
// own reported version wins on approval regardless.
//
// A repository with no registry row for platform is not an error — it simply
// has nothing to track.
func (s *Service) MarkSubmitted(ctx context.Context, repositoryID uuid.UUID, platform, version string) error {
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("storeops: loading store app: %w", err)
	}
	app.ReviewState = domain.ReviewStateWaiting
	if version != "" {
		app.LastSubmittedVersion = version
	}
	if _, err := s.apps.Upsert(ctx, app); err != nil {
		return fmt.Errorf("storeops: recording store submit: %w", err)
	}
	return nil
}

// sweepLive polls a live app's pending store review. Only a row with a
// review_state still open (waiting_for_review, in_review) has anything to
// poll — an approved or rejected row already carries its terminal
// review_state and falls out of this gate on its own, which is how a
// rejection is never re-ingested on a later sweep.
func (m *Monitor) sweepLive(ctx context.Context, app domain.MobileStoreApp) {
	if app.ReviewState != domain.ReviewStateWaiting && app.ReviewState != domain.ReviewStateInReview {
		return
	}
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		m.pollIOSReview(ctx, app)
	case domain.MobileStorePlatformAndroid:
		m.pollAndroidRollout(ctx, app)
	}
}

// ascReviewState maps App Store Connect's raw version state onto the
// review_state values this system tracks. A state outside this map (an
// in-flight PREPARE_FOR_SUBMISSION, for instance) reports no verdict yet, so
// the row is left untouched until the next sweep.
func ascReviewState(state string) string {
	switch state {
	case "WAITING_FOR_REVIEW":
		return domain.ReviewStateWaiting
	case "IN_REVIEW":
		return domain.ReviewStateInReview
	case "READY_FOR_SALE", "PENDING_DEVELOPER_RELEASE":
		return domain.ReviewStateApproved
	case "REJECTED", "METADATA_REJECTED", "DEVELOPER_REJECTED":
		return domain.ReviewStateRejected
	default:
		return ""
	}
}

func (m *Monitor) pollIOSReview(ctx context.Context, app domain.MobileStoreApp) {
	client, err := m.svc.asc(ctx)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: build app store connect client failed")
		return
	}
	info, err := client.LatestVersion(ctx, app.StoreAppID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: check app store review status failed")
		return
	}
	newState := ascReviewState(info.State)
	if newState == "" || newState == app.ReviewState {
		return
	}

	// A rejection must be ingested before the row is marked rejected: once
	// review_state leaves the waiting/in_review gate it is never re-polled
	// (that is exactly how a rejection avoids being re-ingested on a later
	// sweep), so persisting the terminal state ahead of a successful ingest
	// would silently and permanently lose the incident on a transient
	// ingest failure. Same idiom prodops.Monitor.check uses: clear/advance
	// dedupe state only after the ingest that depends on it has succeeded.
	if newState == domain.ReviewStateRejected {
		if err := m.ingestIncident(ctx, domain.IncidentInput{
			RepositoryID: app.RepositoryID,
			Env:          domain.DeployEnvProd,
			Source:       domain.IncidentSourceProbe,
			Severity:     domain.IncidentSeverityHigh,
			Fingerprint:  domain.IncidentFingerprint("store_review", app.Platform, info.Version),
			Title:        fmt.Sprintf("Store review rejected: %s %s", app.Identifier, info.Version),
			Detail:       fmt.Sprintf("App Store Connect reported %s for %s.", info.State, app.Identifier),
			Payload: map[string]any{
				"platform":   app.Platform,
				"identifier": app.Identifier,
				"version":    info.Version,
				"asc_state":  info.State,
			},
		}); err != nil {
			// Logged inside ingestIncident. Leave review_state untouched so
			// the row stays in sweepLive's poll gate and is retried.
			return
		}
	}

	app.ReviewState = newState
	if newState == domain.ReviewStateApproved {
		// ASC's own version string is the authority on what was released.
		// LastSubmittedVersion is only a fallback: the control plane records
		// a submit at prod-deploy time, when the marketing version is still
		// inside the workflow (xcodebuild -showBuildSettings / pubspec.yaml)
		// and unknown here.
		app.LastReleasedVersion = info.Version
		if app.LastReleasedVersion == "" {
			app.LastReleasedVersion = app.LastSubmittedVersion
		}
	}

	stored, err := m.apps.Upsert(ctx, app)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: persisting review state failed")
		return
	}

	if newState == domain.ReviewStateApproved {
		m.commentOnboarding(ctx, stored, fmt.Sprintf("Store review approved for %s %s.", stored.Identifier, stored.LastReleasedVersion))
	}
}

// pollAndroidRollout checks the production track's rollout status. Google
// Play has no ASC-style review states to mirror; the one signal this system
// acts on is a halted staged rollout, which — unlike a rejection — leaves
// review_state untouched, so dedupe falls back to the in-memory notified
// set (see the Monitor.notified field comment).
func (m *Monitor) pollAndroidRollout(ctx context.Context, app domain.MobileStoreApp) {
	client, err := m.svc.play(ctx)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: build google play client failed")
		return
	}
	track, err := client.TrackInfo(ctx, app.Identifier, "production")
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: check play production track failed")
		return
	}
	if track.Status != "halted" {
		return
	}

	fingerprint := domain.IncidentFingerprint("store_rollout", app.Identifier, track.VersionName)
	// Claim the fingerprint under the lock, before ingesting. Checking and
	// marking as two separate critical sections leaves a window where two
	// overlapping Sweeps (a manual Sweep alongside the ticker, or a slow
	// sweep still running when the next one starts) both read "not notified"
	// and both ingest. The claim is released again below if the ingest
	// fails, so a transient ingest error is still retried on the next sweep
	// rather than being permanently swallowed by its own claim.
	m.mu.Lock()
	already := m.notified[fingerprint]
	if !already {
		m.notified[fingerprint] = true
	}
	m.mu.Unlock()
	if already {
		return
	}

	if err := m.ingestIncident(ctx, domain.IncidentInput{
		RepositoryID: app.RepositoryID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceProbe,
		Severity:     domain.IncidentSeverityHigh,
		Fingerprint:  fingerprint,
		Title:        fmt.Sprintf("Store rollout halted: %s %s", app.Identifier, track.VersionName),
		Detail:       fmt.Sprintf("Google Play production track reported status %q for %s.", track.Status, app.Identifier),
		Payload: map[string]any{
			"platform":   app.Platform,
			"identifier": app.Identifier,
			"version":    track.VersionName,
			"status":     track.Status,
		},
	}); err != nil {
		// Release the claim: the rollout is still halted, so the next sweep
		// polls this row again and retries the ingest instead of silently
		// dropping a high-severity incident forever.
		m.mu.Lock()
		delete(m.notified, fingerprint)
		m.mu.Unlock()
		return
	}
}

// commentOnboarding posts a system comment on app's onboarding task. Both
// preconditions — a task on file and a Commenter wired up — are optional, so
// a row with neither never panics here.
func (m *Monitor) commentOnboarding(ctx context.Context, app domain.MobileStoreApp, content string) {
	if app.OnboardingTaskID == nil || m.svc.comments == nil {
		return
	}
	if _, err := m.svc.comments.AddComment(ctx, app.RepositoryID, *app.OnboardingTaskID, domain.CreateTaskCommentRequest{
		Content:    content,
		AuthorType: "system",
	}); err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: posting comment failed")
	}
}

// ingestIncident hands off to the injected IncidentIngester, tolerating a
// nil one (a Monitor can run without incident wiring without panicking) —
// treated as success, since there is nothing to retry. A real ingest error
// is logged and returned so a caller that gates dedupe state on success
// (a rejection's review_state, a halted rollout's notified claim) knows to
// hold back — or release — that state, leaving the row retryable on the next
// sweep instead of silently losing a high-severity incident forever.
func (m *Monitor) ingestIncident(ctx context.Context, in domain.IncidentInput) error {
	if m.ingester == nil {
		return nil
	}
	if _, err := m.ingester.Ingest(ctx, in); err != nil {
		log.Warn().Err(err).Str("fingerprint", in.Fingerprint).Msg("store monitor: incident ingest failed")
		return err
	}
	return nil
}
