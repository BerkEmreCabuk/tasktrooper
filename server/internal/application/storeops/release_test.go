package storeops_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// releaseFixture bundles the collaborators newReleaseTestService wires up,
// so a test can both script a scenario (setState) and assert exactly what
// the release actions did (asc, play, audit).
type releaseFixture struct {
	repoID uuid.UUID
	repos  *fakeRepositoryResolver
	apps   *fakeMobileStoreAppStore
	asc    *fakeASC
	play   *fakePlay
	audit  *fakeOpsAuditStore
}

// setState overwrites both the iOS and Android registry rows' state — every
// release test targets one platform or the other, never both at once, so a
// single knob covering both keeps the fixture setup terse. A platform with
// no registered row yet is silently skipped.
func (f *releaseFixture) setState(state string) {
	for _, platform := range []string{domain.MobileStorePlatformIOS, domain.MobileStorePlatformAndroid} {
		app, err := f.apps.Get(context.Background(), f.repoID, platform)
		if err != nil {
			continue
		}
		app.State = state
		_, _ = f.apps.Upsert(context.Background(), app)
	}
}

// newReleaseTestService wires a storeops.Service with a live iOS app and a
// live Android app already registered for one repository named repoName,
// working App Store Connect / Google Play credentials saved (so any method
// that reaches the client succeeds), and the audit store wired via
// SetAuditor. Tests that need a different starting state call
// f.setState.
func newReleaseTestService(t *testing.T, repoName string) (*storeops.Service, *releaseFixture) {
	t.Helper()

	repoID := uuid.New()
	repos := newFakeRepositoryResolver()
	repos.set(domain.Repository{ID: repoID, Name: repoName})

	apps := newFakeMobileStoreAppStore()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.ios",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	}); err != nil {
		t.Fatal(err)
	}

	asc := &fakeASC{LatestVersionResult: port.AppStoreVersionInfo{Version: "1.2.3"}}
	play := &fakePlay{}
	audit := newFakeOpsAuditStore()

	cipher, err := secrets.NewCipher([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}

	svc := storeops.NewService(storeops.Deps{
		Credentials: newFakeCredentialStore(),
		Apps:        apps,
		Cipher:      cipher,
		NewASC:      newFakeASCFactory(asc, nil),
		NewPlay:     newFakePlayFactory(play, nil),
		Repos:       repos,
	})
	svc.SetAuditor(audit)

	if err := svc.SaveCredential(context.Background(), domain.StoreCredentialASC, map[string]string{
		"key_id": "K1", "issuer_id": "I1", "p8": "fake-p8",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, map[string]string{
		"service_account_json": "{}",
	}); err != nil {
		t.Fatal(err)
	}

	return svc, &releaseFixture{repoID: repoID, repos: repos, apps: apps, asc: asc, play: play, audit: audit}
}

// The same server-side brake the deploy actions have. iOS submit is
// production-class: it puts the app in front of Apple's reviewers.
func TestSubmitIOSRequiresConfirmPhrase(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	err := svc.SubmitIOS(context.Background(), f.repoID, "wrong", "akif")
	if !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("err = %v, want ErrConfirmMismatch", err)
	}
	if f.asc.submits != 0 {
		t.Fatal("submitted to Apple despite a failed confirmation")
	}
}

// Only a live app can be submitted; a half-onboarded one has no store app id
// to submit against, and the error must say so rather than 500 from the API.
func TestSubmitIOSRefusesAnAppThatIsNotOnboarded(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	f.setState(domain.MobileStoreStateOnboarding)
	err := svc.SubmitIOS(context.Background(), f.repoID, "r", "akif")
	if err == nil || f.asc.submits != 0 {
		t.Fatalf("err = %v, submits = %d", err, f.asc.submits)
	}
}

// Rollout fraction is user input reaching a production API — clamp it here,
// not in the browser.
func TestSetAndroidRolloutRejectsFractionOutOfRange(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	for _, bad := range []float64{-0.1, 1.5} {
		if err := svc.SetAndroidRollout(context.Background(), f.repoID, bad, "akif"); err == nil {
			t.Fatalf("fraction %v accepted", bad)
		}
	}
}

// Promoting to production is production-class; promoting to a test track is not.
func TestPromoteAndroidRequiresConfirmOnlyForProduction(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	if err := svc.PromoteAndroid(context.Background(), f.repoID, "internal", 1, "", "akif"); err != nil {
		t.Fatalf("internal promote should not need a confirmation: %v", err)
	}
	if err := svc.PromoteAndroid(context.Background(), f.repoID, "production", 1, "", "akif"); !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("production promote err = %v, want ErrConfirmMismatch", err)
	}
}

// Every attempt is audited, including the refused ones.
func TestRefusedStoreActionIsAudited(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	_ = svc.HaltAndroid(context.Background(), f.repoID, "nope", "akif")
	if len(f.audit.entries) != 1 || f.audit.entries[0].Outcome != domain.OpsOutcomeError {
		t.Fatalf("audit = %+v", f.audit.entries)
	}
}

// --- Additional coverage beyond the plan's required tests ---

// A repository with no registered app for the platform 404s instead of
// panicking or auditing a phantom attempt.
func TestSubmitIOSPropagatesAppNotFound(t *testing.T) {
	svc, f := newReleaseTestService(t, "r")
	otherRepo := uuid.New()
	f.repos.set(domain.Repository{ID: otherRepo, Name: "other"})
	err := svc.SubmitIOS(context.Background(), otherRepo, "other", "akif")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want port.ErrNotFound", err)
	}
	if len(f.audit.entries) != 0 {
		t.Fatalf("a not-found app must not be audited: %+v", f.audit.entries)
	}
}

// The success path: a correctly confirmed submit reaches LatestVersion and
// SubmitForReview, and the single audit row records success.
func TestSubmitIOSSucceedsAndAudits(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	if err := svc.SubmitIOS(context.Background(), f.repoID, "tasktrooper", "akif"); err != nil {
		t.Fatalf("SubmitIOS: %v", err)
	}
	if f.asc.submits != 1 {
		t.Fatalf("submits = %d, want 1", f.asc.submits)
	}
	if len(f.audit.entries) != 1 || f.audit.entries[0].Outcome != domain.OpsOutcomeOK {
		t.Fatalf("audit = %+v", f.audit.entries)
	}
}

// ReleaseIOS follows SubmitIOS's exact shape: same confirm guardrail, same
// live-state precondition.
func TestReleaseIOSRequiresConfirmPhrase(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	err := svc.ReleaseIOS(context.Background(), f.repoID, "wrong", "akif")
	if !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("err = %v, want ErrConfirmMismatch", err)
	}
	if f.asc.releases != 0 {
		t.Fatal("released despite a failed confirmation")
	}
}

// Halt and resume are the two Android actions that need no confirmation on
// resume (Halt is production-class, Resume never touches what users get
// beyond what was already rolling out).
func TestResumeAndroidNeedsNoConfirmation(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	if err := svc.ResumeAndroid(context.Background(), f.repoID, "akif"); err != nil {
		t.Fatalf("ResumeAndroid: %v", err)
	}
	if len(f.play.ResumeRolloutCalls) != 1 {
		t.Fatalf("ResumeRolloutCalls = %+v", f.play.ResumeRolloutCalls)
	}
}

// A missing store credential surfaces as ErrStoreCredentialUnavailable, not
// a bare port.ErrNotFound or a generic 500-shaped error — the HTTP layer
// keys 424 Failed Dependency off this sentinel.
func TestHaltAndroidWithNoCredentialReportsCredentialUnavailable(t *testing.T) {
	svc, f := newReleaseTestService(t, "tasktrooper")
	if err := svc.DeleteCredential(context.Background(), domain.StoreCredentialGooglePlay); err != nil {
		t.Fatal(err)
	}
	err := svc.HaltAndroid(context.Background(), f.repoID, "tasktrooper", "akif")
	if !errors.Is(err, storeops.ErrStoreCredentialUnavailable) {
		t.Fatalf("err = %v, want ErrStoreCredentialUnavailable", err)
	}
}
