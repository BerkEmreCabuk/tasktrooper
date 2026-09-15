// Package tenant carries WHO a request is acting for through the process.
//
// One agent-server serves every tenant. The identity arrives as three signed
// headers the gateway mints (see internal/platform/internalauth and
// adapter/http/middleware_tenant.go), is turned into an Identity once, at the
// edge, and then rides the context — because the thing that consumes it is not
// a handler but the database layer: postgres.DB opens every statement's
// transaction with `SET LOCAL app.tenant_id` read from here, and row-level
// security does the rest.
//
// That is why this package is deliberately tiny and has no dependency on
// anything else in the tree: 469 store methods take a context and none of them
// take a tenant, so the context IS the tenant parameter.
package tenant

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Role is the caller's role in this tenant, as signed by the control plane.
// The control plane owns the value; nothing here may widen it.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// ErrNoTenant is returned by every database entry point that is reached
// without an identity. It is a programming error rather than a user error: the
// HTTP edge refuses an unidentified request long before a store is called, so
// seeing this means a code path built a context by hand and forgot.
var ErrNoTenant = errors.New("tenant: no tenant in context")

// LocalTenantID is the tenant a deployment with no control plane in front of it
// runs as: self-hosted, and the desktop build. Those have one tenant and no
// gateway to sign a header, and they must keep working exactly as they did
// before this became a multi-tenant server - so they get a FIXED uuid, not a
// generated one, or every restart would look like a new tenant and the previous
// run's rows would become invisible.
//
// Deliberately not the nil uuid: migration 114 uses all-zeros as the
// placeholder its ALTER TABLE statements need, and a real tenant sharing that
// value would inherit whatever a future migration leaves behind.
var LocalTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// ParseRole normalises a signed role, defaulting to the least privileged value.
// An unknown string is NOT an error and is NOT elevated: a control plane that
// invents a new role tomorrow should degrade this server to member, never to
// admin.
func ParseRole(s string) Role {
	switch Role(s) {
	case RoleOwner:
		return RoleOwner
	case RoleAdmin:
		return RoleAdmin
	default:
		return RoleMember
	}
}

// CanAdmin reports whether this role may change tenant-wide configuration —
// LLM settings, board configuration, repositories, deploy targets, MCP
// servers, the skills and rules catalogs. Board work is open to every role and
// is not asked about here.
func (r Role) CanAdmin() bool { return r == RoleOwner || r == RoleAdmin }

// Identity is one caller, in one tenant.
type Identity struct {
	// TenantID is the value that ends up in app.tenant_id. Never uuid.Nil for
	// a valid identity.
	TenantID uuid.UUID
	// Role is this caller's role in TenantID.
	Role Role
	// UserID is the caller's Firebase uid, empty on paths that have no human
	// behind them (background sweeps, self-hosted runs with no gateway). The
	// board records it as actor_user_id and the memory scopes key off it.
	UserID string
}

type ctxKey struct{}

// With attaches an identity. The HTTP middleware and the runtime's root context
// are where it belongs; anywhere else is a bug.
func With(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From returns the identity on ctx. ok is false when there is none, which
// every database call turns into ErrNoTenant rather than a default tenant —
// "some tenant" is the one answer that must never be possible.
func From(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	if !ok || id.TenantID == uuid.Nil {
		return Identity{}, false
	}
	return id, true
}

// ID is From for callers that only need the tenant.
func ID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := From(ctx)
	return id.TenantID, ok
}

// RoleOf returns the caller's role, defaulting to the least privileged one so a
// context assembled without an identity can never pass an admin check.
func RoleOf(ctx context.Context) Role {
	if id, ok := From(ctx); ok {
		return id.Role
	}
	return RoleMember
}
