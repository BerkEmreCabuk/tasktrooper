package postgres

import (
	"errors"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

// The store used to derive its own cipher lazily, on first use. Boot unsets
// MCP_SECRETS_KEY right after deriving the cipher (runtime.scrubProcessSecrets),
// so "first use" is always after the key is gone: every credential path failed
// with "MCP_SECRETS_KEY or SERVER_API_KEY required", which surfaced as an
// unreadable GitHub connection and a silently disabled deploy console rather
// than as a missing key. These pin the injection that replaced it.
func TestSetCipherWinsOverTheScrubbedEnvironment(t *testing.T) {
	t.Setenv("MCP_SECRETS_KEY", "")
	t.Setenv("SERVER_API_KEY", "")

	want, err := secrets.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}

	var s SettingsStore
	s.SetCipher(want, nil)

	got, gotErr := s.getCipher()
	if gotErr != nil {
		t.Fatalf("getCipher after SetCipher: %v", gotErr)
	}
	if got != want {
		t.Fatal("getCipher returned a different cipher than the one injected at boot")
	}
}

// A degraded boot (no key configured, e.g. a desktop install) has to stay
// degraded rather than silently re-deriving from an environment that may still
// hold a key for some other component.
func TestSetCipherPropagatesTheBootError(t *testing.T) {
	t.Setenv("MCP_SECRETS_KEY", "")

	bootErr := errors.New("no key at boot")

	var s SettingsStore
	s.SetCipher(nil, bootErr)

	got, gotErr := s.getCipher()
	if !errors.Is(gotErr, bootErr) {
		t.Fatalf("getCipher error = %v, want %v", gotErr, bootErr)
	}
	if got != nil {
		t.Fatal("getCipher returned a cipher despite a failed boot derivation")
	}
}

// Without SetCipher the lazy path must still work: tests and embedders that
// never scrub rely on it.
func TestGetCipherStillFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv("MCP_SECRETS_KEY", "not-base64-but-usable")

	var s SettingsStore
	got, err := s.getCipher()
	if err != nil {
		t.Fatalf("lazy getCipher: %v", err)
	}
	if got == nil {
		t.Fatal("lazy getCipher returned no cipher")
	}
}
