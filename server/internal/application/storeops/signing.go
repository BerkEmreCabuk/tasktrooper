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
	signingKeyBits = 2048

	distCertCommonName = "TaskTrooper Distribution"

	uploadKeyValidityYears = 25

	defaultKeystoreAlias = "1"
)

type signingEnvelope struct {
	P12      string `json:"p12"`
	Password string `json:"password"`
	CertID   string `json:"cert_id,omitempty"`
}

func (s *Service) EnsureIOSSigning(ctx context.Context, bundleID string) (map[string]string, error) {
	return s.ensureIOSSigning(ctx, bundleID, time.Now())
}

func (s *Service) EnsureAndroidKeystore(ctx context.Context, packageName string) (map[string]string, error) {
	return s.ensureAndroidKeystore(ctx, packageName, time.Now())
}

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

func (s *Service) ensureDistCert(ctx context.Context, renewBefore time.Time) (domain.SigningAsset, signingEnvelope, error) {
	asset, err := s.signing.Get(ctx, domain.SigningAssetDistCert, "")
	switch {
	case err == nil:
		env, err := s.openEnvelope(asset)
		if err != nil {
			return domain.SigningAsset{}, signingEnvelope{}, err
		}

		if outlives(asset, renewBefore) && env.CertID != "" {
			return asset, env, nil
		}
	case errors.Is(err, port.ErrNotFound):

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

func (s *Service) ensureProfile(ctx context.Context, bundleID, certID string, certUpdatedAt, renewBefore time.Time) ([]byte, error) {
	asset, err := s.signing.Get(ctx, domain.SigningAssetProfile, bundleID)
	switch {
	case err == nil:
		if outlives(asset, renewBefore) && !asset.UpdatedAt.Before(certUpdatedAt) {
			return s.openRaw(asset)
		}
	case errors.Is(err, port.ErrNotFound):

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

	if _, err := s.sealRaw(ctx, stored, profile.Content); err != nil {
		return nil, err
	}
	return profile.Content, nil
}

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

		secretAndroidKeyPassword: env.Password,
		secretPlaySAJSON:         base64.StdEncoding.EncodeToString([]byte(cred.Data["service_account_json"])),
	}, nil
}

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

	targets = s.withPendingPushes(targets)
	if len(targets) == 0 {
		return nil
	}
	if s.pushSecret == nil {
		return errors.New("storeops: secret push is not configured")
	}

	var errs []error
	for _, target := range targets {

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

type renewTarget struct {
	repositoryID uuid.UUID
	platform     string
	identifier   string
}

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

func outlives(asset domain.SigningAsset, horizon time.Time) bool {
	return asset.ExpiresAt != nil && asset.ExpiresAt.After(horizon)
}

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

func (s *Service) sealAsset(ctx context.Context, asset domain.SigningAsset, env signingEnvelope) (domain.SigningAsset, error) {
	plaintext, err := json.Marshal(env)
	if err != nil {
		return domain.SigningAsset{}, fmt.Errorf("storeops: encoding %s envelope: %w", asset.Kind, err)
	}
	return s.sealRaw(ctx, asset, plaintext)
}

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

func (s *Service) openRaw(asset domain.SigningAsset) ([]byte, error) {
	plaintext, err := s.cipher.Decrypt(asset.Data)
	if err != nil {
		return nil, fmt.Errorf("storeops: decrypting %s: %w", asset.Kind, err)
	}
	return []byte(plaintext), nil
}

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

func provisioningProfileName(bundleID string, now time.Time) string {
	return fmt.Sprintf("TaskTrooper %s %s", bundleID, now.UTC().Format("20060102-150405"))
}

func randomPassword() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("storeops: generating keystore password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

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
