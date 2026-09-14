package storeops_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
	"software.sslmate.com/src/go-pkcs12"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// SigningSuite exercises the system-owned signing assets: the iOS
// distribution certificate + provisioning profile pair, the Android upload
// keystore, and the renewal sweep. Everything it asserts is observable
// behaviour — what landed in the vault, what the store consoles were asked
// for, and what would be pushed to GitHub.
type SigningSuite struct {
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

func TestSigningSuite(t *testing.T) {
	suite.Run(t, new(SigningSuite))
}

func (s *SigningSuite) SetupTest() {
	s.prevKey, s.prevKeySet = os.LookupEnv("MCP_SECRETS_KEY")
	s.Require().NoError(os.Setenv("MCP_SECRETS_KEY", "test-storeops-signing-key"))

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

func (s *SigningSuite) TearDownTest() {
	if s.prevKeySet {
		os.Setenv("MCP_SECRETS_KEY", s.prevKey)
	} else {
		os.Unsetenv("MCP_SECRETS_KEY")
	}
}

const (
	testP8      = "-----BEGIN PRIVATE KEY-----\nfake-asc-key-material\n-----END PRIVATE KEY-----"
	testSAJSON  = `{"client_email":"deploy@project.iam.gserviceaccount.com","private_key":"fake"}`
	testKeyID   = "K1MEE23AB"
	testIssueID = "69a6de8b-fake-issuer"
)

// storeASCCredential puts a valid ASC credential in the vault — the
// precondition for any iOS signing work.
func (s *SigningSuite) storeASCCredential() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, map[string]string{
		"key_id":    testKeyID,
		"issuer_id": testIssueID,
		"p8":        testP8,
	}))
}

func (s *SigningSuite) storePlayCredential() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, map[string]string{
		"service_account_json": testSAJSON,
	}))
}

// ascIssuesCert scripts the fake App Store Connect to hand back a real,
// parseable certificate expiring at expiresAt — ASC signs the CSR we send,
// so the vault's p12 is only well-formed if the service pairs this DER with
// the key it generated.
func (s *SigningSuite) ascIssuesCert(id, serial string, expiresAt time.Time) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	s.Require().NoError(err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "TaskTrooper Distribution"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     expiresAt,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	s.Require().NoError(err)
	s.asc.CreateCertificateResult = port.StoreCert{ID: id, Serial: serial, DER: der, ExpiresAt: expiresAt}
}

func (s *SigningSuite) ascIssuesProfile(id string, content []byte, expiresAt time.Time) {
	s.asc.CreateProfileResult = port.StoreProfile{ID: id, Name: "profile-" + id, Content: content, ExpiresAt: expiresAt}
}

// vaultEnvelope mirrors the JSON the service seals into a keystore-shaped
// signing asset. The test decodes it independently so the assertions are
// against the stored bytes, not against the service's own helpers.
type vaultEnvelope struct {
	P12      string `json:"p12"`
	Password string `json:"password"`
}

// decodeEnvelope decrypts a stored signing asset and unmarshals its
// {"p12": ..., "password": ...} envelope.
func (s *SigningSuite) decodeEnvelope(kind, identifier string) (domain.SigningAsset, vaultEnvelope) {
	asset, err := s.signing.Get(context.Background(), kind, identifier)
	s.Require().NoError(err)
	plaintext, err := s.cipher.Decrypt(asset.Data)
	s.Require().NoError(err)
	var env vaultEnvelope
	s.Require().NoError(json.Unmarshal([]byte(plaintext), &env))
	return asset, env
}

// aliasOfStoredKeystore reports the key-entry alias a keytool/Gradle consumer
// would see for the keystore actually stored in the vault: the private key
// bag's friendlyName, or Java's default alias "1" when the encoder left the
// attribute off.
func (s *SigningSuite) aliasOfStoredKeystore(p12 []byte, password string) string {
	blocks, err := pkcs12.ToPEM(p12, password)
	s.Require().NoError(err)
	for _, block := range blocks {
		if block.Type != "PRIVATE KEY" {
			continue
		}
		if name := block.Headers["friendlyName"]; name != "" {
			return name
		}
	}
	return "1"
}

// An empty vault means the full generation path: a CSR to ASC, a profile for
// the bundle ID, and both artifacts sealed in the vault under their kinds.
func (s *SigningSuite) TestEnsureIOSSigningGeneratesCertAndProfileFromEmptyVault() {
	ctx := context.Background()
	s.storeASCCredential()
	certExpiry := time.Now().Add(365 * 24 * time.Hour).Truncate(time.Second)
	profileExpiry := time.Now().Add(300 * 24 * time.Hour).Truncate(time.Second)
	s.ascIssuesCert("CERT123", "0A1B2C3D", certExpiry)
	profileContent := []byte("fake .mobileprovision payload")
	s.ascIssuesProfile("PROF123", profileContent, profileExpiry)

	got, err := s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)

	// ASC was asked to sign exactly one CSR, and that CSR is a real one.
	s.Require().Len(s.asc.CreateCertificateCalls, 1)
	block, _ := pem.Decode(s.asc.CreateCertificateCalls[0])
	s.Require().NotNil(block, "the CSR must be PEM encoded")
	s.Equal("CERTIFICATE REQUEST", block.Type)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	s.Require().NoError(err)
	s.Require().NoError(csr.CheckSignature(), "the CSR must be signed by the generated key")
	s.Equal("TaskTrooper Distribution", csr.Subject.CommonName)

	// The profile was created for this bundle against the new certificate.
	s.Require().Len(s.asc.CreateProfileCalls, 1)
	s.Equal("com.example.app", s.asc.CreateProfileCalls[0][0])
	s.Equal("CERT123", s.asc.CreateProfileCalls[0][1])
	s.NotEmpty(s.asc.CreateProfileCalls[0][2], "the profile needs a name")

	// The certificate landed under (dist_cert, "") as an encrypted envelope.
	certAsset, env := s.decodeEnvelope(domain.SigningAssetDistCert, "")
	s.Equal("0A1B2C3D", certAsset.Serial)
	s.Require().NotNil(certAsset.ExpiresAt)
	s.WithinDuration(certExpiry, *certAsset.ExpiresAt, time.Second)
	s.NotContains(string(certAsset.Data), env.Password, "the vault row must not carry the password in the clear")

	// The p12 in the envelope opens with the password in the envelope.
	p12, err := base64.StdEncoding.DecodeString(env.P12)
	s.Require().NoError(err)
	key, cert, err := pkcs12.Decode(p12, env.Password)
	s.Require().NoError(err, "the stored p12 must open with the stored password")
	s.NotNil(key)
	s.Equal("TaskTrooper Distribution", cert.Subject.CommonName)

	// The profile is stored raw, under (profile, bundleID).
	profileAsset, err := s.signing.Get(ctx, domain.SigningAssetProfile, "com.example.app")
	s.Require().NoError(err)
	storedProfile, err := s.cipher.Decrypt(profileAsset.Data)
	s.Require().NoError(err)
	s.Equal(profileContent, []byte(storedProfile))
	s.NotContains(string(profileAsset.Data), string(profileContent), "the profile must be encrypted at rest")
	s.Require().NotNil(profileAsset.ExpiresAt)
	s.WithinDuration(profileExpiry, *profileAsset.ExpiresAt, time.Second)

	// And the secret map is exactly the six documented keys.
	s.Equal(map[string]string{
		"IOS_DIST_CERT_P12": env.P12,
		"IOS_CERT_PASSWORD": env.Password,
		"IOS_PROFILE_B64":   base64.StdEncoding.EncodeToString(profileContent),
		"ASC_KEY_ID":        testKeyID,
		"ASC_ISSUER_ID":     testIssueID,
		"ASC_KEY_P8":        base64.StdEncoding.EncodeToString([]byte(testP8)),
	}, got)
}

// A second call with valid assets must not touch ASC at all — signing
// material is generated once and reused, never re-minted per deploy.
func (s *SigningSuite) TestEnsureIOSSigningIsIdempotent() {
	ctx := context.Background()
	s.storeASCCredential()
	s.ascIssuesCert("CERT123", "0A1B2C3D", time.Now().Add(365*24*time.Hour))
	s.ascIssuesProfile("PROF123", []byte("profile-one"), time.Now().Add(300*24*time.Hour))

	first, err := s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)

	second, err := s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)

	s.Equal(1, len(s.asc.CreateCertificateCalls), "a valid certificate must be reused, not re-minted")
	s.Equal(1, len(s.asc.CreateProfileCalls), "a valid profile must be reused, not re-created")
	s.Equal(first, second, "the reused assets must produce the same secrets")
}

// A signing asset with no recorded expiry must be treated as already due for
// renewal, not eternally valid — controller ruling #6: "Treat an asset as
// needing renewal when ExpiresAt is nil or before time.Now()." A zero ASC
// expiry (ensureProfile only sets ExpiresAt when the store hands back a
// non-zero one) is exactly how a nil-expiry profile ends up in the vault, and
// it must not silently escape renewal forever.
func (s *SigningSuite) TestEnsureIOSSigningRegeneratesAssetsWithNilExpiresAt() {
	ctx := context.Background()
	s.storeASCCredential()
	s.ascIssuesCert("CERT123", "0A1B2C3D", time.Now().Add(365*24*time.Hour))
	s.ascIssuesProfile("PROF123", []byte("profile-one"), time.Now().Add(300*24*time.Hour))

	_, err := s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)
	s.Require().Len(s.asc.CreateCertificateCalls, 1)
	s.Require().Len(s.asc.CreateProfileCalls, 1)

	// Baseline: a future ExpiresAt still short-circuits — no new ASC calls.
	_, err = s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)
	s.Require().Len(s.asc.CreateCertificateCalls, 1, "a certificate with a future expiry must still be reused")
	s.Require().Len(s.asc.CreateProfileCalls, 1, "a profile with a future expiry must still be reused")

	// Now simulate what a zero ASC expiry produces: a stored profile with no
	// ExpiresAt at all, everything else untouched.
	stored, err := s.signing.Get(ctx, domain.SigningAssetProfile, "com.example.app")
	s.Require().NoError(err)
	stored.ExpiresAt = nil
	_, err = s.signing.Upsert(ctx, stored)
	s.Require().NoError(err)

	s.ascIssuesProfile("PROF456", []byte("profile-two"), time.Now().Add(300*24*time.Hour))
	_, err = s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)

	s.Len(s.asc.CreateCertificateCalls, 1, "the certificate is still valid and must not be re-minted")
	s.Len(s.asc.CreateProfileCalls, 2, "a nil-expiry profile must be treated as due for renewal, not eternally valid")
}

// Each bundle gets its own profile, but they all share the one team
// distribution certificate — Apple caps how many a team may hold.
func (s *SigningSuite) TestEnsureIOSSigningSharesOneCertificateAcrossBundles() {
	ctx := context.Background()
	s.storeASCCredential()
	s.ascIssuesCert("CERT123", "0A1B2C3D", time.Now().Add(365*24*time.Hour))
	s.ascIssuesProfile("PROF123", []byte("profile-one"), time.Now().Add(300*24*time.Hour))

	first, err := s.svc.EnsureIOSSigning(ctx, "com.example.one")
	s.Require().NoError(err)
	s.ascIssuesProfile("PROF456", []byte("profile-two"), time.Now().Add(300*24*time.Hour))
	second, err := s.svc.EnsureIOSSigning(ctx, "com.example.two")
	s.Require().NoError(err)

	s.Len(s.asc.CreateCertificateCalls, 1, "the team distribution certificate is shared")
	s.Len(s.asc.CreateProfileCalls, 2, "each bundle needs its own profile")
	s.Equal(first["IOS_DIST_CERT_P12"], second["IOS_DIST_CERT_P12"])
	s.NotEqual(first["IOS_PROFILE_B64"], second["IOS_PROFILE_B64"])
}

// Without a stored ASC credential there is nothing to sign with: fail before
// generating a key, and leave the vault empty.
func (s *SigningSuite) TestEnsureIOSSigningFailsWithoutASCCredential() {
	_, err := s.svc.EnsureIOSSigning(context.Background(), "com.example.app")

	s.Require().Error(err)
	s.Empty(s.asc.CreateCertificateCalls)
	_, getErr := s.signing.Get(context.Background(), domain.SigningAssetDistCert, "")
	s.Error(getErr, "nothing may be stored when the credential is missing")
}

// The Android upload keystore is a self-signed PKCS#12 the system owns for
// 25 years — Play rejects a shorter-lived upload key.
func (s *SigningSuite) TestEnsureAndroidKeystoreGeneratesSelfSignedKeystore() {
	ctx := context.Background()
	s.storePlayCredential()

	got, err := s.svc.EnsureAndroidKeystore(ctx, "com.example.app")
	s.Require().NoError(err)

	asset, env := s.decodeEnvelope(domain.SigningAssetUploadKeystore, "com.example.app")
	s.NotContains(string(asset.Data), env.Password, "the vault row must not carry the password in the clear")

	p12, err := base64.StdEncoding.DecodeString(env.P12)
	s.Require().NoError(err)
	key, cert, err := pkcs12.Decode(p12, env.Password)
	s.Require().NoError(err, "the stored keystore must open with the stored password")
	s.NotNil(key)
	s.Equal(cert.Subject.String(), cert.Issuer.String(), "the upload key is self-signed")
	// CheckSignatureFrom enforces CA-chain constraints (IsCA, KeyUsageCertSign
	// on the "parent") which a leaf cert deliberately does not carry — verify
	// the actual self-signature directly instead.
	s.Require().NoError(cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature))
	s.WithinDuration(time.Now().AddDate(25, 0, 0), cert.NotAfter, 48*time.Hour, "the upload key must last 25 years")
	s.False(cert.IsCA, "the upload cert is a leaf signing cert used by keytool/Gradle/apksigner, not a CA")
	s.Equal(x509.KeyUsageDigitalSignature, cert.KeyUsage, "the upload cert needs only digital signature usage, no cert-signing")
	s.Require().NotNil(asset.ExpiresAt)
	s.WithinDuration(cert.NotAfter, *asset.ExpiresAt, time.Second)

	// The alias must be whatever a consumer decoding the STORED keystore
	// would actually see — anything else and Gradle signs with nothing.
	wantAlias := s.aliasOfStoredKeystore(p12, env.Password)
	s.NotEmpty(wantAlias)
	s.Equal(map[string]string{
		"ANDROID_UPLOAD_KEYSTORE_B64": env.P12,
		"ANDROID_KEYSTORE_PASSWORD":   env.Password,
		"ANDROID_KEY_ALIAS":           wantAlias,
		"ANDROID_KEY_PASSWORD":        env.Password,
		"PLAY_SA_JSON":                base64.StdEncoding.EncodeToString([]byte(testSAJSON)),
	}, got)
}

// Regenerating an upload keystore would lock the app out of Play forever —
// a second call must return the very same bytes.
func (s *SigningSuite) TestEnsureAndroidKeystoreIsIdempotent() {
	ctx := context.Background()
	s.storePlayCredential()

	first, err := s.svc.EnsureAndroidKeystore(ctx, "com.example.app")
	s.Require().NoError(err)

	second, err := s.svc.EnsureAndroidKeystore(ctx, "com.example.app")
	s.Require().NoError(err)

	s.Equal(first, second, "the upload keystore must never be regenerated")
	_, env := s.decodeEnvelope(domain.SigningAssetUploadKeystore, "com.example.app")
	s.Equal(env.P12, first["ANDROID_UPLOAD_KEYSTORE_B64"])
}

func (s *SigningSuite) TestEnsureAndroidKeystoreFailsWithoutPlayCredential() {
	_, err := s.svc.EnsureAndroidKeystore(context.Background(), "com.example.app")

	s.Require().Error(err)
	_, getErr := s.signing.Get(context.Background(), domain.SigningAssetUploadKeystore, "com.example.app")
	s.Error(getErr, "nothing may be stored when the credential is missing")
}

// registerApp puts a mobile store registry row in place so the renewal sweep
// can find the repositories an expiring asset belongs to.
func (s *SigningSuite) registerApp(repositoryID uuid.UUID, platform, identifier string) {
	_, err := s.apps.Upsert(context.Background(), domain.MobileStoreApp{
		RepositoryID: repositoryID,
		Platform:     platform,
		Identifier:   identifier,
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)
}

// pushedTo collects the secrets pushed to one repository.
func (s *SigningSuite) pushedTo(repositoryID uuid.UUID) map[string]string {
	out := map[string]string{}
	for _, call := range s.push.Calls {
		if call.RepositoryID == repositoryID {
			out[call.Name] = call.Value
		}
	}
	return out
}

// An expiring team certificate invalidates every profile issued against it,
// so the sweep re-mints the certificate once and re-issues a profile — and
// re-pushes secrets — for every iOS repository.
func (s *SigningSuite) TestRenewExpiringSigningRenewsCertAndPushesEveryIOSRepo() {
	ctx := context.Background()
	s.storeASCCredential()
	repoA, repoB := uuid.New(), uuid.New()
	s.registerApp(repoA, domain.MobileStorePlatformIOS, "com.example.one")
	s.registerApp(repoB, domain.MobileStorePlatformIOS, "com.example.two")

	// Both bundles are set up against a certificate that expires in 10 days.
	soon := time.Now().Add(10 * 24 * time.Hour)
	s.ascIssuesCert("CERT_OLD", "OLD", soon)
	s.ascIssuesProfile("PROF_A_OLD", []byte("profile-a-old"), soon)
	_, err := s.svc.EnsureIOSSigning(ctx, "com.example.one")
	s.Require().NoError(err)
	s.ascIssuesProfile("PROF_B_OLD", []byte("profile-b-old"), time.Now().Add(300*24*time.Hour))
	_, err = s.svc.EnsureIOSSigning(ctx, "com.example.two")
	s.Require().NoError(err)

	// The sweep looks 30 days ahead: the certificate is in scope.
	s.ascIssuesCert("CERT_NEW", "NEW", time.Now().Add(365*24*time.Hour))
	s.asc.CreateProfileResult = port.StoreProfile{ID: "PROF_NEW", Content: []byte("profile-new"), ExpiresAt: time.Now().Add(300 * 24 * time.Hour)}

	err = s.svc.RenewExpiringSigning(ctx, time.Now().Add(30*24*time.Hour))
	s.Require().NoError(err)

	s.Len(s.asc.CreateCertificateCalls, 2, "the expiring certificate is re-minted exactly once")
	certAsset, env := s.decodeEnvelope(domain.SigningAssetDistCert, "")
	s.Equal("NEW", certAsset.Serial)

	// Every profile issued against the old certificate was re-issued.
	s.Len(s.asc.CreateProfileCalls, 4, "both bundles get a profile against the new certificate")
	for _, call := range s.asc.CreateProfileCalls[2:] {
		s.Equal("CERT_NEW", call[1])
	}

	// Both repositories got the full, refreshed secret set.
	for _, repoID := range []uuid.UUID{repoA, repoB} {
		pushed := s.pushedTo(repoID)
		s.Equal(env.P12, pushed["IOS_DIST_CERT_P12"])
		s.Equal(env.Password, pushed["IOS_CERT_PASSWORD"])
		s.Equal(base64.StdEncoding.EncodeToString([]byte("profile-new")), pushed["IOS_PROFILE_B64"])
		s.Equal(testKeyID, pushed["ASC_KEY_ID"])
		s.Equal(testIssueID, pushed["ASC_ISSUER_ID"])
		s.Equal(base64.StdEncoding.EncodeToString([]byte(testP8)), pushed["ASC_KEY_P8"])
	}
}

// One repository's push failure must not cost the others their renewal.
func (s *SigningSuite) TestRenewExpiringSigningKeepsGoingWhenOneRepoFails() {
	ctx := context.Background()
	s.storeASCCredential()
	repoA, repoB := uuid.New(), uuid.New()
	s.registerApp(repoA, domain.MobileStorePlatformIOS, "com.example.one")
	s.registerApp(repoB, domain.MobileStorePlatformIOS, "com.example.two")

	soon := time.Now().Add(10 * 24 * time.Hour)
	s.ascIssuesCert("CERT_OLD", "OLD", soon)
	s.ascIssuesProfile("PROF_A_OLD", []byte("profile-a-old"), soon)
	_, err := s.svc.EnsureIOSSigning(ctx, "com.example.one")
	s.Require().NoError(err)
	s.ascIssuesProfile("PROF_B_OLD", []byte("profile-b-old"), soon)
	_, err = s.svc.EnsureIOSSigning(ctx, "com.example.two")
	s.Require().NoError(err)

	s.ascIssuesCert("CERT_NEW", "NEW", time.Now().Add(365*24*time.Hour))
	s.ascIssuesProfile("PROF_NEW", []byte("profile-new"), time.Now().Add(300*24*time.Hour))
	s.push.ErrFor = map[uuid.UUID]error{repoA: errors.New("github says no")}

	err = s.svc.RenewExpiringSigning(ctx, time.Now().Add(30*24*time.Hour))

	s.Require().Error(err)
	s.Contains(err.Error(), repoA.String(), "the failing repository must be named")
	s.Contains(err.Error(), "github says no")
	s.Len(s.pushedTo(repoB), 6, "the healthy repository still gets its six secrets")
}

// An expiring upload keystore renews against the Android registry row.
func (s *SigningSuite) TestRenewExpiringSigningRenewsAndroidKeystore() {
	ctx := context.Background()
	s.storePlayCredential()
	repoID := uuid.New()
	s.registerApp(repoID, domain.MobileStorePlatformAndroid, "com.example.app")

	_, err := s.svc.EnsureAndroidKeystore(ctx, "com.example.app")
	s.Require().NoError(err)
	_, before := s.decodeEnvelope(domain.SigningAssetUploadKeystore, "com.example.app")

	// A 25-year keystore only shows up in a sweep that looks 26 years out.
	err = s.svc.RenewExpiringSigning(ctx, time.Now().AddDate(26, 0, 0))
	s.Require().NoError(err)

	_, after := s.decodeEnvelope(domain.SigningAssetUploadKeystore, "com.example.app")
	s.NotEqual(before.P12, after.P12, "the expiring keystore is regenerated")
	pushed := s.pushedTo(repoID)
	s.Equal(after.P12, pushed["ANDROID_UPLOAD_KEYSTORE_B64"])
	s.Equal(after.Password, pushed["ANDROID_KEYSTORE_PASSWORD"])
	s.Equal(after.Password, pushed["ANDROID_KEY_PASSWORD"])
	s.NotEmpty(pushed["ANDROID_KEY_ALIAS"])
	s.Equal(base64.StdEncoding.EncodeToString([]byte(testSAJSON)), pushed["PLAY_SA_JSON"])
}

// Nothing expiring means nothing to do: no store calls, no secret churn.
func (s *SigningSuite) TestRenewExpiringSigningIsANoopWhenNothingExpires() {
	ctx := context.Background()
	s.storeASCCredential()
	repoID := uuid.New()
	s.registerApp(repoID, domain.MobileStorePlatformIOS, "com.example.app")
	s.ascIssuesCert("CERT123", "0A1B2C3D", time.Now().Add(365*24*time.Hour))
	s.ascIssuesProfile("PROF123", []byte("profile"), time.Now().Add(300*24*time.Hour))
	_, err := s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)

	err = s.svc.RenewExpiringSigning(ctx, time.Now().Add(30*24*time.Hour))

	s.Require().NoError(err)
	s.Len(s.asc.CreateCertificateCalls, 1)
	s.Len(s.asc.CreateProfileCalls, 1)
	s.Empty(s.push.Calls, "nothing expiring means nothing to push")
}

// An expiring asset nobody uses is not an error — there is simply no
// repository to renew it for.
func (s *SigningSuite) TestRenewExpiringSigningSkipsAssetsWithNoRegisteredApp() {
	ctx := context.Background()
	s.storeASCCredential()
	s.ascIssuesCert("CERT_OLD", "OLD", time.Now().Add(10*24*time.Hour))
	s.ascIssuesProfile("PROF_OLD", []byte("profile"), time.Now().Add(10*24*time.Hour))
	_, err := s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)

	err = s.svc.RenewExpiringSigning(ctx, time.Now().Add(30*24*time.Hour))

	s.Require().NoError(err)
	s.Empty(s.push.Calls)
	s.Len(s.asc.CreateCertificateCalls, 1, "no registered app means no renewal work")
}

// The vault must never hold the generated key material in the clear.
func (s *SigningSuite) TestSigningAssetsAreEncryptedAtRest() {
	ctx := context.Background()
	s.storeASCCredential()
	s.storePlayCredential()
	s.ascIssuesCert("CERT123", "0A1B2C3D", time.Now().Add(365*24*time.Hour))
	s.ascIssuesProfile("PROF123", []byte("profile"), time.Now().Add(300*24*time.Hour))

	_, err := s.svc.EnsureIOSSigning(ctx, "com.example.app")
	s.Require().NoError(err)
	_, err = s.svc.EnsureAndroidKeystore(ctx, "com.example.app")
	s.Require().NoError(err)

	for _, kind := range []struct{ kind, identifier string }{
		{domain.SigningAssetDistCert, ""},
		{domain.SigningAssetProfile, "com.example.app"},
		{domain.SigningAssetUploadKeystore, "com.example.app"},
	} {
		asset, err := s.signing.Get(ctx, kind.kind, kind.identifier)
		s.Require().NoError(err)
		s.False(strings.HasPrefix(string(asset.Data), "{"), "%s must not be stored as plaintext JSON", kind.kind)
		_, decErr := s.cipher.Decrypt(asset.Data)
		s.Require().NoError(decErr, "%s must decrypt with the service cipher", kind.kind)
	}
}
