package storeops_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type OnboardSuite struct {
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
	cipher   *secrets.Cipher

	svc *storeops.Service
}

func TestOnboardSuite(t *testing.T) {
	suite.Run(t, new(OnboardSuite))
}

func (s *OnboardSuite) SetupTest() {
	s.prevKey, s.prevKeySet = os.LookupEnv("MCP_SECRETS_KEY")
	s.Require().NoError(os.Setenv("MCP_SECRETS_KEY", "test-storeops-onboard-key"))

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
}

func (s *OnboardSuite) TearDownTest() {
	if s.prevKeySet {
		os.Setenv("MCP_SECRETS_KEY", s.prevKey)
	} else {
		os.Unsetenv("MCP_SECRETS_KEY")
	}
}

const (
	testOnboardP8      = "-----BEGIN PRIVATE KEY-----\nfake-onboard-key-material\n-----END PRIVATE KEY-----"
	testOnboardSAJSON  = `{"client_email":"deploy@project.iam.gserviceaccount.com","private_key":"fake"}`
	testOnboardKeyID   = "K1MEE23AB"
	testOnboardIssueID = "69a6de8b-fake-issuer"
)

func (s *OnboardSuite) storeASCCredential() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, map[string]string{
		"key_id":    testOnboardKeyID,
		"issuer_id": testOnboardIssueID,
		"p8":        testOnboardP8,
	}))
}

func (s *OnboardSuite) storePlayCredential() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, map[string]string{
		"service_account_json": testOnboardSAJSON,
	}))
}

func (s *OnboardSuite) ascReadyForSigning() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	s.Require().NoError(err)
	expiresAt := time.Now().AddDate(1, 0, 0)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "TaskTrooper Distribution"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     expiresAt,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	s.Require().NoError(err)
	s.asc.CreateCertificateResult = port.StoreCert{ID: "cert-1", Serial: "1", DER: der, ExpiresAt: expiresAt}
	s.asc.CreateProfileResult = port.StoreProfile{ID: "profile-1", Name: "profile-1", Content: []byte("fake-profile-bytes"), ExpiresAt: expiresAt}
}

func (s *OnboardSuite) TestOnboardIOSAlreadyRegisteredGoesStraightToTestReadyWithNoTask() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDID = "asc-app-1"
	s.asc.AppByBundleIDFound = true
	repoID := uuid.New()

	app, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "Example")

	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateTestReady, app.State)
	s.Empty(app.Checklist)
	s.Nil(app.OnboardingTaskID)
	s.Equal("asc-app-1", app.StoreAppID)
	s.Empty(s.tasks.Calls, "no manual steps left: no onboarding task should be opened")
}

func (s *OnboardSuite) TestOnboardIOSWithoutRecordOpensChecklistTask() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDFound = false
	repoID := uuid.New()

	app, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "Example")

	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateOnboarding, app.State)
	s.Require().Len(app.Checklist, 1)
	s.Equal("ios_app_record", app.Checklist[0].Key)
	s.False(app.Checklist[0].Done)
	s.Require().NotNil(app.OnboardingTaskID)

	s.Require().Len(s.tasks.Calls, 1)
	call := s.tasks.Calls[0]
	s.Equal(repoID, call.RepositoryID)
	s.Equal("Store onboarding: com.example.app (ios)", call.Request.Title)
	s.Empty(call.Request.TaskType, "left for CreateTask's own resolveTaskType to fill in the default type")
	s.Equal(domain.TaskPriorityHigh, call.Request.Priority)
	s.Equal(domain.TaskColumnTodo, call.Request.Column)
	s.Equal("system", call.Request.CreatedBy)
	s.Contains(call.Request.Description, "App Store Connect")
	s.Contains(call.Request.Description, "the system verifies each step automatically — no need to tick anything")
}

func (s *OnboardSuite) TestOnboardAndroidFreshHasBothChecklistItems() {
	s.storePlayCredential()
	s.play.AppExistsResult = false
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: false}
	repoID := uuid.New()

	app, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderGooglePlay, "com.example.android", "Example")

	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateOnboarding, app.State)
	s.Require().Len(app.Checklist, 2)
	var keys []string
	for _, item := range app.Checklist {
		keys = append(keys, item.Key)
		s.False(item.Done)
	}
	s.ElementsMatch([]string{"play_app_record", "play_first_upload"}, keys)
	s.Require().NotNil(app.OnboardingTaskID)
}

func (s *OnboardSuite) TestOnboardAndroidSkipsTrackInfoWhenAppDoesNotExist() {
	s.storePlayCredential()
	s.play.AppExistsResult = false

	s.play.TrackInfoErr = errors.New(`googleplay: app "com.example.android" not found`)
	repoID := uuid.New()

	app, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderGooglePlay, "com.example.android", "Example")

	s.Require().NoError(err, "a not-found TrackInfo error must never propagate for an unregistered package")
	s.Empty(s.play.TrackInfoCalls, "TrackInfo must not be called before the app record exists")
	s.Equal(domain.MobileStoreStateOnboarding, app.State)
	s.Require().Len(app.Checklist, 2)
	s.ElementsMatch([]string{"play_app_record", "play_first_upload"},
		[]string{app.Checklist[0].Key, app.Checklist[1].Key})
	s.Require().NotNil(app.OnboardingTaskID, "the guided onboarding task must still open")
}

func (s *OnboardSuite) TestOnboardPushesExactSecretNames() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDFound = false
	repoID := uuid.New()

	_, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "Example")
	s.Require().NoError(err)

	var names []string
	for _, call := range s.push.Calls {
		s.Equal(repoID, call.RepositoryID)
		names = append(names, call.Name)
	}
	s.ElementsMatch([]string{
		"IOS_DIST_CERT_P12", "IOS_CERT_PASSWORD", "IOS_PROFILE_B64",
		"ASC_KEY_ID", "ASC_ISSUER_ID", "ASC_KEY_P8",
	}, names)
}

func (s *OnboardSuite) TestOnboardCalledTwiceOpensOneTask() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDFound = false
	repoID := uuid.New()

	first, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "Example")
	s.Require().NoError(err)
	second, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "Example")
	s.Require().NoError(err)

	s.Equal(1, s.tasks.count(), "a second Onboard call must not open a second task")
	s.Equal(*first.OnboardingTaskID, *second.OnboardingTaskID)
}

func (s *OnboardSuite) TestOnboardFallsBackToRepositoryNameWhenAppNameEmpty() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDFound = false
	repoID := uuid.New()
	s.repos.set(domain.Repository{ID: repoID, Name: "Example Repo"})

	_, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "")

	s.Require().NoError(err)
	s.Require().Len(s.asc.EnsureBundleIDCalls, 1)
	s.Equal([2]string{"com.example.app", "Example Repo"}, s.asc.EnsureBundleIDCalls[0])
}

func (s *OnboardSuite) TestVerifyOnboardingMarksItemDoneAndAdvancesToTestReady() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDFound = false
	repoID := uuid.New()

	onboarded, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "Example")
	s.Require().NoError(err)
	s.Require().Equal(domain.MobileStoreStateOnboarding, onboarded.State)
	s.Require().NotNil(onboarded.OnboardingTaskID)

	s.asc.AppByBundleIDFound = true
	s.asc.AppByBundleIDID = "asc-app-9"

	verified, err := s.svc.VerifyOnboarding(context.Background(), repoID, domain.MobileStorePlatformIOS)

	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateTestReady, verified.State)
	s.Equal("asc-app-9", verified.StoreAppID)
	s.Require().Len(verified.Checklist, 1)
	s.True(verified.Checklist[0].Done)
	s.NotNil(verified.Checklist[0].VerifiedAt)

	s.Require().Len(s.comments.Calls, 1)
	s.Equal(*onboarded.OnboardingTaskID, s.comments.Calls[0].TaskID)
	s.Equal(repoID, s.comments.Calls[0].RepositoryID)
	s.Equal("system", s.comments.Calls[0].Request.AuthorType)
}

func (s *OnboardSuite) TestVerifyOnboardingAndroidBothItemsDoneAdvancesAndPostsComments() {
	s.storePlayCredential()
	s.play.AppExistsResult = false
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: false}
	repoID := uuid.New()

	onboarded, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderGooglePlay, "com.example.android", "Example")
	s.Require().NoError(err)
	s.Require().Len(onboarded.Checklist, 2)

	s.play.AppExistsResult = true
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true}

	verified, err := s.svc.VerifyOnboarding(context.Background(), repoID, domain.MobileStorePlatformAndroid)

	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateTestReady, verified.State)
	s.Require().Len(verified.Checklist, 2)
	for _, item := range verified.Checklist {
		s.True(item.Done, "item %s should be marked done", item.Key)
		s.NotNil(item.VerifiedAt)
	}
	s.Len(s.comments.Calls, 2, "one system comment per newly verified item")
}

func (s *OnboardSuite) TestVerifyOnboardingPersistsStateEvenWhenCommentFails() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDFound = false
	repoID := uuid.New()

	_, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "Example")
	s.Require().NoError(err)

	s.asc.AppByBundleIDFound = true
	s.asc.AppByBundleIDID = "asc-app-77"
	s.comments.Err = errors.New("board unavailable")

	verified, err := s.svc.VerifyOnboarding(context.Background(), repoID, domain.MobileStorePlatformIOS)

	s.Require().Error(err, "a comment failure must be surfaced, not swallowed")
	s.Equal(domain.MobileStoreStateTestReady, verified.State, "state must still advance despite the comment failure")
	s.True(verified.Checklist[0].Done)

	stored, getErr := s.apps.Get(context.Background(), repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(getErr)
	s.Equal(domain.MobileStoreStateTestReady, stored.State, "the advanced state must be persisted despite the comment error")
}

func (s *OnboardSuite) TestVerifyOnboardingNoChangeWhenNothingNewlyVerified() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDFound = false
	repoID := uuid.New()

	_, err := s.svc.Onboard(context.Background(), repoID, domain.DeployProviderAppStore, "com.example.app", "Example")
	s.Require().NoError(err)

	verified, err := s.svc.VerifyOnboarding(context.Background(), repoID, domain.MobileStorePlatformIOS)

	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateOnboarding, verified.State)
	s.False(verified.Checklist[0].Done)
	s.Empty(s.comments.Calls)
}

func (s *OnboardSuite) TestOnboardDemotesToOnboardingWhenTheIdentifierIsRePointed() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDID, s.asc.AppByBundleIDFound = "asc-app-1", true
	repoID := uuid.New()
	ctx := context.Background()

	first, err := s.svc.Onboard(ctx, repoID, domain.DeployProviderAppStore, "com.example.app", "MyApp")
	s.Require().NoError(err)
	s.Require().Equal(domain.MobileStoreStateTestReady, first.State)
	s.Require().Nil(first.OnboardingTaskID)

	s.asc.AppByBundleIDID, s.asc.AppByBundleIDFound = "", false

	second, err := s.svc.Onboard(ctx, repoID, domain.DeployProviderAppStore, "com.example.other", "MyApp")
	s.Require().NoError(err)
	s.Equal("com.example.other", second.Identifier)
	s.Equal(domain.MobileStoreStateOnboarding, second.State,
		"an unverified identifier must not keep the previous identifier's test_ready")
	s.Require().Len(second.Checklist, 1)
	s.False(second.Checklist[0].Done)
	s.NotNil(second.OnboardingTaskID, "the re-opened checklist gets a task to carry it")
}

func (s *OnboardSuite) TestOnboardRefusesToRePointALiveApp() {
	s.storeASCCredential()
	s.ascReadyForSigning()
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

	_, err = s.svc.Onboard(ctx, repoID, domain.DeployProviderAppStore, "com.example.other", "MyApp")
	s.Require().Error(err)
	s.ErrorIs(err, storeops.ErrIdentifierLocked)
	s.Contains(err.Error(), "com.example.app")

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal("com.example.app", got.Identifier, "the stored row is untouched")
	s.Equal(domain.MobileStoreStateLive, got.State)
	s.Empty(s.asc.EnsureBundleIDCalls, "the save is refused before anything is registered with Apple")
}

func (s *OnboardSuite) TestOnboardAllowsResavingALiveAppUnchanged() {
	s.storeASCCredential()
	s.ascReadyForSigning()
	s.asc.AppByBundleIDID, s.asc.AppByBundleIDFound = "asc-app-1", true
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

	got, err := s.svc.Onboard(ctx, repoID, domain.DeployProviderAppStore, "com.example.app", "MyApp")
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, got.State)
	s.Equal("com.example.app", got.Identifier)
}

func (s *OnboardSuite) TestVerifyOnboardingRejectsAnUnknownPlatformBeforeAnyLookup() {
	ctx := context.Background()
	_, err := s.svc.VerifyOnboarding(ctx, uuid.New(), "windows-phone")
	s.Require().Error(err)
	s.ErrorIs(err, storeops.ErrInvalidPlatform)
	s.Contains(err.Error(), "windows-phone")
	s.Empty(s.asc.AppByBundleIDCalls)
	s.Empty(s.play.AppExistsCalls)
}

func (s *OnboardSuite) TestEnsureIdentifierAllowedIsTheSeamSaveTargetCanActOn() {
	ctx := context.Background()
	repoID := uuid.New()

	s.Require().NoError(s.svc.EnsureIdentifierAllowed(ctx, repoID, domain.DeployProviderAppStore, "com.example.app"))

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.svc.EnsureIdentifierAllowed(ctx, repoID, domain.DeployProviderAppStore, "com.example.app"))

	err = s.svc.EnsureIdentifierAllowed(ctx, repoID, domain.DeployProviderAppStore, "com.example.other")
	s.Require().Error(err)
	s.ErrorIs(err, storeops.ErrIdentifierLocked)
	s.Contains(err.Error(), "com.example.app")
	s.Contains(err.Error(), "com.example.other")

	_, err = s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.svc.EnsureIdentifierAllowed(ctx, repoID, domain.DeployProviderGooglePlay, "com.example.android2"))

	err = s.svc.EnsureIdentifierAllowed(ctx, repoID, "not-a-store", "com.example.app")
	s.Require().Error(err)
	s.ErrorIs(err, storeops.ErrInvalidPlatform)
}
