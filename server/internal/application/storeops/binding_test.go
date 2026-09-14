package storeops_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// A Play credential that cannot enumerate is an ANSWER, not a failure — the
// Play Developer API has no listing endpoint at all. The sentinel has to
// survive the service's own wrapping, because that is what lets the HTTP layer
// answer 200 with listing_available:false instead of sending the operator to
// retry something that will never start working.
func TestListStoreAppsKeepsTheUnavailableAnswerMatchable(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.play.ListAppsErr = port.ErrAppListingUnavailable

	_, err := svc.ListStoreApps(context.Background(), domain.StoreCredentialGooglePlay)
	if !errors.Is(err, port.ErrAppListingUnavailable) {
		t.Fatalf("err = %v, want a wrapped ErrAppListingUnavailable", err)
	}
	if errors.Is(err, storeops.ErrInvalidCredential) {
		t.Fatal("an unavailable listing was reported as a bad credential")
	}
}

func TestListStoreAppsSortsTheListing(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.asc.ListAppsResult = []port.StoreAppRef{
		{Identifier: "com.example.zed", Name: "Zed"},
		{Identifier: "com.example.alpha", Name: "Alpha"},
	}
	apps, err := svc.ListStoreApps(context.Background(), domain.StoreCredentialASC)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].Identifier != "com.example.alpha" {
		t.Fatalf("apps = %+v, want a deterministic order", apps)
	}
}

// The binding is confirmed against the store before it is written: a row
// pointing at an app the console cannot resolve is a row every later upload
// fails on, and the refusal is the caller's own mistake rather than a 500.
func TestLinkStoreAppRefusesAnAppTheStoreDoesNotHave(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.setState(domain.MobileStoreStateOnboarding)
	f.play.AppExistsResult = false

	_, err := svc.LinkStoreApp(context.Background(), f.repoID, domain.MobileStorePlatformAndroid,
		port.StoreAppRef{Identifier: "com.example.ghost"})
	if !errors.Is(err, storeops.ErrAppNotInStore) {
		t.Fatalf("err = %v, want ErrAppNotInStore", err)
	}
}

// Linking states identity, nothing else. A link that moved the row forward
// would let mobileStoreGate green-light a deploy against an app whose
// onboarding nobody verified.
func TestLinkStoreAppLeavesTheLifecycleAlone(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()
	f.setState(domain.MobileStoreStateOnboarding)
	f.asc.AppByBundleIDFound = true
	f.asc.AppByBundleIDID = "asc-confirmed"

	app, err := svc.LinkStoreApp(ctx, f.repoID, domain.MobileStorePlatformIOS,
		port.StoreAppRef{Identifier: "com.example.ios", StoreAppID: "stale-from-the-picker", Name: "Trooper"})
	if err != nil {
		t.Fatal(err)
	}
	if app.State != domain.MobileStoreStateOnboarding {
		t.Fatalf("state = %q, want onboarding untouched", app.State)
	}
	// ASC's own answer wins: the resource id is what every later call is
	// addressed with, and a stale one from a cached listing addresses the
	// wrong app.
	if app.StoreAppID != "asc-confirmed" || app.AppName != "Trooper" {
		t.Fatalf("app = %+v, want the console's own app id and name", app)
	}
}

// A live app cannot be re-pointed at a different identifier — the same rule
// Onboard enforces, reached through the picker this time.
func TestLinkStoreAppRefusesRepointingALiveApp(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.play.AppExistsResult = true

	_, err := svc.LinkStoreApp(context.Background(), f.repoID, domain.MobileStorePlatformAndroid,
		port.StoreAppRef{Identifier: "com.example.other"})
	if !errors.Is(err, storeops.ErrIdentifierLocked) {
		t.Fatalf("err = %v, want ErrIdentifierLocked", err)
	}
}

func TestLinkStoreAppRejectsAnUnknownPlatform(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	if _, err := svc.LinkStoreApp(context.Background(), f.repoID, "windows",
		port.StoreAppRef{Identifier: "com.example.x"}); !errors.Is(err, storeops.ErrInvalidPlatform) {
		t.Fatalf("err = %v, want ErrInvalidPlatform", err)
	}
}

// Tracks reads live and leaves the cache behind, so the panel can render the
// channels without waiting for a store round-trip next time.
func TestTracksCachesWhatTheStoreReported(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()
	f.play.TracksResult = domain.StoreTracks{
		Internal: domain.TrackRelease{HasRelease: true, Version: "1.2.3", Status: domain.TrackStatusLive},
	}

	tracks, err := svc.Tracks(ctx, f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	if tracks.Internal.Version != "1.2.3" {
		t.Fatalf("tracks = %+v", tracks)
	}
	app, err := f.apps.Get(ctx, f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	if app.Tracks.Internal.Version != "1.2.3" || app.TracksSyncedAt == nil {
		t.Fatalf("row = %+v, want the channel cache written with a sync stamp", app)
	}
}

// Promotion is strictly one step forward. Jumping internal -> production would
// skip the stage the middle channel exists to hold: Beta App Review on iOS,
// the testing track on Play.
func TestPromoteChannelRefusesASkippedChannel(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelInternal, domain.StoreChannelProduction, "trooper", "akif")
	if !errors.Is(err, storeops.ErrInvalidChannel) {
		t.Fatalf("err = %v, want ErrInvalidChannel", err)
	}
	if len(f.asc.PromoteChannelCalls) != 0 {
		t.Fatal("a skipped promotion reached App Store Connect")
	}
}

// Backwards is not a promotion either, and neither is a channel that is not
// one of the three.
func TestPromoteChannelRefusesBackwardsAndUnknownChannels(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	for _, pair := range [][2]string{
		{domain.StoreChannelProduction, domain.StoreChannelExternal},
		{domain.StoreChannelExternal, domain.StoreChannelInternal},
		{domain.StoreChannelProduction, domain.StoreChannelProduction},
		{"nightly", domain.StoreChannelProduction},
	} {
		err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS, pair[0], pair[1], "trooper", "akif")
		if !errors.Is(err, storeops.ErrInvalidChannel) {
			t.Fatalf("%v -> %v: err = %v, want ErrInvalidChannel", pair[0], pair[1], err)
		}
	}
}

// Production is production-class: the same confirm brake the deploy and
// submit actions carry. A promotion must not slip past it.
func TestPromoteChannelToProductionRequiresTheConfirmPhrase(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelExternal, domain.StoreChannelProduction, "wrong", "akif")
	if !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("err = %v, want ErrConfirmMismatch", err)
	}
	if len(f.asc.PromoteChannelCalls) != 0 {
		t.Fatal("promoted to production despite a failed confirmation")
	}
	entries, _ := f.audit.List(ctx, &f.repoID, 0)
	if len(entries) != 1 || entries[0].Outcome != domain.OpsOutcomeError {
		t.Fatalf("audit = %+v, want the refused attempt recorded", entries)
	}
}

// An app that has not gone live yet may still move between the test channels,
// but production is out of reach until the first manual store submit landed —
// the same gate SubmitIOS and PromoteAndroid apply.
func TestPromoteChannelRefusesProductionForAnAppThatIsNotLive(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()
	f.setState(domain.MobileStoreStateTestReady)

	err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelExternal, domain.StoreChannelProduction, "trooper", "akif")
	if !errors.Is(err, storeops.ErrAppNotReady) {
		t.Fatalf("err = %v, want ErrAppNotReady", err)
	}
	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelInternal, domain.StoreChannelExternal, "", "akif"); err != nil {
		t.Fatalf("a test-channel promotion was refused for a test_ready app: %v", err)
	}
}

// Android speaks Play's track names, not the product's channel words, and
// which of Play's two testing tracks `external` means is only knowable from
// the Tracks read — the adapter records it in the channel's Audience.
func TestPromoteChannelTranslatesAndroidTrackNames(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformAndroid,
		domain.StoreChannelInternal, domain.StoreChannelExternal, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if len(f.play.PromoteTrackCalls) != 1 || f.play.PromoteTrackCalls[0].ToTrack != "beta" {
		t.Fatalf("calls = %+v, want open testing when nothing says otherwise", f.play.PromoteTrackCalls)
	}

	app, err := f.apps.Get(ctx, f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	app.Tracks.External = domain.TrackRelease{HasRelease: true, Audience: "Closed testing"}
	if _, err := f.apps.Upsert(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformAndroid,
		domain.StoreChannelExternal, domain.StoreChannelProduction, "trooper", "akif"); err != nil {
		t.Fatal(err)
	}
	last := f.play.PromoteTrackCalls[len(f.play.PromoteTrackCalls)-1]
	if last.FromTrack != "alpha" || last.ToTrack != "production" {
		t.Fatalf("call = %+v, want alpha -> production", last)
	}
}

// iOS is addressed by its ASC resource id and speaks the channel words
// directly — the adapter is what knows they mean TestFlight groups.
func TestPromoteChannelUsesTheASCResourceIDForIOS(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	if err := svc.PromoteChannel(context.Background(), f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelInternal, domain.StoreChannelExternal, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if len(f.asc.PromoteChannelCalls) != 1 || f.asc.PromoteChannelCalls[0] != [3]string{"asc-app-1", "internal", "external"} {
		t.Fatalf("calls = %+v", f.asc.PromoteChannelCalls)
	}
}

func TestPromoteChannelOnAnUnknownRepositoryIsNotFound(t *testing.T) {
	svc, _ := newReleaseTestService(t, "trooper")
	err := svc.PromoteChannel(context.Background(), uuid.New(), domain.MobileStorePlatformIOS,
		domain.StoreChannelInternal, domain.StoreChannelExternal, "", "akif")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
