package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// emptyRegistry is enough for the routing questions this file asks: whether a
// request reaches the MCP endpoint at all, and which auth decides.
type emptyRegistry struct{}

var _ port.ToolRegistry = emptyRegistry{}

func (emptyRegistry) Register(port.ToolExecutor)                                     {}
func (emptyRegistry) Definitions() []domain.ToolDefinition                           { return nil }
func (emptyRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition { return nil }
func (emptyRegistry) AllToolNames() []string                                         { return nil }

func (emptyRegistry) Execute(context.Context, domain.ToolCall) domain.ToolResult {
	return domain.ToolResult{}
}

func (emptyRegistry) ExecuteWithPolicy(context.Context, domain.ToolCall, domain.ToolPolicy) domain.ToolResult {
	return domain.ToolResult{}
}

func routePaths(app *fiber.App) map[string]bool {
	routes := make(map[string]bool)
	for _, r := range app.GetRoutes() {
		routes[r.Method+" "+r.Path] = true
	}
	return routes
}

// The endpoint is mounted with the executor and only with it: on a host without
// the Claude Code CLI there is no session to serve, so the route must not exist
// rather than sit there answering 401 forever.
func TestMCPToolRoutesAreMountedWithTheExecutor(t *testing.T) {
	withServer := fiber.New()
	(&Handler{mcpToolServer: mcpserver.New(emptyRegistry{}, mcpserver.NewRunTokenRegistry())}).RegisterRoutes(withServer)
	routes := routePaths(withServer)
	assert.True(t, routes["POST /mcp"], "the transport")
	// GET and DELETE exist only so they answer 405 instead of falling through
	// to the SPA, which would hand a protocol client an HTML page.
	assert.True(t, routes["GET /mcp"])
	assert.True(t, routes["DELETE /mcp"])

	without := fiber.New()
	(&Handler{}).RegisterRoutes(without)
	assert.NotContains(t, routePaths(without), "POST /mcp")
}

// The Claude Code CLI is a child process that deliberately holds none of this
// server's credentials — no API key. If the auth middleware applied to /mcp,
// the only client the route exists for could never reach it.
func TestMCPToolEndpointBypassesTheAuthMiddleware(t *testing.T) {
	assert.True(t, (&Handler{}).isPublicPath(mcpserver.Path),
		"the per-run bearer token is this route's own credential")

	tokens := mcpserver.NewRunTokenRegistry()
	runToken, err := tokens.Mint(mcpserver.Run{Ctx: context.Background(), TaskKey: "tt-42"})
	require.NoError(t, err)

	// With an API key configured, the middleware would otherwise demand a
	// credential the child has not got.
	for name, h := range map[string]*Handler{
		"api key mode": {legacyAPIKey: "server-api-key"},
	} {
		t.Run(name, func(t *testing.T) {
			app := fiber.New()
			{
				app.Use(h.authMiddleware)
			}
			mcpserver.New(emptyRegistry{}, tokens).Register(app)

			const body = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

			req := httptest.NewRequest("POST", mcpserver.Path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+runToken)
			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusOK, resp.StatusCode, "the run token is the endpoint's own credential")

			// And that credential is the one that decides: whatever else a
			// caller holds, without a live run token it gets nothing.
			req = httptest.NewRequest("POST", mcpserver.Path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer server-api-key")
			resp, err = app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode, "the server API key is not a run credential")
		})
	}
}
