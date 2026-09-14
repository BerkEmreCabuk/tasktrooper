package storeops_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// MonitorSuite exercises storeops.Monitor's one-pass Sweep: the four rules
// (verify onboarding, detect go-live, poll a live app's pending review,
// renew signing every pass) plus the dedupe and nil-tolerance rulings the
// Task 9 controller called out.
type MonitorSuite struct {
	suite.Suite

	prevKey    string
	prevKeySet bool

	creds    *fakeCredentialStore
	apps     *fakeMobileStoreAppStore
	signing  *fakeSigningAssetStore
	asc      *fakeASC
	play     *fakePlay
	push     *fakePushSecret
	tasks    *fakeTaskCreator
	repos    *fakeRepositoryResolver
	comments *fakeCommenter
	ingester *fakeIncidentIngester
	cipher   *secrets.Cipher

	svc     *storeops.Service
	monitor *storeops.Monitor
}

func TestMonitorSuite(t *testing.T) {
	suite.Run(t, new(MonitorSuite))
}

func (s *MonitorSuite) SetupTest() {
	s.prevKey, s.prevKeySet = os.LookupEnv("MCP_SECRETS_KEY")
	s.Require().NoError(os.Setenv("MCP_SECRETS_KEY", "test-storeops-monitor-key"))

	cipher, err := secrets.NewCipherFromEnv()
	s.Require().NoError(err)
	s.cipher = cipher

	s.creds = newFakeCredentialStore()
	s.apps = newFakeMobileStoreAppStore()
	s.signing = newFakeSigningAssetStore()
	s.asc = &fakeASC{}
	s.play = &fakePlay{}
	s.push = &fakePushSecret{}
	s.tasks = newFakeTaskCreator()
	s.repos = newFakeRepositoryResolver()
	s.comments = newFakeCommenter()
	s.ingester = newFakeIncidentIngester()

	s.svc = storeops.NewService(storeops.Deps{
		Credentials: s.creds,
		Apps:        s.apps,
		Signing:     s.signing,
		Cipher:      s.cipher,
		NewASC:      newFakeASCFactory(s.asc, nil),
		NewPlay:     newFakePlayFactory(s.play, nil),
		PushSecret:  s.push.Push,
		Repos:       s.repos,
		Tasks:       s.tasks,
		Comments:    s.comments,
	})
	s.monitor = storeops.NewMonitor(s.svc, s.apps, s.ingester)
}

func (s *MonitorSuite) TearDownTest() {
	if s.prevKeySet {
		os.Setenv("MCP_SECRETS_KEY", s.prevKey)
	} else {
		os.Unsetenv("MCP_SECRETS_KEY")
	}
}

func (s *MonitorSuite) storeASCCredential() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, map[string]string{
		"key_id":    "K1MEE23AB",
		"issuer_id": "69a6de8b-fake-issuer",
		"p8":        "-----BEGIN PRIVATE KEY-----\nfake-monitor-key-material\n-----END PRIVATE KEY-----",
	}))
}

func (s *MonitorSuite) storePlayCredential() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, map[string]string{
		"service_account_json": `{"client_email":"deploy@project.iam.gserviceaccount.com","private_key":"fake"}`,
	}))
}

// --- Rule 1: onboarding -> VerifyOnboarding --------------------------------

// A row still onboarding gets re-checked against the store API; once the
// last checklist item verifies, VerifyOnboarding's own logic advances it to
// test_ready and posts the verification comment — the monitor's job here is
// only to call it.
func (s *MonitorSuite) TestSweepOnboardingVerifiesAndAdvancesToTestReady() {
	s.storeASCCredential()
	s.asc.AppByBundleIDFound = true
	s.asc.AppByBundleIDID = "asc-app-9"
	repoID := uuid.New()
	taskID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:     repoID,
		Platform:         domain.MobileStorePlatformIOS,
		Identifier:       "com.example.app",
		State:            domain.MobileStoreStateOnboarding,
		Checklist:        []domain.ChecklistItem{{Key: "ios_app_record", Title: "register the bundle id"}},
		OnboardingTaskID: &taskID,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateTestReady, got.State)
	s.Require().Len(got.Checklist, 1)
	s.True(got.Checklist[0].Done)
	s.Equal("asc-app-9", got.StoreAppID)
	s.Require().Len(s.comments.Calls, 1, "VerifyOnboarding posts its own verification comment")
	s.Equal(taskID, s.comments.Calls[0].TaskID)
}

// --- Rule 2: test_ready -> live ---------------------------------------------

func (s *MonitorSuite) TestSweepTestReadyIOSGoesLiveAndComments() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.0.0", State: "READY_FOR_SALE"}
	repoID := uuid.New()
	taskID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:     repoID,
		Platform:         domain.MobileStorePlatformIOS,
		Identifier:       "com.example.app",
		StoreAppID:       "asc-app-1",
		State:            domain.MobileStoreStateTestReady,
		OnboardingTaskID: &taskID,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, got.State)
	s.Require().NotNil(got.FirstPublishedAt)
	s.Empty(got.ReviewState)
	s.Require().Len(s.comments.Calls, 1)
	s.Equal(taskID, s.comments.Calls[0].TaskID)
}

// A go-live with no onboarding task on file (or no Commenter wired up) must
// not panic — ruling #4.
func (s *MonitorSuite) TestSweepTestReadyAndroidGoesLiveWithoutTaskDoesNotPanic() {
	s.storePlayCredential()
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true, VersionName: "2.0.0", Status: "completed"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)

	s.NotPanics(func() { s.monitor.Sweep(ctx) })

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, got.State)
	s.Require().NotNil(got.FirstPublishedAt)
	s.Empty(s.comments.Calls, "no onboarding task means no comment")
}

func (s *MonitorSuite) TestSweepTestReadyStaysWhenNotYetLive() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.0.0", State: "PREPARE_FOR_SUBMISSION"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateTestReady, got.State, "not READY_FOR_SALE yet: stays test_ready")
}

// --- Rule 3: live review polling --------------------------------------------

func (s *MonitorSuite) TestSweepLiveIOSApprovedSetsLastReleasedVersionAndComments() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.1.0", State: "READY_FOR_SALE"}
	repoID := uuid.New()
	taskID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformIOS,
		Identifier:           "com.example.app",
		StoreAppID:           "asc-app-1",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateInReview,
		LastSubmittedVersion: "1.1.0",
		OnboardingTaskID:     &taskID,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateApproved, got.ReviewState)
	s.Equal("1.1.0", got.LastReleasedVersion)
	s.Require().Len(s.comments.Calls, 1)
	s.Empty(s.ingester.Calls, "an approval is not an incident")
}

// The brief's explicit dedupe requirement: a rejected version ingests
// exactly one incident, and a second sweep over the same row must not poll
// ASC again or re-ingest — the persisted "rejected" review_state no longer
// matches the live sweep's poll gate, so the row is skipped outright.
func (s *MonitorSuite) TestSweepLiveIOSRejectedIngestsIncidentExactlyOnceAcrossSweeps() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.2.0", State: "REJECTED"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformIOS,
		Identifier:           "com.example.app",
		StoreAppID:           "asc-app-1",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "1.2.0",
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateRejected, got.ReviewState)
	s.Require().Len(s.ingester.Calls, 1)
	incident := s.ingester.Calls[0]
	s.Equal(repoID, incident.RepositoryID)
	s.Equal(domain.IncidentSeverityHigh, incident.Severity)
	s.Equal("Store review rejected: com.example.app 1.2.0", incident.Title)
	s.Equal(domain.IncidentFingerprint("store_review", domain.MobileStorePlatformIOS, "1.2.0"), incident.Fingerprint)

	s.monitor.Sweep(ctx)

	s.Len(s.ingester.Calls, 1, "rejection must be ingested exactly once across repeated sweeps")
	s.Len(s.asc.LatestVersionCalls, 1, "a rejected row is skipped on the next sweep, not re-polled")
}

// A transient incident-ingest failure must not permanently lose the
// incident: review_state must stay in a poll-gate state so the row is
// retried, rather than flipping to "rejected" (and dropping out of the
// gate) before the ingest is known to have succeeded.
func (s *MonitorSuite) TestSweepLiveIOSRejectedRetriesAfterFailedIngest() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.3.0", State: "REJECTED"}
	s.ingester.Err = errors.New("incident service down")
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformIOS,
		Identifier:           "com.example.app",
		StoreAppID:           "asc-app-1",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "1.3.0",
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateWaiting, got.ReviewState, "a failed ingest must not mark the row rejected")
	s.Require().Len(s.ingester.Calls, 1, "the ingest was attempted")

	// The row is still in the poll gate, so a second sweep retries it.
	s.monitor.Sweep(ctx)
	s.Require().Len(s.ingester.Calls, 2, "still waiting: the row was polled and ingest retried")

	got, err = s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateWaiting, got.ReviewState, "still not marked rejected: the retry also failed")

	// Clear the failure and sweep again: this time the ingest succeeds and
	// the row finally leaves the poll gate.
	s.ingester.Err = nil
	s.monitor.Sweep(ctx)

	got, err = s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateRejected, got.ReviewState)
	s.Require().Len(s.ingester.Calls, 3, "the retry succeeded")

	// A fourth sweep must not re-ingest: the row has left the poll gate.
	s.monitor.Sweep(ctx)
	s.Len(s.ingester.Calls, 3, "rejected row no longer polled: no further ingest attempts")
}

// Same shape as the iOS case, for the Android in-memory notified fallback:
// a failed ingest must not mark the fingerprint notified, or a genuinely
// still-halted rollout would never be retried.
func (s *MonitorSuite) TestSweepLiveAndroidHaltedRolloutRetriesAfterFailedIngest() {
	s.storePlayCredential()
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true, Status: "halted", VersionName: "4.0.0"}
	s.ingester.Err = errors.New("incident service down")
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformAndroid,
		Identifier:           "com.example.android",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "4.0.0",
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)
	s.Require().Len(s.ingester.Calls, 1, "the ingest was attempted")

	// Not marked notified: a second sweep (still failing) retries.
	s.monitor.Sweep(ctx)
	s.Require().Len(s.ingester.Calls, 2, "still not notified: the retry was attempted again")

	// Clear the failure: this time the incident lands and is marked
	// notified, so a further sweep does not re-ingest.
	s.ingester.Err = nil
	s.monitor.Sweep(ctx)
	s.Require().Len(s.ingester.Calls, 3, "the retry succeeded")

	s.monitor.Sweep(ctx)
	s.Len(s.ingester.Calls, 3, "now notified: no further ingest attempts")
}

// One row's failure (here: an iOS row whose ASC credential is missing, so
// VerifyOnboarding errors) must not stop the sweep from processing the
// other rows in the same pass — ruling #1.
func (s *MonitorSuite) TestSweepOneRowsErrorDoesNotAbortTheRest() {
	s.storePlayCredential() // no ASC credential stored: the iOS row's client build fails
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true, VersionName: "1.0.0", Status: "completed"}
	ctx := context.Background()

	repoIOS := uuid.New()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoIOS,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		State:        domain.MobileStoreStateOnboarding,
		Checklist:    []domain.ChecklistItem{{Key: "ios_app_record", Title: "register the bundle id"}},
	})
	s.Require().NoError(err)

	repoAndroid := uuid.New()
	_, err = s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoAndroid,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)

	s.NotPanics(func() { s.monitor.Sweep(ctx) })

	iosRow, err := s.apps.Get(ctx, repoIOS, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateOnboarding, iosRow.State, "the errored row is left untouched, not crashed on")

	androidRow, err := s.apps.Get(ctx, repoAndroid, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, androidRow.State, "the other row is still swept despite the first row's error")
}

// Android has no ASC-style review_state map, so the only live-poll signal
// is a halted production rollout. There is no persisted field to flip on
// halt (unlike rejected), so dedupe here is the documented in-memory
// fallback — the row keeps polling, but only ingests once.
func (s *MonitorSuite) TestSweepLiveAndroidHaltedRolloutIngestsIncidentAndDedupesInMemory() {
	s.storePlayCredential()
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true, Status: "halted", VersionName: "3.0.0"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformAndroid,
		Identifier:           "com.example.android",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "3.0.0",
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)
	s.monitor.Sweep(ctx)

	s.Require().Len(s.ingester.Calls, 1, "the halted rollout is ingested once")
	incident := s.ingester.Calls[0]
	s.Equal(repoID, incident.RepositoryID)
	s.Equal(domain.IncidentSeverityHigh, incident.Severity)
	s.Equal("Store rollout halted: com.example.android 3.0.0", incident.Title)
	s.Equal(domain.IncidentFingerprint("store_rollout", "com.example.android", "3.0.0"), incident.Fingerprint)
	s.Len(s.play.TrackInfoCalls, 2, "the row keeps polling; only the ingest is deduped")
}

func (s *MonitorSuite) TestSweepLiveSkipsWhenReviewStateNotPending() {
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	s.Empty(s.asc.LatestVersionCalls, "no pending review: nothing to poll")
	s.Empty(s.ingester.Calls)
}

// --- Rule 4: every sweep renews expiring signing ----------------------------

func (s *MonitorSuite) TestSweepRenewsExpiringSigningEveryPass() {
	ctx := context.Background()
	s.storePlayCredential()
	repoID := uuid.New()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	_, err = s.svc.EnsureAndroidKeystore(ctx, "com.example.android")
	s.Require().NoError(err)
	asset, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	before := asset.Serial
	soon := time.Now().Add(10 * 24 * time.Hour)
	asset.ExpiresAt = &soon
	_, err = s.signing.Upsert(ctx, asset)
	s.Require().NoError(err)
	s.push.Calls = nil // clear the push from the EnsureAndroidKeystore call above

	s.monitor.Sweep(ctx)

	after, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	s.NotEqual(before, after.Serial, "the sweep re-minted the soon-to-expire keystore")
	s.NotEmpty(s.push.Calls, "renewed secrets are pushed")
}

// --- Rulings: nil ingester tolerance, unregistered skip ---------------------

func (s *MonitorSuite) TestSweepToleratesNilIngester() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.2.0", State: "REJECTED"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformIOS,
		Identifier:           "com.example.app",
		StoreAppID:           "asc-app-1",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "1.2.0",
	})
	s.Require().NoError(err)

	monitor := storeops.NewMonitor(s.svc, s.apps, nil)

	s.NotPanics(func() { monitor.Sweep(ctx) })

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateRejected, got.ReviewState, "the row still transitions even without an ingester wired up")
}

func (s *MonitorSuite) TestSweepSkipsUnregisteredApp() {
	repoID := uuid.New()
	ctx := context.Background()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		State:        domain.MobileStoreStateUnregistered,
	})
	s.Require().NoError(err)

	s.NotPanics(func() { s.monitor.Sweep(ctx) })

	s.Empty(s.asc.AppByBundleIDCalls)
	s.Empty(s.asc.LatestVersionCalls)
}

// --- Start / Stop ------------------------------------------------------------

func (s *MonitorSuite) TestStartDoesNotBlockAndStopIsSafeToCallTwice() {
	ctx := context.Background()

	s.monitor.Start(ctx, time.Hour)
	s.monitor.Stop()
	s.NotPanics(func() { s.monitor.Stop() }, "Stop must be safe to call twice")
}

// --- The submit side: MarkSubmitted ----------------------------------------

// TestMarkSubmittedOpensTheReviewGate is the regression guard for the gap
// that made every line of review monitoring unreachable in production:
// sweepLive polls nothing unless review_state is waiting_for_review or
// in_review, and until MarkSubmitted existed nothing in the repository ever
// wrote either value. The assertion that matters is the second half — after
// the submit is recorded, a sweep really does poll App Store Connect and act
// on the verdict.
func (s *MonitorSuite) TestMarkSubmittedOpensTheReviewGate() {
	s.storeASCCredential()
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	// Before the submit: a live row with no open review is not polled.
	s.monitor.Sweep(ctx)
	s.Empty(s.asc.LatestVersionCalls, "nothing submitted yet, nothing to poll")

	s.Require().NoError(s.svc.MarkSubmitted(ctx, repoID, domain.MobileStorePlatformIOS, ""))

	submitted, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateWaiting, submitted.ReviewState)

	// After the submit: the row is in the poll gate and a rejection is now
	// ingested as an incident — the whole path the gap made dead.
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "2.0.0", State: "REJECTED"}
	s.monitor.Sweep(ctx)

	s.Len(s.asc.LatestVersionCalls, 1, "the submitted row is polled")
	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateRejected, got.ReviewState)
	s.Require().Len(s.ingester.Calls, 1)
	s.Equal("Store review rejected: com.example.app 2.0.0", s.ingester.Calls[0].Title)
}

// The deploy pipeline cannot know the marketing version at dispatch time (the
// workflow reads it out of the project), so it passes "". An empty version
// must never wipe a version a caller did record.
func (s *MonitorSuite) TestMarkSubmittedRecordsVersionOnlyWhenKnown() {
	repoID := uuid.New()
	ctx := context.Background()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformAndroid,
		Identifier:           "com.example.android",
		State:                domain.MobileStoreStateLive,
		LastSubmittedVersion: "1.0.0",
	})
	s.Require().NoError(err)

	s.Require().NoError(s.svc.MarkSubmitted(ctx, repoID, domain.MobileStorePlatformAndroid, ""))
	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal("1.0.0", got.LastSubmittedVersion, "an unknown version must not erase the recorded one")

	s.Require().NoError(s.svc.MarkSubmitted(ctx, repoID, domain.MobileStorePlatformAndroid, "1.1.0"))
	got, err = s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal("1.1.0", got.LastSubmittedVersion)
	s.Equal(domain.ReviewStateWaiting, got.ReviewState)
}

// A repository with no registry row for the platform simply has nothing to
// track — the pipeline calls this for every store prod deploy and must not
// fail the release over it.
func (s *MonitorSuite) TestMarkSubmittedWithoutARowIsANoop() {
	ctx := context.Background()
	s.Require().NoError(s.svc.MarkSubmitted(ctx, uuid.New(), domain.MobileStorePlatformIOS, "1.0.0"))
	all, err := s.apps.ListAll(ctx)
	s.Require().NoError(err)
	s.Empty(all, "nothing is created for a repository that has no store app")
}

// TestConcurrentSweepsIngestHaltedRolloutOnce covers the check-then-mark race
// on the in-memory dedupe set. The ingester is held open so the second sweep
// is guaranteed to reach the dedupe check while the first ingest is still in
// flight — which, with a separate check and mark, is exactly when both sweeps
// decide to report the same halted rollout.
func (s *MonitorSuite) TestConcurrentSweepsIngestHaltedRolloutOnce() {
	s.storePlayCredential()
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true, Status: "halted", VersionName: "3.0.0"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
		ReviewState:  domain.ReviewStateWaiting,
	})
	s.Require().NoError(err)

	arrived, release := s.ingester.block()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.monitor.Sweep(ctx)
		}()
	}

	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		release()
		wg.Wait()
		s.FailNow("no sweep reached the ingester")
	}
	release()
	wg.Wait()

	s.Len(s.ingester.Calls, 1, "overlapping sweeps must report a halted rollout once")
}

// TestSweepRetriesPushAfterAFailedRenewalPush covers the stranding bug: once
// an asset is re-minted its expiry moves out of ListExpiring's window, so a
// push that fails right after the mint would never be retried — the vault
// holding a new keystore while GitHub Actions still runs on the old one, every
// build failing until someone re-saved the deploy target by hand.
func (s *MonitorSuite) TestSweepRetriesPushAfterAFailedRenewalPush() {
	ctx := context.Background()
	s.storePlayCredential()
	repoID := uuid.New()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	_, err = s.svc.EnsureAndroidKeystore(ctx, "com.example.android")
	s.Require().NoError(err)
	asset, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	soon := time.Now().Add(10 * 24 * time.Hour)
	asset.ExpiresAt = &soon
	_, err = s.signing.Upsert(ctx, asset)
	s.Require().NoError(err)
	s.push.Calls = nil

	// Sweep 1: the keystore is renewed, but GitHub rejects every push.
	s.push.Err = errors.New("github is down")
	s.monitor.Sweep(ctx)
	s.NotEmpty(s.push.Calls, "the renewal attempted a push")

	renewed, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	s.True(renewed.ExpiresAt.After(time.Now().Add(365*24*time.Hour)),
		"the renewed keystore is far outside the renewal window, so ListExpiring will not return it again")

	// Sweep 2: nothing is expiring any more, but the owed push is retried.
	s.push.Err = nil
	s.push.Calls = nil
	s.monitor.Sweep(ctx)

	names := map[string]bool{}
	for _, call := range s.push.Calls {
		s.Equal(repoID, call.RepositoryID)
		names[call.Name] = true
	}
	s.True(names["ANDROID_UPLOAD_KEYSTORE_B64"], "the next sweep re-pushes the stranded secrets: got %v", names)

	// Sweep 3: the push landed, so there is nothing left to retry.
	s.push.Calls = nil
	s.monitor.Sweep(ctx)
	s.Empty(s.push.Calls, "a successful push clears the retry")
}

// TestSweepDoesNotRetryForeverWhenTheMintItselfFails is the other half of the
// retry claim's bookkeeping. A target is claimed as owed a push before the
// mint, so a mint that keeps failing would park it in the retry set for the
// life of the process and re-report its error on every sweep. A failed mint
// produced nothing new — the vault and GitHub still agree on the old asset —
// so the claim must be released.
func (s *MonitorSuite) TestSweepDoesNotRetryForeverWhenTheMintItselfFails() {
	ctx := context.Background()
	s.storePlayCredential()
	repoID := uuid.New()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	_, err = s.svc.EnsureAndroidKeystore(ctx, "com.example.android")
	s.Require().NoError(err)
	asset, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	soon := time.Now().Add(10 * 24 * time.Hour)
	asset.ExpiresAt = &soon
	_, err = s.signing.Upsert(ctx, asset)
	s.Require().NoError(err)

	// Sweep 1: renews (expiry now 25 years out, so ListExpiring is done with
	// it) but the push fails, leaving the target owed a push.
	s.push.Err = errors.New("github is down")
	s.push.Calls = nil
	s.monitor.Sweep(ctx)
	s.Require().NotEmpty(s.push.Calls, "the renewal attempted a push")

	// Sweep 2: the retry runs, but now the mint itself fails — the credential
	// the keystore push needs is gone.
	s.Require().NoError(s.creds.Delete(ctx, domain.StoreCredentialGooglePlay))
	s.push.Err = nil
	s.push.Calls = nil
	s.monitor.Sweep(ctx)
	s.Empty(s.push.Calls, "a failed mint has nothing to push")

	// Sweep 3: everything works again. Nothing is expiring and the claim was
	// released on the mint failure, so there is no work left.
	s.storePlayCredential()
	s.push.Calls = nil
	s.monitor.Sweep(ctx)
	s.Empty(s.push.Calls, "a released claim must not resurrect the retry forever")
}

// The collision the narrow track writer exists for, end to end: a sweep reads
// every row, then goes out to the store console for each one — and a prod
// deploy that lands its submit in that window used to be erased when the
// sweep wrote its seconds-old copy back. sweepLive only polls a row whose
// review_state is still open, so losing it meant that release's store verdict
// (the approval AND the rejection incident) was never seen again.
func (s *MonitorSuite) TestTrackSyncDoesNotEraseASubmitThatLandedMidSweep() {
	s.storePlayCredential()
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.midsweep",
		State:        domain.MobileStoreStateLive,
		ReviewState:  "",
	})
	s.Require().NoError(err)

	// The prod deploy submits while the sweep is out reading the store's
	// channels — after the row was listed, before the track write lands.
	s.play.TracksHook = func() {
		s.Require().NoError(s.svc.MarkSubmitted(ctx, repoID, domain.MobileStorePlatformAndroid, "3.1.0"))
	}
	s.play.TracksResult = domain.StoreTracks{Internal: domain.TrackRelease{HasRelease: true, Version: "3.1.0"}}

	s.monitor.Sweep(ctx)

	s.Require().NotEmpty(s.play.TracksCalls, "the sweep never reached the store, so nothing was raced")
	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateWaiting, got.ReviewState, "the submit was erased by the track sync")
	s.Equal("3.1.0", got.LastSubmittedVersion)
	// The sync still did its own job.
	s.Equal("3.1.0", got.Tracks.Internal.Version)
	s.Require().NotNil(got.TracksSyncedAt)
}
