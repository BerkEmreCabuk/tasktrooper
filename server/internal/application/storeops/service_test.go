package storeops_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

// ServiceSuite exercises the credential vault and the app-registry read path.
// It sets MCP_SECRETS_KEY itself and restores whatever was there before —
// same save/restore discipline as secrets.CipherSuite — so it never leaks
// state into other packages' tests.
type ServiceSuite struct {
	suite.Suite

	prevKey    string
	prevKeySet bool

	creds   *fakeCredentialStore
	apps    *fakeMobileStoreAppStore
	signing *fakeSigningAssetStore
	asc     *fakeASC
	play    *fakePlay
	push    *fakePushSecret
	cipher  *secrets.Cipher

	svc *storeops.Service
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) SetupTest() {
	s.prevKey, s.prevKeySet = os.LookupEnv("MCP_SECRETS_KEY")
	s.Require().NoError(os.Setenv("MCP_SECRETS_KEY", "test-storeops-secrets-key"))

	cipher, err := secrets.NewCipherFromEnv()
	s.Require().NoError(err)
	s.cipher = cipher

	s.creds = newFakeCredentialStore()
	s.apps = newFakeMobileStoreAppStore()
	s.signing = newFakeSigningAssetStore()
	s.asc = &fakeASC{}
	s.play = &fakePlay{}
	s.push = &fakePushSecret{}

	s.svc = storeops.NewService(storeops.Deps{
		Credentials: s.creds,
		Apps:        s.apps,
		Signing:     s.signing,
		Cipher:      s.cipher,
		NewASC:      newFakeASCFactory(s.asc, nil),
		NewPlay:     newFakePlayFactory(s.play, nil),
		PushSecret:  s.push.Push,
	})
}

func (s *ServiceSuite) TearDownTest() {
	if s.prevKeySet {
		os.Setenv("MCP_SECRETS_KEY", s.prevKey)
	} else {
		os.Unsetenv("MCP_SECRETS_KEY")
	}
}

func (s *ServiceSuite) ascCredential() map[string]string {
	return map[string]string{
		"key_id":    "K1MEE23AB",
		"issuer_id": "69a6de8b-fake-issuer",
		"p8":        "-----BEGIN PRIVATE KEY-----\nfake-key-material\n-----END PRIVATE KEY-----",
	}
}

func (s *ServiceSuite) playCredential() map[string]string {
	return map[string]string{
		"service_account_json": `{"client_email":"deploy@project.iam.gserviceaccount.com","private_key":"fake"}`,
	}
}

// SaveCredential must validate against the store console before persisting
// anything: an invalid credential is rejected and nothing is written, even
// encrypted.
func (s *ServiceSuite) TestSaveCredentialRejectsInvalidCredentialAndPersistsNothing() {
	s.asc.ValidateAuthErr = errors.New("401 unauthorized")

	err := s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, s.ascCredential())

	s.Require().Error(err)
	s.Equal(1, s.asc.ValidateAuthCalls, "ValidateAuth must be called before persisting")
	s.Equal(0, s.creds.count(), "an invalid credential must not be stored")
}

func (s *ServiceSuite) TestSaveCredentialRejectsInvalidGooglePlayCredentialAndPersistsNothing() {
	s.play.ValidateAuthErr = errors.New("invalid_grant")

	err := s.svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, s.playCredential())

	s.Require().Error(err)
	s.Equal(1, s.play.ValidateAuthCalls, "ValidateAuth must be called before persisting")
	s.Equal(0, s.creds.count(), "an invalid credential must not be stored")
}

// The persisted payload must be encrypted: the stored bytes are not the
// plaintext JSON, and decrypting them round-trips back to the original map.
func (s *ServiceSuite) TestSaveCredentialEncryptsThePersistedPayload() {
	data := s.ascCredential()

	err := s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, data)
	s.Require().NoError(err)
	s.Equal(1, s.asc.ValidateAuthCalls)

	stored, _, err := s.creds.Get(context.Background(), domain.StoreCredentialASC)
	s.Require().NoError(err)

	plainJSON, err := json.Marshal(data)
	s.Require().NoError(err)
	s.NotEqual(plainJSON, stored, "stored bytes must not be the plaintext JSON")
	s.False(strings.Contains(string(stored), "fake-key-material"), "the raw secret must not appear in the stored bytes")

	decrypted, err := s.cipher.Decrypt(stored)
	s.Require().NoError(err)
	var roundTripped map[string]string
	s.Require().NoError(json.Unmarshal([]byte(decrypted), &roundTripped))
	s.Equal(data, roundTripped, "decrypting the stored payload must round-trip to the original map")
}

// An unknown provider is rejected before any client is built or persisted.
func (s *ServiceSuite) TestSaveCredentialRejectsUnknownProvider() {
	err := s.svc.SaveCredential(context.Background(), "unknown_console", map[string]string{"x": "y"})

	s.Require().Error(err)
	s.Equal(0, s.creds.count())
	s.Equal(0, s.asc.ValidateAuthCalls)
	s.Equal(0, s.play.ValidateAuthCalls)
}

// Credentials() is the safe listing: it never carries a payload, only
// whether a provider is configured and when it was last written.
func (s *ServiceSuite) TestCredentialsNeverReturnsPayloads() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, s.ascCredential()))

	views, err := s.svc.Credentials(context.Background())

	s.Require().NoError(err)
	s.Require().Len(views, 2)
	s.Equal(domain.StoreCredentialASC, views[0].Provider)
	s.True(views[0].Configured)
	s.False(views[0].UpdatedAt.IsZero())
}

// Credentials() must list every KNOWN provider, not only the ones with a
// stored row — otherwise a declared-but-unsaved provider is indistinguishable
// from an unknown one, and the web UI (Task 13) can never show the "not yet
// configured" state.
func (s *ServiceSuite) TestCredentialsListsKnownProvidersEvenWhenUnconfigured() {
	views, err := s.svc.Credentials(context.Background())

	s.Require().NoError(err)
	s.Require().Len(views, 2)
	var providers []string
	for _, v := range views {
		providers = append(providers, v.Provider)
		s.False(v.Configured, "%s must be reported unconfigured on a fresh vault", v.Provider)
		s.True(v.UpdatedAt.IsZero(), "%s must carry a zero UpdatedAt when unconfigured", v.Provider)
	}
	s.ElementsMatch([]string{domain.StoreCredentialASC, domain.StoreCredentialGooglePlay}, providers)
}

// Saving one provider flips only that provider's view to configured; the
// other known provider stays unconfigured.
func (s *ServiceSuite) TestCredentialsFlipsOnlyTheSavedProvider() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, s.ascCredential()))

	views, err := s.svc.Credentials(context.Background())

	s.Require().NoError(err)
	s.Require().Len(views, 2)
	byProvider := map[string]storeops.CredentialView{}
	for _, v := range views {
		byProvider[v.Provider] = v
	}
	s.Require().Contains(byProvider, domain.StoreCredentialASC)
	s.Require().Contains(byProvider, domain.StoreCredentialGooglePlay)
	s.True(byProvider[domain.StoreCredentialASC].Configured)
	s.False(byProvider[domain.StoreCredentialASC].UpdatedAt.IsZero())
	s.False(byProvider[domain.StoreCredentialGooglePlay].Configured)
	s.True(byProvider[domain.StoreCredentialGooglePlay].UpdatedAt.IsZero())
}

func (s *ServiceSuite) TestCredentialsListsBothConfiguredProviders() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, s.ascCredential()))
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, s.playCredential()))

	views, err := s.svc.Credentials(context.Background())

	s.Require().NoError(err)
	s.Require().Len(views, 2)
	var providers []string
	for _, v := range views {
		providers = append(providers, v.Provider)
		s.True(v.Configured)
	}
	s.ElementsMatch([]string{domain.StoreCredentialASC, domain.StoreCredentialGooglePlay}, providers)
}

// DeleteCredential removes the stored credential entirely — the provider
// falls back to the unconfigured view, not off the list (ruling 1: the list
// is always every known provider).
func (s *ServiceSuite) TestDeleteCredentialRemovesIt() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, s.playCredential()))
	s.Require().Equal(1, s.creds.count())

	err := s.svc.DeleteCredential(context.Background(), domain.StoreCredentialGooglePlay)

	s.Require().NoError(err)
	s.Equal(0, s.creds.count())
	views, err := s.svc.Credentials(context.Background())
	s.Require().NoError(err)
	s.Require().Len(views, 2)
	for _, v := range views {
		s.False(v.Configured)
	}
}

// AppsByRepository returns exactly the registry rows for that repository,
// leaving other repositories' rows out.
func (s *ServiceSuite) TestAppsByRepositoryReturnsTheRegistryRows() {
	ctx := context.Background()
	repoID := uuid.New()
	otherRepoID := uuid.New()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.ios",
		State:        domain.MobileStoreStateOnboarding,
	})
	s.Require().NoError(err)
	_, err = s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateUnregistered,
	})
	s.Require().NoError(err)
	_, err = s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: otherRepoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.other.ios",
		State:        domain.MobileStoreStateUnregistered,
	})
	s.Require().NoError(err)

	got, err := s.svc.AppsByRepository(ctx, repoID)

	s.Require().NoError(err)
	s.Require().Len(got, 2)
	var platforms []string
	for _, a := range got {
		s.Equal(repoID, a.RepositoryID)
		platforms = append(platforms, a.Platform)
	}
	s.ElementsMatch([]string{domain.MobileStorePlatformIOS, domain.MobileStorePlatformAndroid}, platforms)
}

func (s *ServiceSuite) TestAppsByRepositoryEmptyWhenNoneRegistered() {
	got, err := s.svc.AppsByRepository(context.Background(), uuid.New())

	s.Require().NoError(err)
	s.Empty(got)
}

// TestSaveCredentialWithNilCipherReturnsErrorInsteadOfPanicking covers the
// runtime's own graceful-degrade default: when MCP_SECRETS_KEY/SERVER_API_KEY
// is absent, runtime.go still builds storeOpsSvc with a nil cipher rather
// than refusing to boot. secrets.Cipher.Encrypt/Decrypt dereference their
// receiver unconditionally, so a nil *Cipher used to panic every hit of
// SaveCredential (PUT /v1/store/credentials/:provider) instead of failing
// the one request cleanly.
func TestSaveCredentialWithNilCipherReturnsErrorInsteadOfPanicking(t *testing.T) {
	creds := newFakeCredentialStore()
	asc := &fakeASC{}
	svc := storeops.NewService(storeops.Deps{
		Credentials: creds,
		NewASC:      newFakeASCFactory(asc, nil),
		Cipher:      nil,
	})

	err := svc.SaveCredential(context.Background(), domain.StoreCredentialASC, map[string]string{
		"key_id": "K1MEE23AB", "issuer_id": "69a6de8b-fake-issuer", "p8": "fake-key-material",
	})

	if err == nil {
		t.Fatal("expected an error when the cipher is nil, got nil")
	}
	if got := creds.count(); got != 0 {
		t.Fatalf("nothing must be persisted when the cipher is unavailable, got %d rows", got)
	}
}

// TestVerifyOnboardingWithNilCipherReturnsErrorInsteadOfPanicking covers the
// decrypt side: credential() (reached via asc()/play()) also dereferenced a
// nil cipher unconditionally. Exercised through VerifyOnboarding — the same
// entry point storeops.Monitor.Sweep calls on every onboarding row, so a nil
// cipher used to crash the monitor's sweep goroutine, not just one HTTP
// request.
func TestVerifyOnboardingWithNilCipherReturnsErrorInsteadOfPanicking(t *testing.T) {
	creds := newFakeCredentialStore()
	// Seed a raw row directly, bypassing SaveCredential (which itself now
	// requires a cipher) so the decrypt path is reached in isolation.
	if err := creds.Set(context.Background(), domain.StoreCredentialASC, []byte("irrelevant-bytes")); err != nil {
		t.Fatal(err)
	}
	apps := newFakeMobileStoreAppStore()
	repoID := uuid.New()
	if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		State:        domain.MobileStoreStateOnboarding,
	}); err != nil {
		t.Fatal(err)
	}

	svc := storeops.NewService(storeops.Deps{
		Credentials: creds,
		Apps:        apps,
		NewASC:      newFakeASCFactory(&fakeASC{}, nil),
		Cipher:      nil,
	})

	_, err := svc.VerifyOnboarding(context.Background(), repoID, domain.MobileStorePlatformIOS)
	if err == nil {
		t.Fatal("expected an error when the cipher is nil, got nil")
	}
}

// TestDeleteCredentialRejectsUnknownProvider covers the same input
// validation SaveCredential already had: DELETE /v1/store/credentials/junk
// must not report 204 as if a real provider's credential was removed.
func TestDeleteCredentialRejectsUnknownProvider(t *testing.T) {
	svc := storeops.NewService(storeops.Deps{Credentials: newFakeCredentialStore()})

	err := svc.DeleteCredential(context.Background(), "unknown_console")
	if err == nil {
		t.Fatal("expected an error for an unknown provider, got nil")
	}
}
