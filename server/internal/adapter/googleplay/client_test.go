package googleplay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// testCredential generates a fresh 2048-bit RSA key pair, PEM-encodes the
// private key as PKCS8 (mirroring what Google hands out inside a service
// account's JSON key file), and returns a domain.StoreCredential built from
// it plus the matching public key so tests can independently verify
// signatures produced by the client.
func testCredential(t *testing.T) (domain.StoreCredential, *rsa.PublicKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("x509.MarshalPKCS8PrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	sa := struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
	}{
		ClientEmail: "sa@test-project.iam.gserviceaccount.com",
		PrivateKey:  string(pemBytes),
	}
	saJSON, err := json.Marshal(sa)
	if err != nil {
		t.Fatalf("json.Marshal(sa): %v", err)
	}

	cred := domain.StoreCredential{
		Provider: domain.StoreCredentialGooglePlay,
		Data: map[string]string{
			"service_account_json": string(saJSON),
		},
	}
	return cred, &priv.PublicKey
}

// tokenHandlerOK is a fake OAuth token endpoint that accepts any JWT-bearer
// assertion and returns a fixed access token. Used by tests exercising the
// Play Developer API surface (AppExists, TrackInfo) where the token
// exchange itself isn't under test — that is covered directly by
// TestTokenExchangeCarriesValidJWTBearerAssertion.
func tokenHandlerOK(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			w.Write([]byte(`{"access_token":"fake-access-token","expires_in":3600}`))
		default:
			t.Errorf("unexpected token call %s %s", r.Method, r.URL.Path)
		}
	}
}

// newTestClient builds a Client wired to a fresh httptest token server
// (tokenHandler) and a fresh httptest Play Developer API server
// (apiHandler), and returns the public key matching the credential's
// private key so callers can verify signed JWTs.
func newTestClient(t *testing.T, tokenHandler, apiHandler http.HandlerFunc) (*Client, *rsa.PublicKey) {
	t.Helper()
	cred, pub := testCredential(t)
	c, err := New(cred)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tokenSrv := httptest.NewServer(tokenHandler)
	t.Cleanup(tokenSrv.Close)
	apiSrv := httptest.NewServer(apiHandler)
	t.Cleanup(apiSrv.Close)
	c.SetTokenURL(tokenSrv.URL)
	c.SetBaseURL(apiSrv.URL)
	return c, pub
}

func TestNewRejectsMissingServiceAccountJSON(t *testing.T) {
	_, err := New(domain.StoreCredential{Provider: domain.StoreCredentialGooglePlay, Data: map[string]string{}})
	if err == nil {
		t.Fatal("expected error for missing service_account_json, got nil")
	}
}

func TestNewRejectsInvalidServiceAccountJSON(t *testing.T) {
	_, err := New(domain.StoreCredential{
		Provider: domain.StoreCredentialGooglePlay,
		Data:     map[string]string{"service_account_json": "not json"},
	})
	if err == nil {
		t.Fatal("expected error for invalid service_account_json, got nil")
	}
}

func TestNewRejectsInvalidPrivateKey(t *testing.T) {
	saJSON := `{"client_email":"sa@test-project.iam.gserviceaccount.com","private_key":"not a real pem"}`
	_, err := New(domain.StoreCredential{
		Provider: domain.StoreCredentialGooglePlay,
		Data:     map[string]string{"service_account_json": saJSON},
	})
	if err == nil {
		t.Fatal("expected error for invalid private_key, got nil")
	}
}

// TestTokenExchangeCarriesValidJWTBearerAssertion verifies ValidateAuth
// performs the OAuth 2.0 JWT-bearer grant (RFC 7523) exactly: a
// grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer form POST whose
// assertion is a JWT carrying the service account's client_email as iss,
// the androidpublisher scope ALONE, and the token URL as aud — and,
// critically, that the RS256 signature actually validates against the
// credential's public key with rsa.VerifyPKCS1v15. A decode-only assertion
// would not prove the signing itself is correct.
func TestTokenExchangeCarriesValidJWTBearerAssertion(t *testing.T) {
	cred, pub := testCredential(t)

	var gotForm url.Values
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			gotForm = r.Form
			w.Write([]byte(`{"access_token":"fake-access-token","expires_in":3600}`))
		default:
			t.Errorf("unexpected token call %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(tokenSrv.Close)

	c, err := New(cred)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetTokenURL(tokenSrv.URL)

	if err := c.ValidateAuth(context.Background()); err != nil {
		t.Fatalf("ValidateAuth: %v", err)
	}

	if got := gotForm.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Errorf("grant_type = %q, want the JWT-bearer URN", got)
	}

	assertion := gotForm.Get("assertion")
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("assertion has %d parts, want 3 (header.claims.signature)", len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decoding header: %v", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.Alg != "RS256" {
		t.Errorf("alg = %q, want RS256", header.Alg)
	}
	if header.Typ != "JWT" {
		t.Errorf("typ = %q, want JWT", header.Typ)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding claims: %v", err)
	}
	var claims struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Iat   int64  `json:"iat"`
		Exp   int64  `json:"exp"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "sa@test-project.iam.gserviceaccount.com" {
		t.Errorf("iss = %q, want the service account client_email", claims.Iss)
	}
	if claims.Scope != androidPublisherScope {
		t.Errorf("scope = %q, want %q alone — the optional reporting scope must never ride on the token every deploy needs", claims.Scope, androidPublisherScope)
	}
	if claims.Aud != tokenSrv.URL {
		t.Errorf("aud = %q, want %q (the token URI)", claims.Aud, tokenSrv.URL)
	}
	if claims.Exp-claims.Iat != 3600 {
		t.Errorf("exp-iat = %d, want 3600 (1 hour)", claims.Exp-claims.Iat)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}
	signingInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("RS256 signature does not validate against the credential's public key: %v", err)
	}
}

func TestAppExistsTrue(t *testing.T) {
	var insertCalled, deleteCalled bool
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits":
			insertCalled = true
			w.Write([]byte(`{"id":"edit123"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit123":
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	exists, err := c.AppExists(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("AppExists: %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true")
	}
	if !insertCalled {
		t.Fatal("edits.insert was not called")
	}
	if !deleteCalled {
		t.Fatal("edit was not deleted — probe edit leaked")
	}
}

func TestAppExistsFalseNotFound(t *testing.T) {
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.missing/edits":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"App not found"}}`))
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	exists, err := c.AppExists(context.Background(), "com.example.missing")
	if err != nil {
		t.Fatalf("AppExists: %v", err)
	}
	if exists {
		t.Fatal("exists = true, want false")
	}
}

func TestAppExistsFalseForbidden(t *testing.T) {
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.forbidden/edits":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"code":403,"message":"no access"}}`))
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	exists, err := c.AppExists(context.Background(), "com.example.forbidden")
	if err != nil {
		t.Fatalf("AppExists: %v", err)
	}
	if exists {
		t.Fatal("exists = true, want false")
	}
}

// TestAppExistsUnauthorizedIsError verifies a 401 from edits.insert (broken
// auth) surfaces as an error rather than being folded into "app doesn't
// exist" — those are different failure modes and callers must be able to
// tell them apart.
func TestAppExistsUnauthorizedIsError(t *testing.T) {
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"code":401,"message":"invalid credentials"}}`))
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	_, err := c.AppExists(context.Background(), "com.example.app")
	if err == nil {
		t.Fatal("expected error for a 401, got nil (app-absent and auth-broken must not be conflated)")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want it to mention status 401", err)
	}
}

func TestTrackInfoWithRelease(t *testing.T) {
	var deleteCalled bool
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits":
			w.Write([]byte(`{"id":"edit456"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit456/tracks/production":
			w.Write([]byte(`{"track":"production","releases":[{"name":"1.2.3","status":"completed","userFraction":1.0}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit456":
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	info, err := c.TrackInfo(context.Background(), "com.example.app", "production")
	if err != nil {
		t.Fatalf("TrackInfo: %v", err)
	}
	if !info.HasRelease {
		t.Fatal("HasRelease = false, want true")
	}
	if info.VersionName != "1.2.3" {
		t.Errorf("VersionName = %q, want 1.2.3", info.VersionName)
	}
	if info.Status != "completed" {
		t.Errorf("Status = %q, want completed", info.Status)
	}
	if info.UserFraction != 1.0 {
		t.Errorf("UserFraction = %v, want 1.0", info.UserFraction)
	}
	if !deleteCalled {
		t.Fatal("edit was not deleted — probe edit leaked")
	}
}

func TestTrackInfoNoRelease(t *testing.T) {
	var deleteCalled bool
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits":
			w.Write([]byte(`{"id":"edit789"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit789/tracks/internal":
			w.Write([]byte(`{"track":"internal","releases":[]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit789":
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	info, err := c.TrackInfo(context.Background(), "com.example.app", "internal")
	if err != nil {
		t.Fatalf("TrackInfo: %v", err)
	}
	if info.HasRelease {
		t.Fatal("HasRelease = true, want false")
	}
	if !deleteCalled {
		t.Fatal("edit was not deleted — probe edit leaked")
	}
}

func TestTrackInfoAppNotFoundIsError(t *testing.T) {
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.missing/edits":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":404,"message":"App not found"}}`))
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	_, err := c.TrackInfo(context.Background(), "com.example.missing", "production")
	if err == nil {
		t.Fatal("expected error for a missing app, got nil")
	}
}

// TestTrackInfoDeletesEditEvenOnAPIError verifies the edit opened by
// TrackInfo is discarded even when the subsequent tracks.get call fails —
// the "never leak an edit" contract must hold on error paths too, not just
// the success path.
func TestTrackInfoDeletesEditEvenOnAPIError(t *testing.T) {
	var deleteCalled bool
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits":
			w.Write([]byte(`{"id":"edit999"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit999/tracks/production":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"message":"boom"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit999":
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	_, err := c.TrackInfo(context.Background(), "com.example.app", "production")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention status 500", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want it to include a response body snippet", err)
	}
	if !deleteCalled {
		t.Fatal("edit was not deleted on the API error path — probe edit leaked")
	}
}

// TestTrackInfoWithoutUserFraction covers the shape Play actually returns for
// a fully-rolled-out or halted release: `userFraction` is omitted entirely,
// not sent as 0. The zero value is the right answer, but it must come with
// HasRelease/Status still populated — the store monitor keys its
// halted-rollout incident off Status, and dropping the release because one
// optional field is missing would silence that alarm.
func TestTrackInfoWithoutUserFraction(t *testing.T) {
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits":
			w.Write([]byte(`{"id":"edit999"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit999/tracks/production":
			w.Write([]byte(`{"track":"production","releases":[{"name":"2.0.0","status":"halted"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit999":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	info, err := c.TrackInfo(context.Background(), "com.example.app", "production")
	if err != nil {
		t.Fatalf("TrackInfo: %v", err)
	}
	if !info.HasRelease {
		t.Fatal("HasRelease = false, want true")
	}
	if info.VersionName != "2.0.0" {
		t.Errorf("VersionName = %q, want 2.0.0", info.VersionName)
	}
	if info.Status != "halted" {
		t.Errorf("Status = %q, want halted", info.Status)
	}
	if info.UserFraction != 0 {
		t.Errorf("UserFraction = %v, want 0 for an omitted field", info.UserFraction)
	}
}

// TestDeleteEditFailureDoesNotChangeTheResult pins the best-effort contract
// deleteEdit's warn log accompanies: a failed discard is logged, never
// promoted into the caller's error.
func TestDeleteEditFailureDoesNotChangeTheResult(t *testing.T) {
	apiHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits":
			w.Write([]byte(`{"id":"edit111"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/edit111":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"code":500,"message":"boom"}}`))
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
	c, _ := newTestClient(t, tokenHandlerOK(t), apiHandler)

	exists, err := c.AppExists(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("AppExists: %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true — a failed edit discard must not change the answer")
	}
}

// Every write is a full edit transaction: insert, mutate, COMMIT. A write
// that only opens and discards an edit changes nothing in the Play console.
func TestPromoteTrackCommitsTheEdit(t *testing.T) {
	var calls []string
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/edits"):
			_, _ = w.Write([]byte(`{"id":"EDIT1"}`))
		case strings.HasSuffix(r.URL.Path, "/tracks/internal"):
			_, _ = w.Write([]byte(`{"releases":[{"name":"1.4.0","status":"completed","versionCodes":["41"]}]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
	if err := c.PromoteTrack(context.Background(), "com.x", "internal", "production", 0.1); err != nil {
		t.Fatalf("PromoteTrack: %v", err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "POST /androidpublisher/v3/applications/com.x/edits/EDIT1:commit") {
		t.Fatalf("edit was never committed:\n%s", joined)
	}
	if strings.Contains(joined, "DELETE /androidpublisher/v3/applications/com.x/edits/EDIT1") {
		t.Fatalf("write edit was discarded:\n%s", joined)
	}
}

// A fraction below 1 must publish as inProgress — sending status completed
// with a fraction silently ships to 100% of users.
func TestPromoteTrackPartialRolloutUsesInProgressStatus(t *testing.T) {
	var body map[string]any
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/tracks/production") {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		if strings.HasSuffix(r.URL.Path, "/edits") {
			_, _ = w.Write([]byte(`{"id":"E"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/tracks/internal") {
			_, _ = w.Write([]byte(`{"releases":[{"name":"1.4.0","status":"completed","versionCodes":["41"]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if err := c.PromoteTrack(context.Background(), "com.x", "internal", "production", 0.1); err != nil {
		t.Fatalf("PromoteTrack: %v", err)
	}
	releases, _ := body["releases"].([]any)
	first, _ := releases[0].(map[string]any)
	if first["status"] != "inProgress" || first["userFraction"] != 0.1 {
		t.Fatalf("release payload = %v", first)
	}
}

// A fraction of 1 is a full rollout: status completed and NO userFraction —
// Play rejects a completed release that carries one.
func TestSetRolloutFractionOneCompletesWithoutFraction(t *testing.T) {
	body := captureTrackUpdate(t, "production",
		`{"releases":[{"name":"1.4.0","status":"inProgress","userFraction":0.1,"versionCodes":["41"]}]}`,
		func(c *Client) error {
			return c.SetRolloutFraction(context.Background(), "com.x", "production", 1)
		})
	rel := firstRelease(t, body)
	if rel["status"] != "completed" {
		t.Fatalf("status = %v, want completed", rel["status"])
	}
	if _, present := rel["userFraction"]; present {
		t.Fatalf("completed release carried a userFraction: %v", rel)
	}
}

// Halting must not touch the fraction — resuming has to restore exactly what
// was rolling out before.
func TestHaltRolloutSetsHaltedAndKeepsFraction(t *testing.T) {
	body := captureTrackUpdate(t, "production",
		`{"releases":[{"name":"1.4.0","status":"inProgress","userFraction":0.25,"versionCodes":["41"]}]}`,
		func(c *Client) error {
			return c.HaltRollout(context.Background(), "com.x", "production")
		})
	rel := firstRelease(t, body)
	if rel["status"] != "halted" || rel["userFraction"] != 0.25 {
		t.Fatalf("release = %v, want halted at 0.25", rel)
	}
}

// Nothing to promote is a clear error, not a silent no-op.
func TestPromoteTrackFailsWhenSourceTrackIsEmpty(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/edits") {
			_, _ = w.Write([]byte(`{"id":"E"}`))
			return
		}
		_, _ = w.Write([]byte(`{"releases":[]}`))
	})
	err := c.PromoteTrack(context.Background(), "com.x", "internal", "production", 1)
	if err == nil || !strings.Contains(err.Error(), "no release to promote") {
		t.Fatalf("err = %v, want a 'no release to promote' failure", err)
	}
}

// captureTrackUpdate runs fn against a Play stub whose source track returns
// sourceJSON, and returns the body of the PUT to /tracks/<track>. firstRelease
// pulls releases[0] out of it. Both are test helpers in this file.
func captureTrackUpdate(t *testing.T, track, sourceJSON string, fn func(*Client) error) map[string]any {
	t.Helper()
	var body map[string]any
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/edits"):
			_, _ = w.Write([]byte(`{"id":"E"}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/tracks/"+track):
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/tracks/"+track):
			_, _ = w.Write([]byte(sourceJSON))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
	if err := fn(c); err != nil {
		t.Fatalf("fn: %v", err)
	}
	return body
}

func firstRelease(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	releases, _ := body["releases"].([]any)
	if len(releases) == 0 {
		t.Fatalf("body has no releases: %v", body)
	}
	rel, _ := releases[0].(map[string]any)
	return rel
}

// newReportingClient wires a Client to a fake Play Developer Reporting API.
// Its Play Developer API stub fails the test on any call: the app listing
// must go to the reporting host, which is a different service entirely.
func newReportingClient(t *testing.T, reportingHandler http.HandlerFunc) *Client {
	t.Helper()
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("ListApps hit the Play Developer API: %s %s", r.Method, r.URL.Path)
	})
	srv := httptest.NewServer(reportingHandler)
	t.Cleanup(srv.Close)
	c.SetReportingBaseURL(srv.URL)
	return c
}

func TestListAppsReturnsPackageNamesWithoutStoreAppIDs(t *testing.T) {
	var gotMethod, gotPath string
	c := newReportingClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"apps":[
			{"name":"apps/com.example.one","packageName":"com.example.one","displayName":"One"},
			{"name":"apps/com.example.two","packageName":"com.example.two","displayName":"Two"}
		]}`))
	})

	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v1beta1/apps:search" {
		t.Errorf("request = %s %s, want GET /v1beta1/apps:search", gotMethod, gotPath)
	}
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2: %v", len(apps), apps)
	}
	if apps[0].Identifier != "com.example.one" || apps[0].Name != "One" {
		t.Errorf("apps[0] = %+v", apps[0])
	}
	for _, a := range apps {
		if a.StoreAppID != "" {
			t.Errorf("StoreAppID = %q, want empty — Play keys apps by package name and has no app id", a.StoreAppID)
		}
	}
}

// A 403 means the reporting API is off or the service account was never
// granted it. That is an expected answer, not a failure: callers fall back
// to a hand-typed package name, which they can only do if they can tell it
// apart from a broken credential.
func TestListAppsForbiddenIsUnavailableSentinel(t *testing.T) {
	c := newReportingClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Play Developer Reporting API has not been used"}}`))
	})

	_, err := c.ListApps(context.Background())
	if !errors.Is(err, port.ErrAppListingUnavailable) {
		t.Fatalf("err = %v, want it to wrap port.ErrAppListingUnavailable", err)
	}
}

func TestListAppsNotFoundIsUnavailableSentinel(t *testing.T) {
	c := newReportingClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found"}}`))
	})

	_, err := c.ListApps(context.Background())
	if !errors.Is(err, port.ErrAppListingUnavailable) {
		t.Fatalf("err = %v, want it to wrap port.ErrAppListingUnavailable", err)
	}
}

// 401 is broken auth, a real failure. Folding it into the unavailable
// sentinel would silently downgrade a dead credential to "just type the
// package name yourself".
func TestListAppsUnauthorizedIsRealError(t *testing.T) {
	c := newReportingClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":401,"message":"invalid credentials"}}`))
	})

	_, err := c.ListApps(context.Background())
	if err == nil {
		t.Fatal("expected an error for a 401, got nil")
	}
	if errors.Is(err, port.ErrAppListingUnavailable) {
		t.Fatalf("err = %v, must not be the unavailable sentinel — 401 is broken auth", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want it to mention status 401", err)
	}
}

// An account that really has no apps answers with an empty list and no
// error — the opposite of the unavailable sentinel.
func TestListAppsEmptyListIsNotUnavailable(t *testing.T) {
	c := newReportingClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})

	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("got %d apps, want 0", len(apps))
	}
}

func TestListAppsFollowsPageTokens(t *testing.T) {
	var tokens []string
	c := newReportingClient(t, func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.URL.Query().Get("pageToken"))
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"apps":[{"packageName":"com.example.one","displayName":"One"}],"nextPageToken":"PAGE2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"apps":[{"packageName":"com.example.two","displayName":"Two"}]}`))
	})

	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want both pages: %v", len(apps), apps)
	}
	if len(tokens) != 2 || tokens[1] != "PAGE2" {
		t.Fatalf("page tokens = %v, want the second request to carry PAGE2", tokens)
	}
}

// tracksHandler is a Play stub that answers tracks.list with tracksJSON and
// records every call, so tests can assert exactly one edit was opened.
func tracksHandler(t *testing.T, editID, tracksJSON string, calls *[]string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/edits"):
			_, _ = w.Write([]byte(`{"id":"` + editID + `"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/edits/"+editID+"/tracks"):
			_, _ = w.Write([]byte(tracksJSON))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
}

// Play's edit quota is narrow: every track has to be read inside the ONE
// edit, and that edit discarded rather than committed.
func TestTracksReadsEveryTrackInOneEdit(t *testing.T) {
	var calls []string
	c, _ := newTestClient(t, tokenHandlerOK(t), tracksHandler(t, "E1", `{"tracks":[
		{"track":"internal","releases":[{"name":"1.5.0","status":"completed","versionCodes":["50"]}]},
		{"track":"beta","releases":[{"name":"1.4.0","status":"draft","versionCodes":["40"]}]},
		{"track":"production","releases":[{"name":"1.3.0","status":"inProgress","userFraction":0.25,"versionCodes":["30"]}]}
	]}`, &calls))

	tracks, err := c.Tracks(context.Background(), "com.x")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}

	var inserts, deletes, commits int
	for _, call := range calls {
		switch {
		case strings.HasPrefix(call, "POST") && strings.HasSuffix(call, "/edits"):
			inserts++
		case strings.HasPrefix(call, "DELETE"):
			deletes++
		case strings.HasSuffix(call, ":commit"):
			commits++
		}
	}
	if inserts != 1 {
		t.Fatalf("opened %d edits, want exactly 1:\n%s", inserts, strings.Join(calls, "\n"))
	}
	if deletes != 1 {
		t.Fatalf("discarded %d edits, want exactly 1 — a probe edit leaked:\n%s", deletes, strings.Join(calls, "\n"))
	}
	if commits != 0 {
		t.Fatalf("a read-only probe committed its edit:\n%s", strings.Join(calls, "\n"))
	}

	if tracks.Internal.Version != "1.5.0" || tracks.Internal.Build != "50" || tracks.Internal.Status != domain.TrackStatusLive {
		t.Errorf("internal = %+v", tracks.Internal)
	}
	if tracks.External.Status != domain.TrackStatusDraft || tracks.External.Audience != "Open testing" {
		t.Errorf("external = %+v", tracks.External)
	}
	if tracks.Production.Status != domain.TrackStatusRollingOut || tracks.Production.UserFraction != 0.25 {
		t.Errorf("production = %+v", tracks.Production)
	}
	if !tracks.Internal.HasRelease || !tracks.External.HasRelease || !tracks.Production.HasRelease {
		t.Errorf("tracks = %+v, want a release on every channel", tracks)
	}
}

// alpha (closed testing) and beta (open testing) are one product channel.
// With both populated the newer version code wins, and the channel says
// which track it came from — otherwise the panel shows a build without
// saying who can actually install it.
func TestTracksExternalPrefersNewerOfAlphaAndBeta(t *testing.T) {
	for _, tc := range []struct {
		name         string
		alpha, beta  string
		wantBuild    string
		wantAudience string
	}{
		{
			name:         "alpha is newer",
			alpha:        `{"track":"alpha","releases":[{"name":"2.0.0","status":"completed","versionCodes":["60"]}]}`,
			beta:         `{"track":"beta","releases":[{"name":"1.9.0","status":"completed","versionCodes":["50"]}]}`,
			wantBuild:    "60",
			wantAudience: "Closed testing",
		},
		{
			name:         "beta is newer",
			alpha:        `{"track":"alpha","releases":[{"name":"1.9.0","status":"completed","versionCodes":["50"]}]}`,
			beta:         `{"track":"beta","releases":[{"name":"2.1.0","status":"completed","versionCodes":["70"]}]}`,
			wantBuild:    "70",
			wantAudience: "Open testing",
		},
		{
			name:         "only alpha",
			alpha:        `{"track":"alpha","releases":[{"name":"1.9.0","status":"completed","versionCodes":["50"]}]}`,
			beta:         `{"track":"beta","releases":[]}`,
			wantBuild:    "50",
			wantAudience: "Closed testing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			c, _ := newTestClient(t, tokenHandlerOK(t), tracksHandler(t, "E2", `{"tracks":[`+tc.alpha+`,`+tc.beta+`]}`, &calls))

			tracks, err := c.Tracks(context.Background(), "com.x")
			if err != nil {
				t.Fatalf("Tracks: %v", err)
			}
			if tracks.External.Build != tc.wantBuild {
				t.Errorf("external build = %q, want %q", tracks.External.Build, tc.wantBuild)
			}
			if tracks.External.Audience != tc.wantAudience {
				t.Errorf("external audience = %q, want %q", tracks.External.Audience, tc.wantAudience)
			}
		})
	}
}

func TestTracksEmptyChannelsReportNone(t *testing.T) {
	var calls []string
	c, _ := newTestClient(t, tokenHandlerOK(t), tracksHandler(t, "E3", `{"tracks":[{"track":"internal","releases":[]}]}`, &calls))

	tracks, err := c.Tracks(context.Background(), "com.x")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	for name, channel := range map[string]domain.TrackRelease{
		"internal":   tracks.Internal,
		"external":   tracks.External,
		"production": tracks.Production,
	} {
		if channel.HasRelease {
			t.Errorf("%s HasRelease = true, want false", name)
		}
		if channel.Status != domain.TrackStatusNone {
			t.Errorf("%s status = %q, want %q", name, channel.Status, domain.TrackStatusNone)
		}
	}
}

// A track holds every active release at once. The channel is about what is
// going out now, so the staged rollout wins over the completed release it is
// replacing — reporting the older one would hide an in-flight rollout.
func TestTracksPicksTheHighestVersionCodeRelease(t *testing.T) {
	var calls []string
	c, _ := newTestClient(t, tokenHandlerOK(t), tracksHandler(t, "E4", `{"tracks":[
		{"track":"production","releases":[
			{"name":"1.0.0","status":"completed","versionCodes":["10"]},
			{"name":"1.1.0","status":"inProgress","userFraction":0.1,"versionCodes":["11"]}
		]}
	]}`, &calls))

	tracks, err := c.Tracks(context.Background(), "com.x")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if tracks.Production.Build != "11" || tracks.Production.Status != domain.TrackStatusRollingOut {
		t.Fatalf("production = %+v, want the in-flight rollout at build 11", tracks.Production)
	}
}

// Form-factor tracks carry a prefix (`wear:production`). They are a
// different app surface and must not stand in for the phone track.
func TestTracksIgnoresFormFactorTracks(t *testing.T) {
	var calls []string
	c, _ := newTestClient(t, tokenHandlerOK(t), tracksHandler(t, "E5", `{"tracks":[
		{"track":"wear:production","releases":[{"name":"9.9.9","status":"completed","versionCodes":["99"]}]}
	]}`, &calls))

	tracks, err := c.Tracks(context.Background(), "com.x")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if tracks.Production.HasRelease {
		t.Fatalf("production = %+v, want empty — wear:production is not the phone track", tracks.Production)
	}
}

// The "never leak an edit" contract holds on the error path too.
func TestTracksDeletesEditEvenOnAPIError(t *testing.T) {
	var deleteCalled bool
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/edits"):
			_, _ = w.Write([]byte(`{"id":"E6"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tracks"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/edits/E6"):
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	})

	if _, err := c.Tracks(context.Background(), "com.x"); err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !deleteCalled {
		t.Fatal("edit was not deleted on the API error path — probe edit leaked")
	}
}

// An app the credential cannot see is an error, never an empty StoreTracks:
// three empty channels would read as a real app that has shipped nothing.
func TestTracksAppNotFoundIsError(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"App not found"}}`))
	})

	_, err := c.Tracks(context.Background(), "com.example.missing")
	if err == nil {
		t.Fatal("expected an error for a missing app, got nil")
	}
	if !errors.Is(err, errAppNotFound) {
		t.Errorf("error = %v, want it to wrap errAppNotFound", err)
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "com.example.missing") {
		t.Errorf("error = %v, want it to name the missing app", err)
	}
}

// assertionScope pulls the scope claim out of a JWT-bearer assertion so a fake
// token endpoint can answer per scope the way Google's does.
func assertionScope(t *testing.T, assertion string) string {
	t.Helper()
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Errorf("assertion has %d parts, want 3", len(parts))
		return ""
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Errorf("decoding claims: %v", err)
		return ""
	}
	var claims struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Errorf("unmarshal claims: %v", err)
		return ""
	}
	return claims.Scope
}

// scopedTokenHandler is a fake OAuth token endpoint that mints a distinct
// access token per requested scope, records every scope it was asked for, and
// refuses the ones in reject the way Google refuses a scope it does not
// recognize: invalid_scope, and the whole exchange fails.
func scopedTokenHandler(t *testing.T, reject map[string]bool, seen *[]string) http.HandlerFunc {
	t.Helper()
	var mu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			return
		}
		scope := assertionScope(t, r.Form.Get("assertion"))
		mu.Lock()
		*seen = append(*seen, scope)
		mu.Unlock()
		if reject[scope] {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_scope","error_description":"Invalid OAuth scope"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"tok-` + scope + `","expires_in":3600}`))
	}
}

// TestReportingScopeRejectionLeavesThePublisherPathWorking is the reason the
// two scopes are minted separately. Google refuses an assertion whole rather
// than granting the scopes it recognizes, so a reporting scope it will not
// grant has to cost the optional app listing and nothing else: credential
// validation and the deploy path both run on the publisher token and must
// stay green.
func TestReportingScopeRejectionLeavesThePublisherPathWorking(t *testing.T) {
	var seen []string
	c, _ := newTestClient(t, scopedTokenHandler(t, map[string]bool{playReportingScope: true}, &seen), func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer tok-"+androidPublisherScope; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/edits"):
			_, _ = w.Write([]byte(`{"id":"E9"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tracks"):
			_, _ = w.Write([]byte(`{"tracks":[{"track":"production","releases":[{"name":"1.0.0","status":"completed","versionCodes":["7"]}]}]}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	})
	reportingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("reporting API called without a token: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(reportingSrv.Close)
	c.SetReportingBaseURL(reportingSrv.URL)

	if err := c.ValidateAuth(context.Background()); err != nil {
		t.Fatalf("ValidateAuth: %v — a refused reporting scope must not fail credential validation", err)
	}

	tracks, err := c.Tracks(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("Tracks: %v — a refused reporting scope must not break the deploy path", err)
	}
	if tracks.Production.Status != domain.TrackStatusLive {
		t.Errorf("Production.Status = %q, want %q", tracks.Production.Status, domain.TrackStatusLive)
	}

	if _, err := c.ListApps(context.Background()); !errors.Is(err, port.ErrAppListingUnavailable) {
		t.Fatalf("ListApps err = %v, want it to wrap port.ErrAppListingUnavailable — the listing is the only thing a refused reporting scope may cost", err)
	}
}

// The reporting host gets the reporting-scope token and nothing else: the two
// services are separate and each rejects the other's token.
func TestListAppsUsesTheReportingScopeToken(t *testing.T) {
	var seen []string
	c, _ := newTestClient(t, scopedTokenHandler(t, nil, &seen), func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("ListApps hit the Play Developer API: %s %s", r.Method, r.URL.Path)
	})
	var gotAuth string
	reportingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"apps":[{"packageName":"com.example.one","displayName":"One"}]}`))
	}))
	t.Cleanup(reportingSrv.Close)
	c.SetReportingBaseURL(reportingSrv.URL)

	if _, err := c.ListApps(context.Background()); err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if want := "Bearer tok-" + playReportingScope; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	for _, scope := range seen {
		if scope != playReportingScope {
			t.Errorf("token exchange asked for %q; ListApps must mint the reporting scope alone", scope)
		}
	}
}

// A cursor that alternates A→B→A never repeats consecutively, so the
// "same token twice" guard cannot end the walk on its own.
func TestListAppsStopsAtThePageCeiling(t *testing.T) {
	requests := 0
	c := newReportingClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		next := "A"
		if r.URL.Query().Get("pageToken") == "A" {
			next = "B"
		}
		_, _ = w.Write([]byte(`{"apps":[{"packageName":"com.example.one"}],"nextPageToken":"` + next + `"}`))
	})

	_, err := c.ListApps(context.Background())
	if err == nil {
		t.Fatal("expected an error at the page ceiling, got nil")
	}
	if errors.Is(err, port.ErrAppListingUnavailable) {
		t.Fatalf("err = %v, must not be the unavailable sentinel — a runaway cursor is a real failure, not a credential that cannot enumerate", err)
	}
	if requests != listAppsMaxPages {
		t.Errorf("requests = %d, want exactly %d (the page bound)", requests, listAppsMaxPages)
	}
}

// Revoked app access must not be reported as a missing app: the operator would
// go re-check a package name that is already correct.
func TestTracksForbiddenSaysAccessNotMissingApp(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"The caller does not have permission"}}`))
	})

	_, err := c.Tracks(context.Background(), "com.example.app")
	if !errors.Is(err, errAppForbidden) {
		t.Fatalf("err = %v, want it to wrap errAppForbidden", err)
	}
	if errors.Is(err, errAppNotFound) || strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, must not read as a missing app", err)
	}
	if !strings.Contains(err.Error(), "com.example.app") {
		t.Errorf("error = %v, want it to name the app", err)
	}
}

// AppExists still folds 403 into "no". It answers whether this credential can
// work with the app, and one it cannot reach is no more usable than an absent
// one — the distinction only matters where a caller goes on to read or write.
func TestAppExistsStillTreatsForbiddenAsAbsent(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"no access"}}`))
	})

	exists, err := c.AppExists(context.Background(), "com.example.forbidden")
	if err != nil {
		t.Fatalf("AppExists: %v, want the forbidden app reported as absent", err)
	}
	if exists {
		t.Fatal("exists = true, want false")
	}
}

// An unmapped Play status has to reach the client as "unknown", not "":
// TrackRelease.Status is omitempty, so an empty string never leaves the server
// and the panel renders a channel that does hold a release as an empty one.
func TestTracksUnknownStatusSurvivesSerialization(t *testing.T) {
	var calls []string
	c, _ := newTestClient(t, tokenHandlerOK(t), tracksHandler(t, "E7",
		`{"tracks":[{"track":"production","releases":[{"name":"3.1.0","status":"someFuturePlayStatus","versionCodes":["91"]}]}]}`, &calls))

	tracks, err := c.Tracks(context.Background(), "com.x")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if tracks.Production.Status != domain.TrackStatusUnknown {
		t.Fatalf("Production.Status = %q, want %q", tracks.Production.Status, domain.TrackStatusUnknown)
	}
	if !tracks.Production.HasRelease {
		t.Error("HasRelease = false, want true — the release exists, only its status is unmapped")
	}
	body, err := json.Marshal(tracks.Production)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(body), `"status":"unknown"`) {
		t.Errorf("serialized channel = %s, want a status the client can render", body)
	}
}
