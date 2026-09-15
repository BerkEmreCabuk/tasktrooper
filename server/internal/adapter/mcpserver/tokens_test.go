package mcpserver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestMintIssuesADistinctTokenPerRun(t *testing.T) {
	tokens := NewRunTokenRegistry()

	seen := make(map[string]bool)
	for range 64 {
		token, err := tokens.Mint(Run{Ctx: context.Background()})
		require.NoError(t, err)
		assert.False(t, seen[token], "a reused token would let one run act as another")
		assert.GreaterOrEqual(t, len(token), 40, "32 random bytes, base64url-encoded")
		seen[token] = true
	}
	assert.Equal(t, 64, tokens.Live())
}

func TestLookupResolvesTheRunAndRevokeEndsIt(t *testing.T) {
	tokens := NewRunTokenRegistry()
	policy := domain.ToolPolicy{AllowTools: []string{"move_board_task"}}
	ctx := context.WithValue(context.Background(), testKey{}, "run-ctx")

	token, err := tokens.Mint(Run{Ctx: ctx, Policy: policy, TaskKey: "tt-42"})
	require.NoError(t, err)

	run, ok := tokens.Lookup(token)
	require.True(t, ok)
	assert.Equal(t, policy, run.Policy)
	assert.Equal(t, "tt-42", run.TaskKey)
	assert.Equal(t, "run-ctx", run.Ctx.Value(testKey{}))

	tokens.Revoke(token)
	_, ok = tokens.Lookup(token)
	assert.False(t, ok, "a token has to die with the run that minted it")
	assert.Equal(t, 0, tokens.Live())

	// Revoking twice is what a defer on a path that already released does.
	tokens.Revoke(token)
	tokens.Revoke("")
}

func TestLookupRejectsTheEmptyToken(t *testing.T) {
	tokens := NewRunTokenRegistry()
	_, ok := tokens.Lookup("")
	assert.False(t, ok, "an empty Authorization header must not resolve to anything")
}

// Without the run's context every tool call would land on a background context
// with no workspace, no task and no ledger — served, but unattributable.
func TestMintRefusesARunWithoutAContext(t *testing.T) {
	_, err := NewRunTokenRegistry().Mint(Run{TaskKey: "tt-42"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

type testKey struct{}

// One token names one run. Two live runs are two credentials, and presenting
// one must serve the other's policy to nobody.
func TestATokenNamesExactlyOneRun(t *testing.T) {
	tokens := NewRunTokenRegistry()

	first, err := tokens.Mint(Run{Ctx: context.Background(), TaskKey: "tt-1"})
	require.NoError(t, err)
	second, err := tokens.Mint(Run{Ctx: context.Background(), TaskKey: "tt-2"})
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	runA, ok := tokens.Lookup(first)
	require.True(t, ok)
	assert.Equal(t, "tt-1", runA.TaskKey)

	// Run A ends. Its credential stops working; run B's is untouched.
	tokens.Revoke(first)
	_, ok = tokens.Lookup(first)
	assert.False(t, ok, "a finished run's token must not be usable for anything")
	runB, ok := tokens.Lookup(second)
	require.True(t, ok, "one run ending must not revoke another's credential")
	assert.Equal(t, "tt-2", runB.TaskKey)
}

// The absolute ceiling on a credential that left this machine.
//
// Not the revocation — the executor's defer is, and it fires however the run
// ended. This is the backstop for a copy that outlived the process that minted
// it, and it is why a remote token is a different object from a loopback one.
func TestAnExpiredTokenIsRefusedAndDropped(t *testing.T) {
	now := time.Now()
	tokens := NewRunTokenRegistry()
	tokens.now = func() time.Time { return now }

	token, err := tokens.Mint(Run{
		Ctx:       context.Background(),
		TaskKey:   "tt-remote",
		ExpiresAt: now.Add(time.Minute),
	})
	require.NoError(t, err)

	_, ok := tokens.Lookup(token)
	assert.True(t, ok, "inside its window the credential works")

	now = now.Add(time.Minute + time.Second)
	_, ok = tokens.Lookup(token)
	assert.False(t, ok, "a token must not outlive its ceiling")
	assert.Equal(t, 0, tokens.Live(),
		"an expired entry is dropped, not left in the map for a process that never got to revoke it")
}

// A loopback token has no ceiling: it never leaves the host, and the only copy
// of it dies with the process. Its lifetime is its run's, ended by the
// executor's revoke.
func TestATokenWithNoExpiryNeverAgesOut(t *testing.T) {
	tokens := NewRunTokenRegistry()
	tokens.now = func() time.Time { return time.Now().Add(100 * 365 * 24 * time.Hour) }

	token, err := tokens.Mint(Run{Ctx: context.Background(), TaskKey: "tt-local"})
	require.NoError(t, err)
	_, ok := tokens.Lookup(token)
	assert.True(t, ok)
}

// No token, a malformed one and an unknown one are all 401 — never a request
// served anyway. This is the whole security property of the path: there is no
// other credential on it and no default to fall back to.
func TestRequestsWithoutAValidTokenAreRefused(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, tokens, token := newTestServer(t, reg, Run{Ctx: context.Background()})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

	for _, presented := range []string{"", "not-the-token", token + "x"} {
		resp, _ := call(t, app, presented, body)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	}
	assert.Nil(t, reg.lastCtx, "an unauthenticated call must never reach the registry")

	// And the same request with the real token is served, so the assertions
	// above are about the credential and not about the request.
	resp, _ := call(t, app, token, body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// A token whose run has ended is in the same class as one that never
	// existed: refused, not served.
	tokens.Revoke(token)
	resp, _ = call(t, app, token, body)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// The /api prefix is the control plane's, stripped before the request reaches
// this process. Stated as a test because the two halves live in two
// repositories and only agree by construction.
func TestPublicURL(t *testing.T) {
	assert.Equal(t, "https://app.example.com/api/mcp", PublicURL("https://app.example.com"))
	assert.Equal(t, "https://app.example.com/api/mcp", PublicURL("https://app.example.com/"))
	assert.Equal(t, "https://app.example.com/api/mcp", PublicURL("  https://app.example.com  "))
	assert.Empty(t, PublicURL(""), "an unconfigured public base is no address, not a relative one")
	assert.Equal(t, PublicPath, "/api"+Path)
}
