package http

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// newAuthPathTestApp uses the router setting runtime.go pins (CaseSensitive) and
// a configured API key, with two protected routes behind authMiddleware.
func newAuthPathTestApp(ran *bool) *fiber.App {
	h := &Handler{legacyAPIKey: "server-api-key"}
	app := fiber.New(fiber.Config{CaseSensitive: true})
	app.Use(h.authMiddleware)
	hit := func(c *fiber.Ctx) error {
		*ran = true
		return c.SendStatus(fiber.StatusNoContent)
	}
	app.Put("/v1/settings/github", hit)
	app.Put("/admin/billing/plan", hit)
	return app
}

func doAuthPath(t *testing.T, app *fiber.App, method, path, key string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp.StatusCode
}

// isPublicPath calls anything outside `/v1` and `/admin` public, and it reads
// the raw path. A router that matched `/V1/settings` case-insensitively would
// serve a protected handler with no API key, which is why runtime.go pins
// CaseSensitive on.
func TestMixedCasePathsDoNotBypassTheAPIKey(t *testing.T) {
	for _, path := range []string{
		"/V1/settings/github",
		"/v1/Settings/github",
		"/Admin/billing/plan",
		"/ADMIN/BILLING/PLAN",
	} {
		ran := false
		app := newAuthPathTestApp(&ran)
		doAuthPath(t, app, "PUT", path, "")
		if ran {
			t.Errorf("PUT %s: handler ran without an API key", path)
		}
	}

	ran := false
	app := newAuthPathTestApp(&ran)
	if status := doAuthPath(t, app, "PUT", "/v1/settings/github", ""); status != fiber.StatusUnauthorized {
		t.Errorf("lowercase path without a key: status %d, want 401", status)
	}
	if status := doAuthPath(t, app, "PUT", "/v1/settings/github", "server-api-key"); status != fiber.StatusNoContent || !ran {
		t.Errorf("lowercase path with the key: status %d, ran %v; want 204 and the handler", status, ran)
	}
}

// Percent-encoding and doubled slashes are the other way to look like a
// different path to isPublicPath than to the router. They are not: Fiber leaves
// UnescapePath off, so the router matches on the same raw bytes c.Path()
// returns, and none of these reach a handler. Pinned because turning
// UnescapePath on would reopen exactly that divergence.
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
		app := newAuthPathTestApp(&ran)
		status := doAuthPath(t, app, "PUT", path, "")
		if ran {
			t.Errorf("%s: handler ran", path)
		}
		if status == fiber.StatusNoContent {
			t.Errorf("%s: status 204, want a refusal", path)
		}
	}
}
