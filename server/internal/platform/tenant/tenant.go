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
	"github.com/rs/zerolog/log"
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
	// ControlPlane says the control plane made this call on its OWN behalf — a
	// billing push, an OAuth token writeback — rather than proxying a person's
	// request. Read from internalauth.ScopeHeader, which the gateway strips
	// from client-supplied requests and sets only on the calls it originates.
	//
	// Whatever UserID such a call carries is not an acting human, so nothing
	// may record one from it. It only ever WITHDRAWS a claim, which is why the
	// unsigned header is enough: forging it costs the forger the mirror write
	// it would otherwise get.
	ControlPlane bool
}

type ctxKey struct{}

// With attaches an identity. It is called in exactly two places: the HTTP
// middleware that verified the headers, and the background fan-out in
// postgres.DB.EachTenant. Anywhere else is a bug.
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

// UserID returns the acting human's uid, or "" when there is none.
func UserID(ctx context.Context) string {
	id, _ := From(ctx)
	return id.UserID
}

// Detach is how work that OUTLIVES a request keeps the request's tenant.
//
// It is one line, and it exists because the obvious line was wrong in a way
// nothing reported. A handler that starts a goroutine cannot hand it the
// request's context — that context is cancelled the moment the response is
// written — so the idiom throughout this codebase was
//
//	ctx, cancel := context.WithTimeout(context.Background(), timeout)
//
// which drops the cancellation and the identity together. The work then ran
// with no tenant, every store call answered ErrNoTenant, and the only trace was
// one Warn line in a goroutine nobody was watching. Measured on a live stack:
// every repository import on every tenant silently got no project profile and
// no GitHub push webhook.
//
// context.WithoutCancel keeps the values and drops the deadline and the
// cancellation, which is exactly and only what this case wants. It is named
// here rather than called inline so that the name says which of the two the
// caller means to keep — and so grepping for it finds every place that made
// this decision on purpose.
//
// Wrap it in a WithTimeout at the call site: detached must not mean unbounded.
func Detach(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// --- fleet-wide background work -------------------------------------------

// Lister answers "which tenants does this database serve". It is satisfied by
// postgres.DB, which reads the `tenants` registry (migration 115).
type Lister interface {
	Tenants(ctx context.Context) ([]uuid.UUID, error)
}

type listerKey struct{}

// WithLister installs the fan-out source. Called once, in platform/runtime,
// on the context every background loop is started with.
func WithLister(ctx context.Context, l Lister) context.Context {
	return context.WithValue(ctx, listerKey{}, l)
}

// EachTenant runs fn once per tenant, on a context scoped to that tenant.
//
// It exists because the background loops — the reconciler, the quota, deploy,
// device and work-order sweepers, the pipeline gate, the health and store
// monitors — were written when "all rows" and "this tenant's rows" were the
// same set. Under row-level security they are not, and a sweep that runs
// un-scoped now raises ErrNoTenant instead of quietly doing nothing, so each
// one calls this in place of its bare tick.
//
// With no lister on ctx (self-hosted, desktop, and every existing test) fn runs
// exactly once on ctx unchanged. That is deliberate: those deployments have one
// tenant, already on the context, and the fan-out must not become a second code
// path they never exercise.
//
// A failure to list is not silently a no-op — it is reported to the caller,
// which logs it. fn's own errors belong to fn; one tenant's bad sweep must not
// stop the next tenant's.
func EachTenant(ctx context.Context, fn func(context.Context)) error {
	lister, ok := ctx.Value(listerKey{}).(Lister)
	if !ok || lister == nil {
		fn(ctx)
		return nil
	}
	ids, err := lister.Tenants(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Least privilege: a background sweep has no human behind it and makes
		// no role decisions, so if one ever grows a role check it gets the
		// answer a stranger would.
		fn(With(ctx, Identity{TenantID: id, Role: RoleMember}))
	}
	return nil
}

// Sweep is EachTenant for a background loop: it runs one tick per tenant and
// says so when the fan-out itself failed, which is otherwise indistinguishable
// from a pass that found nothing to do.
func Sweep(ctx context.Context, name string, tick func(context.Context)) {
	if err := EachTenant(ctx, tick); err != nil {
		log.Warn().Err(err).Str("sweeper", name).
			Msg("tenant fan-out failed, sweep skipped this pass")
	}
}
