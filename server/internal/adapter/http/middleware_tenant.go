package http

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// tenantMiddleware puts the one thing the rest of the process needs on the
// request context: a tenant.
//
// One machine, one person, who owns it. The tenant is a FIXED uuid rather than
// a generated one, or every restart would look like a new tenant. The workspace
// path helpers still derive directories from it and refuse without one.
func (h *Handler) tenantMiddleware(c *fiber.Ctx) error {
	// The GitHub webhook is public — GitHub holds no credential of ours and
	// authenticates with the per-repository HMAC over the raw body — but it
	// still gets the identity every other request gets. Short-circuiting it
	// here is what once dropped every push, with a green 204 and nothing red
	// anywhere. Public paths get the identity too; only the board seed below is
	// withheld from them.
	webhook := c.Path() == githubWebhookPath
	public := !webhook && h.isPublicPath(c.Path())
	id := tenant.Identity{TenantID: tenant.LocalTenantID, Role: tenant.RoleOwner}

	ctx := tenant.With(c.UserContext(), id)
	c.SetUserContext(ctx)
	c.Locals("tenant_id", id.TenantID.String())
	c.Locals("tenant_role", string(id.Role))

	// Seeding a first-seen tenant happens here, on the first request, rather
	// than in a provisioning step nobody runs. Failures are logged and the
	// request continues: a half-seeded board is better than a 500 on boot.
	//
	// Never on the webhook path. Sight's first job is ensureSeeded, which for a
	// tenant it has not seen INSERTs a tenants row and runs the whole board
	// seed — 13 columns, counters, provider list, model prices. A GitHub
	// delivery arrives on an endpoint anybody on the internet can POST to and
	// must not be able to conjure a workspace nobody signed up for.
	if h.tenantOnboarder != nil && !webhook && !public {
		if err := h.tenantOnboarder.Sight(ctx, id); err != nil {
			log.Warn().Err(err).Str("tenant_id", id.TenantID.String()).
				Msg("tenant sight failed; roster and board seed may be stale")
		}
	}
	return c.Next()
}

// TenantOnboarder is the half of tenantboot.Service this layer needs. Declared
// here rather than imported as a concrete type so the handler stays testable
// without a database, and so a build with no Postgres can leave it nil.
type TenantOnboarder interface {
	Sight(ctx context.Context, id tenant.Identity) error
	Booting() bool
}
