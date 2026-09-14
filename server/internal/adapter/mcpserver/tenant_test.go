package mcpserver

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// The tenant a tool call executes under comes off the TOKEN.
//
// /mcp is a public path — its caller is a CLI session holding no gateway
// signature and no tenant API key, which is the whole point of the environment
// scrub — so tenantMiddleware never runs for it and there is no signed
// X-Internal-Tenant on the request to read. Whatever this endpoint executes has
// to carry a tenant anyway, because every store method opens its transaction
// with SET LOCAL app.tenant_id and refuses without one.
//
// So the tenant is bound when the credential is minted and re-applied here. The
// property being asserted is that the tenant reaching the registry is the one
// the mint named, not one the caller could influence.
func TestToolCallExecutesUnderTheTokensTenant(t *testing.T) {
	tenantID := uuid.New()
	reg := &fakeRegistry{defs: fullCatalog()}
	app, _, token := newTestServer(t, reg, Run{
		Ctx:     context.Background(),
		Tenant:  tenant.Identity{TenantID: tenantID, Role: tenant.RoleMember, UserID: "uid-7"},
		TaskKey: "tt-42",
	})

	resp, _ := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"move_board_task","arguments":{}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	id, ok := tenant.From(reg.lastCtx)
	require.True(t, ok, "a tool call with no tenant would fail closed at the first statement")
	assert.Equal(t, tenantID, id.TenantID)
	assert.Equal(t, tenant.RoleMember, id.Role)
	assert.Equal(t, "uid-7", id.UserID)
}

// A single-tenant install (self-hosted, desktop) mints with no identity on the
// run context, and must keep behaving exactly as it did: the context is passed
// through untouched rather than being given some invented default.
func TestToolCallWithNoBoundTenantPassesTheContextThrough(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	type marker struct{}
	ctx := context.WithValue(context.Background(), marker{}, "run")
	app, _, token := newTestServer(t, reg, Run{Ctx: ctx, TaskKey: "tt-local"})

	resp, _ := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"move_board_task","arguments":{}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, "run", reg.lastCtx.Value(marker{}))
	_, ok := tenant.From(reg.lastCtx)
	assert.False(t, ok, "no tenant is a fail-closed database call, never a defaulted one")
}

// One token names one run. Two live runs of the same tenant are two
// credentials, and presenting one must serve the other's policy to nobody.
func TestATokenNamesExactlyOneRun(t *testing.T) {
	tenantID := uuid.New()
	tokens := NewRunTokenRegistry()

	first, err := tokens.Mint(Run{
		Ctx:     context.Background(),
		Tenant:  tenant.Identity{TenantID: tenantID},
		TaskKey: "tt-1",
	})
	require.NoError(t, err)
	second, err := tokens.Mint(Run{
		Ctx:     context.Background(),
		Tenant:  tenant.Identity{TenantID: tenantID},
		TaskKey: "tt-2",
	})
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

// A token minted for one tenant cannot be made to act for another: the tenant
// is a property of the entry this process wrote, and the only thing the caller
// presents is the opaque token.
func TestATokenCannotCrossTenants(t *testing.T) {
	tokens := NewRunTokenRegistry()
	acme := tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleMember}
	other := tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner}

	acmeToken, err := tokens.Mint(Run{Ctx: context.Background(), Tenant: acme, TaskKey: "tt-acme"})
	require.NoError(t, err)
	otherToken, err := tokens.Mint(Run{Ctx: context.Background(), Tenant: other, TaskKey: "tt-other"})
	require.NoError(t, err)

	run, ok := tokens.Lookup(acmeToken)
	require.True(t, ok)
	id, ok := tenant.From(run.Scoped())
	require.True(t, ok)
	assert.Equal(t, acme.TenantID, id.TenantID)
	assert.Equal(t, tenant.RoleMember, id.Role)

	run, ok = tokens.Lookup(otherToken)
	require.True(t, ok)
	id, ok = tenant.From(run.Scoped())
	require.True(t, ok)
	assert.Equal(t, other.TenantID, id.TenantID)
	assert.Equal(t, tenant.RoleOwner, id.Role,
		"the role is the run's own too; nothing on the wire can widen it")
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
// served against some tenant. This is the whole security property of the path:
// there is no other credential on it and no default to fall back to.
func TestRequestsWithoutAValidTokenAreRefused(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, tokens, token := newTestServer(t, reg, Run{
		Ctx:    context.Background(),
		Tenant: tenant.Identity{TenantID: uuid.New()},
	})
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
	// existed: refused, not served with a stale tenant.
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
