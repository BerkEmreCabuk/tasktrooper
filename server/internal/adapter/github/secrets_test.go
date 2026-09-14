package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

// TestPutRepoSecretSealsWithRepoPublicKey verifies PutRepoSecret fetches the
// repo's Actions public key, seals the plaintext with NaCl anonymous box
// sealing, and PUTs the base64-encoded sealed value + key_id. The seal is
// checked by actually decrypting it with the matching private key, not by
// mocking the crypto.
//
// apiBase is a package-level var (see api.go) specifically so this test can
// redirect the adapter's HTTP calls at an httptest server; there is no
// pre-existing base-URL override convention in this package to mirror (the
// only prior test, TestParseOwnerRepo in api_test.go, exercises a pure
// string function and never touches HTTP).
func TestPutRepoSecretSealsWithRepoPublicKey(t *testing.T) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("box.GenerateKey: %v", err)
	}

	var captured struct {
		EncryptedValue string `json:"encrypted_value"`
		KeyID          string `json:"key_id"`
	}
	putCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/secrets/public-key"):
			json.NewEncoder(w).Encode(map[string]string{
				"key_id": "k1", "key": base64.StdEncoding.EncodeToString(pub[:]),
			})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/actions/secrets/MY_SECRET"):
			putCalled = true
			json.NewDecoder(r.Body).Decode(&captured)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	original := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = original })

	err = PutRepoSecret(context.Background(), "tok", "o", "r", "MY_SECRET", "hunter2")
	if err != nil {
		t.Fatal(err)
	}

	if !putCalled {
		t.Fatal("expected PUT /actions/secrets/MY_SECRET to be called")
	}
	if captured.KeyID != "k1" {
		t.Fatalf("key_id = %q, want %q", captured.KeyID, "k1")
	}

	sealed, err := base64.StdEncoding.DecodeString(captured.EncryptedValue)
	if err != nil {
		t.Fatalf("decoding encrypted_value: %v", err)
	}
	plain, ok := box.OpenAnonymous(nil, sealed, pub, priv)
	if !ok || string(plain) != "hunter2" {
		t.Fatalf("sealed value did not round-trip, got %q", plain)
	}
}

// TestPutRepoSecretPropagatesPublicKeyError verifies a non-2xx from the
// public-key fetch surfaces as an error and never reaches the PUT step.
func TestPutRepoSecretPropagatesPublicKeyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/secrets/public-key"):
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "nope"})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	original := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = original })

	err := PutRepoSecret(context.Background(), "tok", "o", "r", "MY_SECRET", "hunter2")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*apiError)
	if !ok {
		t.Fatalf("expected *apiError, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", apiErr.Status, http.StatusForbidden)
	}
}

// TestPutRepoSecretPropagatesPutError verifies a non-2xx from the PUT step
// surfaces as an error.
func TestPutRepoSecretPropagatesPutError(t *testing.T) {
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("box.GenerateKey: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/secrets/public-key"):
			json.NewEncoder(w).Encode(map[string]string{
				"key_id": "k1", "key": base64.StdEncoding.EncodeToString(pub[:]),
			})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/actions/secrets/MY_SECRET"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"message": "bad request"})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	original := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = original })

	err = PutRepoSecret(context.Background(), "tok", "o", "r", "MY_SECRET", "hunter2")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*apiError)
	if !ok {
		t.Fatalf("expected *apiError, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", apiErr.Status, http.StatusUnprocessableEntity)
	}
}

// TestPutRepoSecretEscapesSecretName mirrors the escaping DispatchWorkflow
// applies to its own path segment in actions.go: a name carrying a character
// that means something in a URL path must be percent-encoded, not spliced in
// raw where it could re-point the request at a different endpoint.
func TestPutRepoSecretEscapesSecretName(t *testing.T) {
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("box.GenerateKey: %v", err)
	}

	var putPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/secrets/public-key") {
			json.NewEncoder(w).Encode(map[string]string{
				"key_id": "k1", "key": base64.StdEncoding.EncodeToString(pub[:]),
			})
			return
		}
		// r.URL.EscapedPath preserves what actually went on the wire;
		// r.URL.Path is already decoded and would hide the bug.
		putPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	original := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = original })

	if err := PutRepoSecret(context.Background(), "tok", "o", "r", "A/../B", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(putPath, "A/../B") {
		t.Fatalf("PUT path = %q, want the secret name percent-escaped", putPath)
	}
	if !strings.HasSuffix(putPath, "/actions/secrets/A%2F..%2FB") {
		t.Fatalf("PUT path = %q, want it to end with the escaped secret name", putPath)
	}
}

// TestPutRepoSecretRejectsUndecodablePublicKey covers the defensive base64
// branch: GitHub returning a key that isn't base64 must fail loudly instead
// of sealing against zero bytes.
func TestPutRepoSecretRejectsUndecodablePublicKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/secrets/public-key") {
			json.NewEncoder(w).Encode(map[string]string{"key_id": "k1", "key": "!!!not base64!!!"})
			return
		}
		t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	original := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = original })

	err := PutRepoSecret(context.Background(), "tok", "o", "r", "MY_SECRET", "hunter2")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decoding repo public key") {
		t.Fatalf("error = %v, want it to name the public key decode failure", err)
	}
}

// TestPutRepoSecretRejectsWrongLengthPublicKey covers the 32-byte length
// guard: NaCl box keys are fixed width, and a short key silently zero-padded
// into the array would produce a value the repo can never decrypt.
func TestPutRepoSecretRejectsWrongLengthPublicKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/secrets/public-key") {
			json.NewEncoder(w).Encode(map[string]string{
				"key_id": "k1", "key": base64.StdEncoding.EncodeToString([]byte("too short")),
			})
			return
		}
		t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	original := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = original })

	err := PutRepoSecret(context.Background(), "tok", "o", "r", "MY_SECRET", "hunter2")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "want 32") {
		t.Fatalf("error = %v, want it to report the expected key length", err)
	}
}
