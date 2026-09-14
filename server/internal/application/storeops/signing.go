package storeops

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/google/uuid"
	"software.sslmate.com/src/go-pkcs12"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// GitHub Actions secret names the mobile release workflows read. They are the
// contract between this service and the workflow templates — renaming one
// here breaks every generated workflow.
const (
	secretIOSDistCertP12  = "IOS_DIST_CERT_P12"
	secretIOSCertPassword = "IOS_CERT_PASSWORD"
	secretIOSProfileB64   = "IOS_PROFILE_B64"
	secretASCKeyID        = "ASC_KEY_ID"
	secretASCIssuerID     = "ASC_ISSUER_ID"
	secretASCKeyP8        = "ASC_KEY_P8"

	secretAndroidKeystoreB64      = "ANDROID_UPLOAD_KEYSTORE_B64"
	secretAndroidKeystorePassword = "ANDROID_KEYSTORE_PASSWORD"
	secretAndroidKeyAlias         = "ANDROID_KEY_ALIAS"
	secretAndroidKeyPassword      = "ANDROID_KEY_PASSWORD"
	secretPlaySAJSON              = "PLAY_SA_JSON"
)

const (
	// signingKeyBits is the RSA size for every key this service mints. Apple
	// and Play both accept 2048.
	signingKeyBits = 2048
	// distCertCommonName is the subject Apple's developer console shows for
	// the distribution certificate the system owns.
	distCertCommonName = "TaskTrooper Distribution"
	// uploadKeyValidityYears is how long an Android upload key lives. Play
	// wants an upload key that outlives the app; keytool's own guidance for
	// a release key is 25 years.
	uploadKeyValidityYears = 25
	// defaultKeystoreAlias is the alias Java reports for a PKCS#12 key entry
	// that carries no friendlyName attribute.
	defaultKeystoreAlias = "1"
)

// signingEnvelope is the JSON sealed into a keystore-shaped signing asset:
// the PKCS#12 bytes together with the password that opens them, so the two
// can never drift apart across a rotation. CertID is set only for the iOS
// distribution certificate — App Store Connect needs that ID to issue a
// profile against the certificate, and a team may hold only a couple of
// distribution certificates, so re-minting one just to re-learn its ID is
// not an option.
type signingEnvelope struct {
	P12      string `json:"p12"`      // base64 of the PKCS#12 bytes
	Password string `json:"password"` // 32 hex characters
	CertID   string `json:"cert_id,omitempty"`
}

// EnsureIOSSigning guarantees a valid dist cert + profile for bundleID exist
// in the vault, creating or renewing through ASC as needed, and returns the
// GitHub secret map to push: IOS_DIST_CERT_P12 (base64), IOS_CERT_PASSWORD,
// IOS_PROFILE_B64, ASC_KEY_ID, ASC_ISSUER_ID, ASC_KEY_P8 (base64).
func (s *Service) EnsureIOSSigning(ctx context.Context, bundleID string) (map[string]string, error) {
	return s.ensureIOSSigning(ctx, bundleID, time.Now())
}

// EnsureAndroidKeystore guarantees an upload keystore for packageName exists
// in the vault and returns secrets: ANDROID_UPLOAD_KEYSTORE_B64,
// ANDROID_KEYSTORE_PASSWORD, ANDROID_KEY_ALIAS, ANDROID_KEY_PASSWORD,
// PLAY_SA_JSON (base64).
func (s *Service) EnsureAndroidKeystore(ctx context.Context, packageName string) (map[string]string, error) {
	return s.ensureAndroidKeystore(ctx, packageName, time.Now())
}

// ensureIOSSigning is EnsureIOSSigning with an explicit staleness horizon:
// an asset is reused only when it outlives renewBefore. Callers pass "now"
// for the ordinary path and the sweep deadline when renewing ahead of time.
func (s *Service) ensureIOSSigning(ctx context.Context, bundleID string, renewBefore time.Time) (map[string]string, error) {
	if bundleID == "" {
		return nil, errors.New("storeops: bundle id is required to ensure iOS signing")
	}
	cred, err := s.credential(ctx, domain.StoreCredentialASC)
	if err != nil {
		return nil, err
	}

	certAsset, env, err := s.ensureDistCert(ctx, renewBefore)
	if err != nil {
		return nil, err
	}
	profile, err := s.ensureProfile(ctx, bundleID, env.CertID, certAsset.UpdatedAt, renewBefore)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		secretIOSDistCertP12:  env.P12,
		secretIOSCertPassword: env.Password,
		secretIOSProfileB64:   base64.StdEncoding.EncodeToString(profile),
		secretASCKeyID:        cred.Data["key_id"],
		secretASCIssuerID:     cred.Data["issuer_id"],
		secretASCKeyP8:        base64.StdEncoding.EncodeToString([]byte(cred.Data["p8"])),
	}, nil
}

// ensureDistCert returns the team's distribution certificate, minting a new
// one through App Store Connect when the vault holds none or the stored one
// does not outlive renewBefore. There is exactly one per Apple team, so it
// lives under identifier "".
func (s *Service) ensureDistCert(ctx context.Context, renewBefore time.Time) (domain.SigningAsset, signingEnvelope, error) {
	asset, err := s.signing.Get(ctx, domain.SigningAssetDistCert, "")
	switch {
	case err == nil:
		env, err := s.openEnvelope(asset)
		if err != nil {
			return domain.SigningAsset{}, signingEnvelope{}, err
		}
		// A stored certificate without its ASC ID cannot back a new
		// profile — treat it as unusable and mint a fresh one.
		if outlives(asset, renewBefore) && env.CertID != "" {
			return asset, env, nil
		}
	case errors.Is(err, port.ErrNotFound):
		// Nothing in the vault yet: fall through and generate.
	default:
		return domain.SigningAsset{}, signingEnvelope{}, fmt.Errorf("storeops: loading distribution certificate: %w", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, signingKeyBits)
	if err != nil {
		return domain.SigningAsset{}, signingEnvelope{}, fmt.Errorf("storeops: generating distribution key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: distCertCommonName},
		SignatureAlgorithm: x509.SHA256WithRSA,
	}, key)
	if err != nil {
		return domain.SigningAsset{}, signingEnvelope{}, fmt.Errorf("storeops: building certificate request: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	client, err := s.asc(ctx)
	if err != nil {
		return domain.SigningAsset{}, signingEnvelope{}, err
	}
	cert, err := client.CreateCertificate(ctx, csrPEM)
	if err != nil {
		return domain.SigningAsset{}, signingEnvelope{}, fmt.Errorf("storeops: creating distribution certificate: %w", err)
	}
	parsed, err := x509.ParseCertificate(cert.DER)
	if err != nil {
		return domain.SigningAsset{}, signingEnvelope{}, fmt.Errorf("storeops: parsing issued certificate: %w", err)
	}

	env, err := sealPKCS12(key, parsed)
	if err != nil {
		return domain.SigningAsset{}, signingEnvelope{}, err
	}
	env.CertID = cert.ID

	expiresAt := cert.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = parsed.NotAfter
	}
	stored, err := s.sealAsset(ctx, domain.SigningAsset{
		Kind:      domain.SigningAssetDistCert,
		Serial:    cert.Serial,
		ExpiresAt: &expiresAt,
	}, env)
	if err != nil {
		return domain.SigningAsset{}, signingEnvelope{}, err
	}
	return stored, env, nil
}

// ensureProfile returns the raw .mobileprovision for bundleID, asking App
// Store Connect for a new one when the vault holds none, when the stored one
// does not outlive renewBefore, or when it predates the certificate it must
// embed — a profile issued against a replaced certificate signs nothing.
func (s *Service) ensureProfile(ctx context.Context, bundleID, certID string, certUpdatedAt, renewBefore time.Time) ([]byte, error) {
	asset, err := s.signing.Get(ctx, domain.SigningAssetProfile, bundleID)
	switch {
	case err == nil:
		if outlives(asset, renewBefore) && !asset.UpdatedAt.Before(certUpdatedAt) {
			return s.openRaw(asset)
		}
	case errors.Is(err, port.ErrNotFound):
		// Nothing in the vault yet: fall through and create one.
	default:
		return nil, fmt.Errorf("storeops: loading provisioning profile: %w", err)
	}

	if certID == "" {
		return nil, errors.New("storeops: cannot create a provisioning profile without a distribution certificate")
	}
	client, err := s.asc(ctx)
	if err != nil {
		return nil, err
	}
	name := provisioningProfileName(bundleID, time.Now())
	profile, err := client.CreateProfile(ctx, bundleID, certID, name)
	if err != nil {
		return nil, fmt.Errorf("storeops: creating provisioning profile for %s: %w", bundleID, err)
	}

	stored := domain.SigningAsset{Kind: domain.SigningAssetProfile, Identifier: bundleID}
	if !profile.ExpiresAt.IsZero() {
		expiresAt := profile.ExpiresAt
		stored.ExpiresAt = &expiresAt
	}
	// The profile is not a keystore: it goes into the vault as the raw
	// .mobileprovision bytes, encrypted, with no envelope around it.
	if _, err := s.sealRaw(ctx, stored, profile.Content); err != nil {
		return nil, err
	}
	return profile.Content, nil
}

// ensureAndroidKeystore is EnsureAndroidKeystore with an explicit staleness
// horizon — see ensureIOSSigning.
func (s *Service) ensureAndroidKeystore(ctx context.Context, packageName string, renewBefore time.Time) (map[string]string, error) {
	if packageName == "" {
		return nil, errors.New("storeops: package name is required to ensure an upload keystore")
	}
	cred, err := s.credential(ctx, domain.StoreCredentialGooglePlay)
	if err != nil {
		return nil, err
	}

	env, err := s.ensureUploadKeystore(ctx, packageName, renewBefore)
	if err != nil {
		return nil, err
	}
	p12, err := base64.StdEncoding.DecodeString(env.P12)
	if err != nil {
		return nil, fmt.Errorf("storeops: decoding stored upload keystore: %w", err)
	}

	return map[string]string{
		secretAndroidKeystoreB64:      env.P12,
		secretAndroidKeystorePassword: env.Password,
		secretAndroidKeyAlias:         keystoreAlias(p12, env.Password),
		// PKCS#12 protects the key entry with the store password.
		secretAndroidKeyPassword: env.Password,
		secretPlaySAJSON:         base64.StdEncoding.EncodeToString([]byte(cred.Data["service_account_json"])),
	}, nil
}

// ensureUploadKeystore returns packageName's upload keystore, generating a
// self-signed 25-year one when the vault holds none or the stored one does
// not outlive renewBefore.
func (s *Service) ensureUploadKeystore(ctx context.Context, packageName string, renewBefore time.Time) (signingEnvelope, error) {
	asset, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, packageName)
	switch {
	case err == nil:
		env, err := s.openEnvelope(asset)
		if err != nil {
			return signingEnvelope{}, err
		}
		if outlives(asset, renewBefore) {
			return env, nil
		}
	case errors.Is(err, port.ErrNotFound):
		// Nothing in the vault yet: fall through and generate.
	default:
		return signingEnvelope{}, fmt.Errorf("storeops: loading upload keystore: %w", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, signingKeyBits)
	if err != nil {
		return signingEnvelope{}, fmt.Errorf("storeops: generating upload key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return signingEnvelope{}, fmt.Errorf("storeops: generating upload certificate serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: packageName, Organization: []string{"TaskTrooper"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(uploadKeyValidityYears, 0, 0),
		// This is a leaf signing certificate keytool/Gradle/apksigner use to
		// sign an AAB/APK upload — not a CA. It never issues other
		// certificates, so it carries no CA bit and no cert-signing usage.
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return signingEnvelope{}, fmt.Errorf("storeops: self-signing upload certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return signingEnvelope{}, fmt.Errorf("storeops: parsing upload certificate: %w", err)
	}

	env, err := sealPKCS12(key, cert)
	if err != nil {
		return signingEnvelope{}, err
	}
	expiresAt := cert.NotAfter
	if _, err := s.sealAsset(ctx, domain.SigningAsset{
		Kind:       domain.SigningAssetUploadKeystore,
		Identifier: packageName,
		Serial:     cert.SerialNumber.String(),
		ExpiresAt:  &expiresAt,
	}, env); err != nil {
		return signingEnvelope{}, err
	}
	return env, nil
}

// RenewExpiringSigning re-generates any signing asset expiring before
// deadline and re-pushes secrets for every repo whose store target uses it.
// One repository's failure never aborts the sweep — every failure is
// collected and returned together.
func (s *Service) RenewExpiringSigning(ctx context.Context, deadline time.Time) error {
	expiring, err := s.signing.ListExpiring(ctx, deadline)
	if err != nil {
		return fmt.Errorf("storeops: listing expiring signing assets: %w", err)
	}
	var targets []renewTarget
	if len(expiring) > 0 {
		apps, err := s.apps.ListAll(ctx)
		if err != nil {
			return fmt.Errorf("storeops: listing mobile store apps: %w", err)
		}
		targets = renewTargets(expiring, apps)
	}
	// Anything renewed on an earlier sweep whose push never landed is retried
	// here even though ListExpiring no longer reports it — see pendingPush.
	targets = s.withPendingPushes(targets)
	if len(targets) == 0 {
		return nil
	}
	if s.pushSecret == nil {
		return errors.New("storeops: secret push is not configured")
	}

	var errs []error
	for _, target := range targets {
		// Claim the target as owed a push before touching anything. A mint
		// that succeeds and a push that then fails is the dangerous shape:
		// the fresh asset's expiry has moved out of ListExpiring's window, so
		// nothing would ever bring this repository back here, and the vault
		// would hold a new certificate while Actions still ran on the old
		// one — every build failing until a human re-saved the deploy target.
		s.markPushPending(target)

		var values map[string]string
		var err error
		switch target.platform {
		case domain.MobileStorePlatformIOS:
			values, err = s.ensureIOSSigning(ctx, target.identifier, deadline)
		case domain.MobileStorePlatformAndroid:
			values, err = s.ensureAndroidKeystore(ctx, target.identifier, deadline)
		}
		if err != nil {
			// The mint failed, so nothing new exists to strand: the vault and
			// GitHub still hold the same old asset, and if it really is
			// expiring ListExpiring keeps returning it anyway. Release the
			// claim — holding it would park a permanently failing target in
			// the retry set and re-report its error on every single sweep.
			s.clearPushPending(target)
			errs = append(errs, fmt.Errorf("storeops: renewing %s signing for repository %s: %w", target.platform, target.repositoryID, err))
			continue
		}
		if err := s.pushSecrets(ctx, target.repositoryID, values); err != nil {
			errs = append(errs, err)
			continue
		}
		s.clearPushPending(target)
	}
	return errors.Join(errs...)
}

// markPushPending records that target's freshly minted secrets have not made
// it to GitHub yet, so the next sweep retries them regardless of what
// ListExpiring says.
//
// This is deliberately in-memory, matching Monitor.notified: making it
// durable would mean a schema change, and the window it leaves — a process
// restart between the failed push and the very next sweep, seconds or a
// minute later — falls back to the pre-existing manual recovery (re-save the
// deploy target). The bug it closes was unconditional and permanent.
func (s *Service) markPushPending(target renewTarget) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingPush == nil {
		s.pendingPush = map[renewTarget]bool{}
	}
	s.pendingPush[target] = true
}

func (s *Service) clearPushPending(target renewTarget) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pendingPush, target)
}

// withPendingPushes appends every target still owed a push to targets,
// skipping the ones already there.
func (s *Service) withPendingPushes(targets []renewTarget) []renewTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pendingPush) == 0 {
		return targets
	}
	have := make(map[renewTarget]bool, len(targets))
	for _, t := range targets {
		have[t] = true
	}
	pending := make([]renewTarget, 0, len(s.pendingPush))
	for t := range s.pendingPush {
		if !have[t] {
			pending = append(pending, t)
		}
	}
	// Map iteration order is random; keep the sweep's work deterministic.
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].repositoryID != pending[j].repositoryID {
			return pending[i].repositoryID.String() < pending[j].repositoryID.String()
		}
		if pending[i].platform != pending[j].platform {
			return pending[i].platform < pending[j].platform
		}
		return pending[i].identifier < pending[j].identifier
	})
	return append(targets, pending...)
}

// renewTarget is one repository's app on one store — the unit the sweep
// renews and pushes secrets for.
type renewTarget struct {
	repositoryID uuid.UUID
	platform     string
	identifier   string
}

// renewTargets maps expiring assets onto the registry rows they affect,
// de-duplicated so a repository whose certificate and profile both expire is
// renewed once. The team's distribution certificate backs every iOS app, so
// it pulls in all of them; profiles and keystores only their own identifier.
func renewTargets(expiring []domain.SigningAsset, apps []domain.MobileStoreApp) []renewTarget {
	var out []renewTarget
	seen := map[renewTarget]bool{}
	add := func(app domain.MobileStoreApp) {
		target := renewTarget{repositoryID: app.RepositoryID, platform: app.Platform, identifier: app.Identifier}
		if seen[target] {
			return
		}
		seen[target] = true
		out = append(out, target)
	}
	for _, asset := range expiring {
		for _, app := range apps {
			switch asset.Kind {
			case domain.SigningAssetDistCert:
				if app.Platform == domain.MobileStorePlatformIOS {
					add(app)
				}
			case domain.SigningAssetProfile:
				if app.Platform == domain.MobileStorePlatformIOS && app.Identifier == asset.Identifier {
					add(app)
				}
			case domain.SigningAssetUploadKeystore:
				if app.Platform == domain.MobileStorePlatformAndroid && app.Identifier == asset.Identifier {
					add(app)
				}
			}
		}
	}
	return out
}

// pushSecrets writes one repository's secret map in a stable order, stopping
// at that repository's first failure so a broken repository is reported
// rather than half-written and forgotten.
func (s *Service) pushSecrets(ctx context.Context, repositoryID uuid.UUID, values map[string]string) error {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := s.pushSecret(ctx, repositoryID, name, values[name]); err != nil {
			return fmt.Errorf("storeops: pushing %s to repository %s: %w", name, repositoryID, err)
		}
	}
	return nil
}

// outlives reports whether asset is still good at horizon. An asset with no
// recorded expiry is treated as already due for renewal, not eternally
// valid — controller ruling #6: "Treat an asset as needing renewal when
// ExpiresAt is nil or before time.Now()."
func outlives(asset domain.SigningAsset, horizon time.Time) bool {
	return asset.ExpiresAt != nil && asset.ExpiresAt.After(horizon)
}

// sealPKCS12 wraps key and cert in a password-protected PKCS#12 and returns
// the envelope to store. The password is generated here and only ever lives
// next to the bytes it opens.
func sealPKCS12(key *rsa.PrivateKey, cert *x509.Certificate) (signingEnvelope, error) {
	password, err := randomPassword()
	if err != nil {
		return signingEnvelope{}, err
	}
	p12, err := pkcs12.Modern.Encode(key, cert, nil, password)
	if err != nil {
		return signingEnvelope{}, fmt.Errorf("storeops: encoding PKCS#12: %w", err)
	}
	return signingEnvelope{P12: base64.StdEncoding.EncodeToString(p12), Password: password}, nil
}

// sealAsset encrypts an envelope and upserts it as asset's payload.
func (s *Service) sealAsset(ctx context.Context, asset domain.SigningAsset, env signingEnvelope) (domain.SigningAsset, error) {
	plaintext, err := json.Marshal(env)
	if err != nil {
		return domain.SigningAsset{}, fmt.Errorf("storeops: encoding %s envelope: %w", asset.Kind, err)
	}
	return s.sealRaw(ctx, asset, plaintext)
}

// sealRaw encrypts plaintext and upserts it as asset's payload. Nothing in
// this file ever hands the store bytes that have not been through the
// cipher, and none of it is ever logged.
func (s *Service) sealRaw(ctx context.Context, asset domain.SigningAsset, plaintext []byte) (domain.SigningAsset, error) {
	encrypted, err := s.cipher.Encrypt(string(plaintext))
	if err != nil {
		return domain.SigningAsset{}, fmt.Errorf("storeops: encrypting %s: %w", asset.Kind, err)
	}
	asset.Data = encrypted
	stored, err := s.signing.Upsert(ctx, asset)
	if err != nil {
		return domain.SigningAsset{}, fmt.Errorf("storeops: persisting %s: %w", asset.Kind, err)
	}
	return stored, nil
}

// openRaw decrypts an asset sealed by sealRaw back into its plaintext bytes.
// It is openEnvelope's counterpart for the assets that carry no envelope —
// today only the provisioning profile, which goes into the vault as the raw
// .mobileprovision. Every read of the vault goes through one of the two, so
// no caller reaches for s.cipher directly.
func (s *Service) openRaw(asset domain.SigningAsset) ([]byte, error) {
	plaintext, err := s.cipher.Decrypt(asset.Data)
	if err != nil {
		return nil, fmt.Errorf("storeops: decrypting %s: %w", asset.Kind, err)
	}
	return []byte(plaintext), nil
}

// openEnvelope decrypts a keystore-shaped asset back into its envelope.
func (s *Service) openEnvelope(asset domain.SigningAsset) (signingEnvelope, error) {
	plaintext, err := s.openRaw(asset)
	if err != nil {
		return signingEnvelope{}, err
	}
	var env signingEnvelope
	if err := json.Unmarshal(plaintext, &env); err != nil {
		return signingEnvelope{}, fmt.Errorf("storeops: decoding %s envelope: %w", asset.Kind, err)
	}
	return env, nil
}

// provisioningProfileName returns the name a new provisioning profile for
// bundleID is issued under. ASC profile names are unique per team and a
// replaced profile keeps occupying its old name (this service never
// deletes the ASC-side profile it replaces, only the vault's copy of it),
// so each one is stamped with the UTC second it was minted — fastlane's
// sigh applies the same defensive pattern (append a timestamp) when it
// finds a name already taken. now is threaded through as a parameter
// rather than read internally so the two-calls-differ contract is
// verifiable without a real clock.
func provisioningProfileName(bundleID string, now time.Time) string {
	return fmt.Sprintf("TaskTrooper %s %s", bundleID, now.UTC().Format("20060102-150405"))
}

// randomPassword returns 32 hex characters — the password protecting one
// generated PKCS#12. It is stored with the bytes it opens and never logged.
func randomPassword() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("storeops: generating keystore password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// keystoreAlias reports the alias a keytool or Gradle consumer actually sees
// for p12's key entry: its friendlyName attribute, or Java's default name
// for an unnamed entry. go-pkcs12 writes no friendlyName, so the alias we
// hand a build is read back out of the bytes we stored instead of assumed.
// ToPEM is the only exported API that surfaces bag attributes; its
// deprecation is about the PEM private keys it emits, which we ignore.
func keystoreAlias(p12 []byte, password string) string {
	blocks, err := pkcs12.ToPEM(p12, password)
	if err != nil {
		return defaultKeystoreAlias
	}
	for _, block := range blocks {
		if block.Type != "PRIVATE KEY" {
			continue
		}
		if name := block.Headers["friendlyName"]; name != "" {
			return name
		}
	}
	return defaultKeystoreAlias
}
