package http

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// The whole gate rests on adminRoutes being written in the same shape
// matchAdminRoute normalises to. A rule added with a capital letter would
// silently never match anything.
func TestAdminRoutesAreLowercase(t *testing.T) {
	for _, rule := range adminRoutes {
		if rule.prefix != strings.ToLower(rule.prefix) {
			t.Errorf("adminRoutes prefix %q is not lowercase; matchAdminRoute would never match it", rule.prefix)
		}
		for _, suffix := range rule.memberSuffixes {
			if suffix != strings.ToLower(suffix) {
				t.Errorf("adminRoutes suffix %q under %q is not lowercase", suffix, rule.prefix)
			}
		}
	}
}

// Fiber routes on a lowercased path while c.Path() hands back the raw one, so
// a member could reach an admin handler just by capitalising a letter.
func TestMatchAdminRouteIgnoresCase(t *testing.T) {
	for _, path := range []string{
		"/V1/repositories/abc/store/apps/android/promote",
		"/v1/Store/Credentials/asc",
		"/V1/store/credentials/google-play",
		"/Admin/api-keys",
		"/ADMIN/mcp-servers",
		"/V1/settings/github",
		"/v1/SETTINGS/vercel",
		"/V1/board/columns",
		"/v1/Board/transitions",
		"/V1/llm/providers",
		"/V1/projects",
		"/V1/repositories/abc",
		"/v1/Repositories/abc/deploy/prod/dispatch",
		"/V1/agents/a1/subscriptions",
	} {
		if _, ok := matchAdminRoute(path); !ok {
			t.Errorf("%s: not gated as an admin route", path)
		}
	}
}

// The exemptions have to survive the same normalisation, or capitalising a
// board URL would turn ordinary member work into a 403.
func TestMatchAdminRouteExemptionsIgnoreCase(t *testing.T) {
	for _, path := range []string{
		"/v1/Repositories/r1/Tasks",
		"/V1/repositories/r1/TASKS/t1/comments",
		"/V1/repositories/r1/Index/Search",
		"/V1/repositories/r1/Incidents",
		"/V1/repositories/r1/Webhook",
		"/V1/agents/a1/Memories",
		"/V1/agents/a1/Reflect",
	} {
		if rule, ok := matchAdminRoute(path); ok {
			t.Errorf("%s: gated as %q, but it is member work", path, rule.why)
		}
	}
}

// Nothing about the lowercase behaviour may move: this is the classification
// the product already runs on.
func TestMatchAdminRouteLowercaseUnchanged(t *testing.T) {
	gated := []string{
		"/admin/agents",
		"/admin/api-keys",
		"/admin/billing/plan",
		"/v1/settings/github",
		"/v1/board/columns",
		"/v1/llm/providers",
		"/v1/store/credentials/asc",
		"/v1/projects",
		"/v1/repositories/import",
		"/v1/repositories/r1",
		"/v1/repositories/r1/store/apps/android/promote",
		"/v1/agents/a1/subscriptions",
	}
	for _, path := range gated {
		if _, ok := matchAdminRoute(path); !ok {
			t.Errorf("%s: should be gated", path)
		}
	}

	open := []string{
		"/health",
		"/v1/chat/completions",
		"/v1/repositories/r1/tasks",
		"/v1/repositories/r1/tasks/t1/comments",
		"/v1/repositories/r1/index/search",
		"/v1/repositories/r1/incidents",
		"/v1/repositories/r1/webhook",
		"/v1/agents/a1/memories",
		"/v1/agents/a1/reflect",
	}
	for _, path := range open {
		if rule, ok := matchAdminRoute(path); ok {
			t.Errorf("%s: gated as %q, but it should be open", path, rule.why)
		}
	}
}

func newRoleCaseTestApp(ran *bool) *fiber.App {
	h := &Handler{}
	// Default config on purpose: this is Fiber's routing behaviour with
	// CaseSensitive off, which is the shape the bug lived in.
	app := fiber.New()
	app.Use(h.roleMiddleware)
	hit := func(c *fiber.Ctx) error {
		*ran = true
		return c.SendStatus(fiber.StatusNoContent)
	}
	app.Put("/v1/settings/github", hit)
	app.Post("/v1/repositories/:id/store/apps/android/promote", hit)
	app.Put("/admin/billing/plan", hit)
	app.Post("/v1/repositories/:id/tasks", hit)
	return app
}

func doRole(t *testing.T, app *fiber.App, method, path string, role tenant.Role) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if role != "" {
		req.Header.Set("X-Test-Role", string(role))
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp.StatusCode
}

// A member capitalising one letter must not reach an admin handler. The
// request carries no identity at all, so the role on the context is the
// least-privilege default and the gate is the only thing standing there.
func TestRoleMiddlewareRefusesMixedCaseMutation(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{"PUT", "/V1/settings/github"},
		{"PUT", "/v1/Settings/github"},
		{"POST", "/V1/Repositories/abc/store/apps/android/promote"},
		{"POST", "/v1/repositories/abc/Store/Apps/Android/Promote"},
		{"PUT", "/Admin/billing/plan"},
		{"PUT", "/ADMIN/BILLING/PLAN"},
	} {
		ran := false
		app := newRoleCaseTestApp(&ran)
		status := doRole(t, app, tc.method, tc.path, "")
		if ran {
			t.Errorf("%s %s: handler ran — role gate bypassed", tc.method, tc.path)
		}
		if status != fiber.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403", tc.method, tc.path, status)
		}
	}
}

// The lowercase equivalents must behave exactly as before: refused for a
// member, allowed for an admin, and board work left alone.
func TestRoleMiddlewareLowercaseUnchanged(t *testing.T) {
	ran := false
	app := newRoleCaseTestApp(&ran)
	if status := doRole(t, app, "PUT", "/v1/settings/github", ""); status != fiber.StatusForbidden {
		t.Errorf("lowercase admin mutation: status %d, want 403", status)
	}
	if ran {
		t.Error("lowercase admin mutation reached the handler")
	}

	ran = false
	app = newRoleCaseTestApp(&ran)
	if status := doRole(t, app, "POST", "/v1/repositories/abc/tasks", ""); status != fiber.StatusNoContent {
		t.Errorf("member board write: status %d, want 204", status)
	}
	if !ran {
		t.Error("member board write did not reach the handler")
	}

	ran = false
	app = newRoleCaseTestApp(&ran)
	if status := doRole(t, app, "GET", "/v1/settings/github", ""); status == fiber.StatusForbidden {
		t.Error("a read was refused; reads are never gated")
	}
}

// An admin still gets through on either spelling.
func TestRoleMiddlewareAdminPassesEitherCase(t *testing.T) {
	for _, path := range []string{"/v1/settings/github", "/V1/Settings/GitHub"} {
		ran := false
		h := &Handler{}
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			c.SetUserContext(tenant.With(c.UserContext(), tenant.Identity{
				TenantID: uuid.New(),
				Role:     tenant.RoleAdmin,
			}))
			return c.Next()
		})
		app.Use(h.roleMiddleware)
		app.Put("/v1/settings/github", func(c *fiber.Ctx) error {
			ran = true
			return c.SendStatus(fiber.StatusNoContent)
		})
		if status := doRole(t, app, "PUT", path, ""); status != fiber.StatusNoContent {
			t.Errorf("%s: admin got %d, want 204", path, status)
		}
		if !ran {
			t.Errorf("%s: admin did not reach the handler", path)
		}
	}
}

// Percent-encoding and doubled slashes were the other suspected way to look
// like a different path to the gate than to the router. They are not: Fiber
// leaves UnescapePath off, so the router matches on the same raw bytes
// c.Path() returns, and none of these reach a handler at all. Pinned because
// turning UnescapePath on would reopen exactly the divergence this file fixes.
func TestEncodedPathsDoNotReachHandlers(t *testing.T) {
	for _, path := range []string{
		"/%76%31/settings/github",
		"/v1/%73ettings/github",
		"//v1/settings/github",
		"/v1//settings/github",
		"/v1/./settings/github",
		"/v1/settings/../settings/github",
	} {
		ran := false
		app := newRoleCaseTestApp(&ran)
		status := doRole(t, app, "PUT", path, "")
		if ran {
			t.Errorf("%s: handler ran", path)
		}
		if status == fiber.StatusNoContent {
			t.Errorf("%s: status 204, want a refusal", path)
		}
	}
}
