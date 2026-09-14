package appstore

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// testCredential generates a fresh P-256 key pair, PEM-encodes the private
// key as PKCS8 (mirroring what Apple hands out as a .p8 download), and
// returns a domain.StoreCredential built from it plus the matching public
// key so tests can independently verify signatures produced by the client.
func testCredential(t *testing.T) (domain.StoreCredential, *ecdsa.PublicKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("x509.MarshalPKCS8PrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	cred := domain.StoreCredential{
		Provider: domain.StoreCredentialASC,
		Data: map[string]string{
			"key_id":    "KEY123",
			"issuer_id": "ISSUER456",
			"p8":        string(pemBytes),
		},
	}
	return cred, &priv.PublicKey
}

// newTestClient builds a Client wired to a fresh httptest server running
// handler, and returns the public key matching the credential's private key
// so callers can verify signed JWTs.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *ecdsa.PublicKey) {
	t.Helper()
	cred, pub := testCredential(t)
	c, err := New(cred)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c.SetBaseURL(srv.URL)
	return c, pub
}

func TestNewRejectsMissingCredentialFields(t *testing.T) {
	_, err := New(domain.StoreCredential{Provider: domain.StoreCredentialASC, Data: map[string]string{}})
	if err == nil {
		t.Fatal("expected error for missing credential fields, got nil")
	}
}

func TestNewRejectsInvalidP8(t *testing.T) {
	_, err := New(domain.StoreCredential{
		Provider: domain.StoreCredentialASC,
		Data: map[string]string{
			"key_id":    "KEY123",
			"issuer_id": "ISSUER456",
			"p8":        "not a real pem",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid p8, got nil")
	}
}

// TestRequestsCarryValidES256JWT verifies every request carries an
// Authorization: Bearer <jwt> header, that the claims decode with the
// expected iss/aud, and — critically — that the signature is raw r||s (64
// bytes) rather than ASN.1 DER, by actually validating it with ecdsa.Verify
// against the public key. A decode-only assertion would miss a DER-encoded
// signature entirely, which is the single most common way to get ES256
// wrong.
func TestRequestsCarryValidES256JWT(t *testing.T) {
	var gotAuth string
	c, pub := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			gotAuth = r.Header.Get("Authorization")
			w.Write([]byte(`{"data":[]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	if err := c.ValidateAuth(context.Background()); err != nil {
		t.Fatalf("ValidateAuth: %v", err)
	}

	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("Authorization header = %q, want Bearer prefix", gotAuth)
	}
	token := strings.TrimPrefix(gotAuth, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3 (header.claims.signature)", len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decoding header: %v", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", header.Alg)
	}
	if header.Kid != "KEY123" {
		t.Errorf("kid = %q, want KEY123", header.Kid)
	}
	if header.Typ != "JWT" {
		t.Errorf("typ = %q, want JWT", header.Typ)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding claims: %v", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Aud string `json:"aud"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "ISSUER456" {
		t.Errorf("iss = %q, want ISSUER456", claims.Iss)
	}
	if claims.Aud != "appstoreconnect-v1" {
		t.Errorf("aud = %q, want appstoreconnect-v1", claims.Aud)
	}
	if claims.Exp-claims.Iat != 15*60 {
		t.Errorf("exp-iat = %d, want 900 (15 minutes)", claims.Exp-claims.Iat)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("signature length = %d bytes, want 64 (raw r||s, not ASN.1 DER)", len(sig))
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))
	if !ecdsa.Verify(pub, digest[:], r, s) {
		t.Fatal("ES256 signature does not validate against the credential's public key")
	}
}

func TestAppByBundleIDFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			if got := r.URL.Query().Get("filter[bundleId]"); got != "com.example.app" {
				t.Errorf("filter[bundleId] = %q, want com.example.app", got)
			}
			w.Write([]byte(`{"data":[{"type":"apps","id":"app123","attributes":{"bundleId":"com.example.app"}}]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	id, found, err := c.AppByBundleID(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("AppByBundleID: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if id != "app123" {
		t.Fatalf("id = %q, want app123", id)
	}
}

func TestAppByBundleIDNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			w.Write([]byte(`{"data":[]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	id, found, err := c.AppByBundleID(context.Background(), "com.example.missing")
	if err != nil {
		t.Fatalf("AppByBundleID: %v", err)
	}
	if found {
		t.Fatal("found = true, want false")
	}
	if id != "" {
		t.Fatalf("id = %q, want empty", id)
	}
}

func TestEnsureBundleIDAlreadyRegistered(t *testing.T) {
	postCalled := false
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bundleIds":
			if got := r.URL.Query().Get("filter[identifier]"); got != "com.example.app" {
				t.Errorf("filter[identifier] = %q, want com.example.app", got)
			}
			w.Write([]byte(`{"data":[{"type":"bundleIds","id":"bid123","attributes":{"identifier":"com.example.app"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bundleIds":
			postCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	if err := c.EnsureBundleID(context.Background(), "com.example.app", "Example"); err != nil {
		t.Fatalf("EnsureBundleID: %v", err)
	}
	if postCalled {
		t.Fatal("POST /v1/bundleIds was called for an already-registered bundle id")
	}
}

func TestEnsureBundleIDRegistersWhenAbsent(t *testing.T) {
	var captured struct {
		Data struct {
			Type       string `json:"type"`
			Attributes struct {
				Identifier string `json:"identifier"`
				Name       string `json:"name"`
				Platform   string `json:"platform"`
			} `json:"attributes"`
		} `json:"data"`
	}
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bundleIds":
			w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bundleIds":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Errorf("decoding POST body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"data":{"type":"bundleIds","id":"bid456"}}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	if err := c.EnsureBundleID(context.Background(), "com.example.app", "Example"); err != nil {
		t.Fatalf("EnsureBundleID: %v", err)
	}
	if captured.Data.Type != "bundleIds" {
		t.Errorf("data.type = %q, want bundleIds", captured.Data.Type)
	}
	if captured.Data.Attributes.Identifier != "com.example.app" {
		t.Errorf("identifier = %q, want com.example.app", captured.Data.Attributes.Identifier)
	}
	if captured.Data.Attributes.Name != "Example" {
		t.Errorf("name = %q, want Example", captured.Data.Attributes.Name)
	}
	if captured.Data.Attributes.Platform != "IOS" {
		t.Errorf("platform = %q, want IOS", captured.Data.Attributes.Platform)
	}
}

func TestCreateCertificate(t *testing.T) {
	csrPEM := []byte("-----BEGIN CERTIFICATE REQUEST-----\nfakecsrbytes\n-----END CERTIFICATE REQUEST-----\n")
	derContent := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	var captured struct {
		Data struct {
			Type       string `json:"type"`
			Attributes struct {
				CertificateType string `json:"certificateType"`
				CsrContent      string `json:"csrContent"`
			} `json:"attributes"`
		} `json:"data"`
	}

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/certificates":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Errorf("decoding POST body: %v", err)
			}
			resp := map[string]any{
				"data": map[string]any{
					"type": "certificates",
					"id":   "cert789",
					"attributes": map[string]any{
						"serialNumber":       "SERIAL01",
						"certificateContent": base64.StdEncoding.EncodeToString(derContent),
						"expirationDate":     "2027-06-01T00:00:00.000+0000",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	cert, err := c.CreateCertificate(context.Background(), csrPEM)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	if captured.Data.Type != "certificates" {
		t.Errorf("data.type = %q, want certificates", captured.Data.Type)
	}
	if captured.Data.Attributes.CertificateType != "IOS_DISTRIBUTION" {
		t.Errorf("certificateType = %q, want IOS_DISTRIBUTION", captured.Data.Attributes.CertificateType)
	}
	wantCsr := base64.StdEncoding.EncodeToString(csrPEM)
	if captured.Data.Attributes.CsrContent != wantCsr {
		t.Errorf("csrContent = %q, want %q", captured.Data.Attributes.CsrContent, wantCsr)
	}

	if cert.ID != "cert789" {
		t.Errorf("ID = %q, want cert789", cert.ID)
	}
	if cert.Serial != "SERIAL01" {
		t.Errorf("Serial = %q, want SERIAL01", cert.Serial)
	}
	if string(cert.DER) != string(derContent) {
		t.Errorf("DER = %v, want %v", cert.DER, derContent)
	}
	wantExpiry := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	if !cert.ExpiresAt.Equal(wantExpiry) {
		t.Errorf("ExpiresAt = %v, want %v", cert.ExpiresAt, wantExpiry)
	}
}

// TestCreateProfile also exercises the internal bundleId-resource-id lookup
// (GET /v1/bundleIds?filter[identifier]=) that CreateProfile must perform
// before POSTing /v1/profiles: the ASC profiles relationship requires the
// bundleIds resource's opaque id, not the bundle identifier string that
// EnsureBundleID/CreateProfile callers pass around.
func TestCreateProfile(t *testing.T) {
	profileContent := []byte("fake mobileprovision bytes")
	var captured struct {
		Data struct {
			Type       string `json:"type"`
			Attributes struct {
				Name        string `json:"name"`
				ProfileType string `json:"profileType"`
			} `json:"attributes"`
			Relationships struct {
				BundleID struct {
					Data struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					} `json:"data"`
				} `json:"bundleId"`
				Certificates struct {
					Data []struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					} `json:"data"`
				} `json:"certificates"`
			} `json:"relationships"`
		} `json:"data"`
	}

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bundleIds":
			if got := r.URL.Query().Get("filter[identifier]"); got != "com.example.app" {
				t.Errorf("filter[identifier] = %q, want com.example.app", got)
			}
			w.Write([]byte(`{"data":[{"type":"bundleIds","id":"bid123"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/profiles":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Errorf("decoding POST body: %v", err)
			}
			resp := map[string]any{
				"data": map[string]any{
					"type": "profiles",
					"id":   "prof555",
					"attributes": map[string]any{
						"name":           "Example Distribution",
						"profileContent": base64.StdEncoding.EncodeToString(profileContent),
						"expirationDate": "2027-06-01T00:00:00.000+0000",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	profile, err := c.CreateProfile(context.Background(), "com.example.app", "cert789", "Example Distribution")
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	if captured.Data.Type != "profiles" {
		t.Errorf("data.type = %q, want profiles", captured.Data.Type)
	}
	if captured.Data.Attributes.ProfileType != "IOS_APP_STORE" {
		t.Errorf("profileType = %q, want IOS_APP_STORE", captured.Data.Attributes.ProfileType)
	}
	if captured.Data.Relationships.BundleID.Data.ID != "bid123" {
		t.Errorf("relationships.bundleId.data.id = %q, want bid123", captured.Data.Relationships.BundleID.Data.ID)
	}
	if got := captured.Data.Relationships.Certificates.Data; len(got) != 1 || got[0].ID != "cert789" {
		t.Errorf("relationships.certificates.data = %+v, want one entry with id cert789", got)
	}

	if profile.ID != "prof555" {
		t.Errorf("ID = %q, want prof555", profile.ID)
	}
	if profile.Name != "Example Distribution" {
		t.Errorf("Name = %q, want %q", profile.Name, "Example Distribution")
	}
	if string(profile.Content) != string(profileContent) {
		t.Errorf("Content = %q, want %q", profile.Content, profileContent)
	}
	wantExpiry := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	if !profile.ExpiresAt.Equal(wantExpiry) {
		t.Errorf("ExpiresAt = %v, want %v", profile.ExpiresAt, wantExpiry)
	}
}

func TestLatestVersion(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/app123/appStoreVersions":
			if got := r.URL.Query().Get("limit"); got != "" {
				t.Errorf("limit = %q, want it not to be sent at all", got)
			}
			w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"v1","attributes":{"versionString":"1.2.3","appStoreState":"READY_FOR_SALE"}}]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	info, err := c.LatestVersion(context.Background(), "app123")
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if info.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", info.Version)
	}
	if info.State != "READY_FOR_SALE" {
		t.Errorf("State = %q, want READY_FOR_SALE", info.State)
	}
}

func TestLatestVersionNoVersionsYet(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/app123/appStoreVersions":
			w.Write([]byte(`{"data":[]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	info, err := c.LatestVersion(context.Background(), "app123")
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if info != (port.AppStoreVersionInfo{}) {
		t.Errorf("info = %+v, want zero value", info)
	}
}

// TestNonSuccessResponseSurfacesStatusAndBody verifies a non-2xx ASC
// response surfaces as an error carrying both the HTTP status and a snippet
// of the response body.
func TestNonSuccessResponseSurfacesStatusAndBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":[{"detail":"not authorized"}]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	_, _, err := c.AppByBundleID(context.Background(), "com.example.app")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want it to mention status 403", err)
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("error = %v, want it to include a response body snippet", err)
	}
}

// TestLatestVersionPicksNewestFromUnorderedPage is the regression guard for
// the `?limit=1` bug: /v1/apps/{id}/appStoreVersions accepts no `sort`
// parameter and documents no ordering, so taking the first row of a
// one-item page can return an ancient version. When that happens the store
// monitor never sees READY_FOR_SALE and the app is stranded at test_ready
// forever, which is the only thing that opens the prod gate. The client must
// fetch the page and pick the highest versionString itself, exactly like
// fastlane's spaceship does.
func TestLatestVersionPicksNewestFromUnorderedPage(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/app123/appStoreVersions":
			if got := r.URL.Query().Get("limit"); got != "" {
				t.Errorf("limit = %q, want it not to be sent at all", got)
			}
			// Deliberately oldest-first, with a double-digit minor so a
			// lexicographic comparison would also pick the wrong row.
			w.Write([]byte(`{"data":[
				{"type":"appStoreVersions","id":"v1","attributes":{"versionString":"1.9.0","appStoreState":"REJECTED"}},
				{"type":"appStoreVersions","id":"v2","attributes":{"versionString":"1.10.0","appStoreState":"READY_FOR_SALE"}},
				{"type":"appStoreVersions","id":"v3","attributes":{"versionString":"1.2.0","appStoreState":"REJECTED"}}
			]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	info, err := c.LatestVersion(context.Background(), "app123")
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if info.Version != "1.10.0" {
		t.Errorf("Version = %q, want 1.10.0", info.Version)
	}
	if info.State != "READY_FOR_SALE" {
		t.Errorf("State = %q, want READY_FOR_SALE", info.State)
	}
}

// TestCreateProfileRejectsUnregisteredBundleID covers the guard that stops a
// profile being requested against a bundle ID ASC has never seen: the POST
// must never be attempted, because ASC would otherwise fail with an opaque
// relationship error.
func TestCreateProfileRejectsUnregisteredBundleID(t *testing.T) {
	posted := false
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bundleIds":
			w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/profiles":
			posted = true
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	_, err := c.CreateProfile(context.Background(), "com.example.ghost", "cert1", "name")
	if err == nil {
		t.Fatal("expected an error for an unregistered bundle id, got nil")
	}
	if !strings.Contains(err.Error(), "com.example.ghost") {
		t.Errorf("error = %v, want it to name the bundle id", err)
	}
	if posted {
		t.Error("profile POST was attempted for an unregistered bundle id")
	}
}

// TestNewRejectsNonECDSAP8 covers the type assertion in parseP8PrivateKey: a
// perfectly valid PKCS8 PEM that happens to hold an RSA key is not an Apple
// .p8 and must be rejected at credential-build time, not at signing time.
func TestNewRejectsNonECDSAP8(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKCS8PrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	_, err = New(domain.StoreCredential{
		Provider: domain.StoreCredentialASC,
		Data:     map[string]string{"key_id": "KEY123", "issuer_id": "ISSUER456", "p8": string(pemBytes)},
	})
	if err == nil {
		t.Fatal("expected an error for a non-ECDSA p8, got nil")
	}
	if !strings.Contains(err.Error(), "ECDSA") {
		t.Errorf("error = %v, want it to say the key is not ECDSA", err)
	}
}

// TestCreateCertificateRejectsMalformedContent and its profile twin cover the
// decode branches: ASC returning something that isn't base64 must surface as
// an error rather than a silently empty DER/profile blob being sealed into
// the signing vault.
func TestCreateCertificateRejectsMalformedContent(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/certificates" {
			w.Write([]byte(`{"data":{"id":"cert1","attributes":{"serialNumber":"AB","certificateContent":"!!!not base64!!!","expirationDate":"2027-06-01T00:00:00.000+0000"}}}`))
			return
		}
		t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
	})

	_, err := c.CreateCertificate(context.Background(), []byte("-----BEGIN CERTIFICATE REQUEST-----"))
	if err == nil {
		t.Fatal("expected an error for a malformed certificateContent, got nil")
	}
	if !strings.Contains(err.Error(), "certificateContent") {
		t.Errorf("error = %v, want it to name certificateContent", err)
	}
}

func TestCreateProfileRejectsMalformedContent(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bundleIds":
			w.Write([]byte(`{"data":[{"type":"bundleIds","id":"bundle1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/profiles":
			w.Write([]byte(`{"data":{"id":"prof1","attributes":{"name":"n","profileContent":"!!!not base64!!!","expirationDate":"2027-06-01T00:00:00.000+0000"}}}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	_, err := c.CreateProfile(context.Background(), "com.example.app", "cert1", "name")
	if err == nil {
		t.Fatal("expected an error for a malformed profileContent, got nil")
	}
	if !strings.Contains(err.Error(), "profileContent") {
		t.Errorf("error = %v, want it to name profileContent", err)
	}
}

// TestBearerTokenIsCachedAcrossRequests covers the cache-hit branch of
// bearerToken: a second call inside the token's lifetime must reuse the
// minted JWT rather than signing a fresh one (identical iat/exp claims would
// otherwise still differ by signature nonce).
func TestBearerTokenIsCachedAcrossRequests(t *testing.T) {
	var seen []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Write([]byte(`{"data":[]}`))
	})

	if err := c.ValidateAuth(context.Background()); err != nil {
		t.Fatalf("ValidateAuth: %v", err)
	}
	if err := c.ValidateAuth(context.Background()); err != nil {
		t.Fatalf("ValidateAuth (second): %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("got %d requests, want 2", len(seen))
	}
	if seen[0] != seen[1] {
		t.Error("the second request minted a fresh token instead of reusing the cached one")
	}
	if seen[0] == "" {
		t.Error("no Authorization header was sent")
	}
}

// Submitting has to create the version first when one is not already sitting
// in PREPARE_FOR_SUBMISSION — ASC has no "submit the app", only "submit a
// version".
func TestSubmitForReviewCreatesVersionThenSubmits(t *testing.T) {
	var paths []string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersions":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"VER1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersionSubmissions":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"SUB1"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	if err := client.SubmitForReview(context.Background(), "APP1", "1.4.0"); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	want := []string{"GET /v1/apps/APP1/appStoreVersions", "POST /v1/appStoreVersions", "POST /v1/appStoreVersionSubmissions"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("calls = %v, want %v", paths, want)
	}
}

// An existing editable version is reused rather than duplicated — ASC rejects
// a second version with the same string.
func TestSubmitForReviewReusesEditableVersion(t *testing.T) {
	var created bool
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"id":"VER9","attributes":{"versionString":"1.4.0","appStoreState":"PREPARE_FOR_SUBMISSION"}}]}`))
		case r.URL.Path == "/v1/appStoreVersions":
			created = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"NEW"}}`))
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"SUB1"}}`))
		}
	})
	if err := client.SubmitForReview(context.Background(), "APP1", "1.4.0"); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if created {
		t.Fatal("created a duplicate version instead of reusing the editable one")
	}
}

// Release only makes sense for a version Apple has approved and is holding.
func TestReleaseVersionRefusesWhenNothingIsPendingRelease(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"VER1","attributes":{"versionString":"1.4.0","appStoreState":"IN_REVIEW"}}]}`))
	})
	err := client.ReleaseVersion(context.Background(), "APP1")
	if err == nil {
		t.Fatal("released a version that is still in review")
	}
}

// TestTransportErrorCarriesMethodAndPath: a dial failure used to surface as a
// bare *url.Error with no indication of which ASC call died, which is
// useless in a monitor sweep log that touches half a dozen endpoints.
func TestTransportErrorCarriesMethodAndPath(t *testing.T) {
	cred, _ := testCredential(t)
	c, err := New(cred)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close() // nothing is listening on that port any more
	c.SetBaseURL(addr)

	_, _, err = c.AppByBundleID(context.Background(), "com.example.app")
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	if !strings.Contains(err.Error(), http.MethodGet) {
		t.Errorf("error = %v, want it to name the HTTP method", err)
	}
	if !strings.Contains(err.Error(), "/v1/apps") {
		t.Errorf("error = %v, want it to name the request path", err)
	}
}

// --- appStoreVersions paging -------------------------------------------
//
// ASC serves every collection in fixed-size pages (50 by default) and this
// system auto-releases on each merged PR, so the appStoreVersions collection
// only grows and the row a caller wants ends up past page 1 permanently. Every
// fixture below is deliberately multi-page: a single-page one cannot tell a
// paging client from the non-paging one these tests exist to prevent.

// versionsPager serves /v1/apps/APP1/appStoreVersions as a sequence of pages
// selected by the `cursor` query parameter, and records the cursors it was
// asked for.
//
// Each page's links.next points at an absolute URL on a host that is NOT the
// test server, mirroring ASC (which always returns absolute links) and pinning
// the rule that the client keeps its configured base URL: if it ever followed
// the link's host instead, these requests would never arrive here — and in
// production would carry a live ASC bearer token to whatever host the link
// named.
type versionsPager struct {
	pages   []string // JSON array bodies, one per page, in cursor order
	cursors []string // cursors the server was asked for, "" for the first page
}

func (p *versionsPager) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/APP1/appStoreVersions" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		cursor := r.URL.Query().Get("cursor")
		p.cursors = append(p.cursors, cursor)
		idx := 0
		if cursor != "" {
			n, err := strconv.Atoi(cursor)
			if err != nil {
				t.Errorf("cursor = %q, want an index the fixture handed out", cursor)
				return
			}
			idx = n
		}
		if idx >= len(p.pages) {
			t.Errorf("cursor %q is past the last fixture page", cursor)
			return
		}
		links := "{}"
		if idx+1 < len(p.pages) {
			links = fmt.Sprintf(
				`{"next":"https://appstore.invalid/v1/apps/APP1/appStoreVersions?cursor=%d"}`, idx+1)
		}
		_, _ = fmt.Fprintf(w, `{"data":%s,"links":%s}`, p.pages[idx], links)
	}
}

// postedVersionID pulls the appStoreVersion id out of a relationship-carrying
// POST body, which is how both the release request and the submission name the
// version they act on.
func postedVersionID(t *testing.T, r *http.Request) string {
	t.Helper()
	var body struct {
		Data struct {
			Relationships struct {
				AppStoreVersion struct {
					Data struct {
						ID string `json:"id"`
					} `json:"data"`
				} `json:"appStoreVersion"`
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decoding %s body: %v", r.URL.Path, err)
	}
	return body.Data.Relationships.AppStoreVersion.Data.ID
}

// The newest version sits on page 2. Reading page 1 alone reports an older
// version, and since LatestVersion's READY_FOR_SALE signal is the only thing
// that flips a mobile app to `live`, that strands the app short of the
// production deploy gate forever.
func TestLatestVersionFollowsPagingCursor(t *testing.T) {
	pager := &versionsPager{pages: []string{
		`[{"id":"v1","attributes":{"versionString":"1.2.0","appStoreState":"REJECTED"}},
		  {"id":"v2","attributes":{"versionString":"1.9.0","appStoreState":"REJECTED"}}]`,
		`[{"id":"v3","attributes":{"versionString":"1.10.0","appStoreState":"READY_FOR_SALE"}}]`,
	}}
	c, _ := newTestClient(t, pager.handler(t))

	info, err := c.LatestVersion(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if info.Version != "1.10.0" {
		t.Errorf("Version = %q, want 1.10.0 (the row on page 2)", info.Version)
	}
	if info.State != "READY_FOR_SALE" {
		t.Errorf("State = %q, want READY_FOR_SALE", info.State)
	}
	if !reflect.DeepEqual(pager.cursors, []string{"", "1"}) {
		t.Errorf("cursors requested = %v, want [\"\" \"1\"]", pager.cursors)
	}
}

// The version Apple is holding in PENDING_DEVELOPER_RELEASE is on page 2.
// Without paging, ReleaseVersion reports "no version is pending developer
// release" and every subsequent automated release is blocked.
func TestReleaseVersionFindsPendingReleaseBeyondFirstPage(t *testing.T) {
	pager := &versionsPager{pages: []string{
		`[{"id":"VER1","attributes":{"versionString":"1.1.0","appStoreState":"READY_FOR_SALE"}}]`,
		`[{"id":"VER7","attributes":{"versionString":"1.2.0","appStoreState":"PENDING_DEVELOPER_RELEASE"}}]`,
	}}
	released := ""
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersionReleaseRequests" {
			released = postedVersionID(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"REL1"}}`))
			return
		}
		pager.handler(t)(w, r)
	})

	if err := c.ReleaseVersion(context.Background(), "APP1"); err != nil {
		t.Fatalf("ReleaseVersion: %v", err)
	}
	if released != "VER7" {
		t.Errorf("released version = %q, want VER7 (the row on page 2)", released)
	}
}

// The editable version carrying this versionString is on page 2. Without
// paging, SubmitForReview misses it and POSTs a second appStoreVersions
// resource with a duplicate versionString, which ASC rejects.
func TestSubmitForReviewReusesEditableVersionBeyondFirstPage(t *testing.T) {
	pager := &versionsPager{pages: []string{
		`[{"id":"VER1","attributes":{"versionString":"1.3.0","appStoreState":"READY_FOR_SALE"}}]`,
		`[{"id":"VER9","attributes":{"versionString":"1.4.0","appStoreState":"PREPARE_FOR_SUBMISSION"}}]`,
	}}
	createdVersion := false
	submitted := ""
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersions":
			createdVersion = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"NEW"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersionSubmissions":
			submitted = postedVersionID(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"SUB1"}}`))
		default:
			pager.handler(t)(w, r)
		}
	})

	if err := c.SubmitForReview(context.Background(), "APP1", "1.4.0"); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if createdVersion {
		t.Error("created a duplicate version instead of reusing the editable one on page 2")
	}
	if submitted != "VER9" {
		t.Errorf("submitted version = %q, want VER9", submitted)
	}
}

// A cursor that never terminates must not hang the caller — a store monitor
// sweep runs on a schedule and would pile up behind it. The walk stops at the
// bound and fails loudly rather than returning the pages it did get: a
// silently truncated list is the exact failure paging was added to remove.
func TestPagingStopsAtPageBoundInsteadOfLooping(t *testing.T) {
	requests := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"data":[{"id":"VER1","attributes":{"versionString":"1.0.0","appStoreState":"READY_FOR_SALE"}}],` +
			`"links":{"next":"https://appstore.invalid/v1/apps/APP1/appStoreVersions?cursor=forever"}}`))
	})

	_, err := c.LatestVersion(context.Background(), "APP1")
	if err == nil {
		t.Fatal("expected an error after the page bound, got nil")
	}
	if !strings.Contains(err.Error(), "paging") {
		t.Errorf("error = %v, want it to explain that paging was cut off", err)
	}
	if requests != ascMaxCollectionPages {
		t.Errorf("requests = %d, want exactly %d (the page bound)", requests, ascMaxCollectionPages)
	}
}

// A malformed links.next is a hard error, not a quietly truncated list, for
// the same reason as the page bound above.
func TestPagingRejectsUnparseableNextLink(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"links":{"next":"://not a url"}}`))
	})

	if _, err := c.LatestVersion(context.Background(), "APP1"); err == nil {
		t.Fatal("expected an error for an unparseable next link, got nil")
	}
}
