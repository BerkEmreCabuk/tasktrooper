package http

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// adminRule names one route group whose MUTATIONS reconfigure the tenant.
//
// Reads are never gated: a member is on the team and may look at the board's
// configuration, the repository list and the provider status — hiding those
// only makes the UI lie about why a button does nothing. What a member may not
// do is change them.
type adminRule struct {
	// prefix is matched against the request path. Lowercase, always:
	// matchAdminRoute normalises the path it is handed, so a rule spelled with
	// a capital could never match anything.
	prefix string
	// memberSuffixes are paths UNDER prefix that go back to the member: parts
	// of an admin-shaped URL space that are ordinary board work. They are
	// matched as substrings of the remainder, because the segment they sit
	// after is a repository or agent id.
	memberSuffixes []string
	// why is what the log line and the refusal say.
	why string
}

// adminRoutes is the explicit list. Explicit, and not a pattern like "every
// PUT", because the two are genuinely different: `PUT /v1/board/columns`
// rewrites the board for everybody and `PATCH /v1/repositories/:id/tasks/:id`
// moves one card. A rule that could not tell them apart would either lock
// members out of the product or leave the tenant's configuration open to all.
var adminRoutes = []adminRule{
	// The whole /admin surface: the agent catalog, their skills, rules, KPIs
	// and golden tasks, API keys, MCP servers, the billing plan and the model
	// price list. Every one of these is tenant-wide.
	{prefix: "/admin", why: "agent, skill, rule, api key, MCP and billing catalogs"},

	// Tenant settings: GitHub and Vercel credentials, mobile devices, the
	// workspace root, the default language.
	{prefix: "/v1/settings", why: "tenant settings"},

	// Board configuration: columns, transitions, which agents are members and
	// which columns they subscribe to, the key prefix. /v1/board/config is a
	// read and is unaffected.
	{prefix: "/v1/board/", why: "board configuration"},

	// LLM providers, endpoints, the embedding provider, and the reindex that
	// an embedding change forces on every repository.
	{prefix: "/v1/llm/", why: "LLM provider configuration"},

	// App Store / Play credentials.
	{prefix: "/v1/store/credentials", why: "store credentials"},

	// The Google Cloud service-account credential. The prefix is
	// /v1/gcloud/ rather than /v1/gcloud/credential so the resource PICKER
	// (/v1/gcloud/resources) is covered too — it is a GET, which isReadMethod
	// lets through anyway, but a future mutation under this prefix must not
	// arrive ungated by default.
	{prefix: "/v1/gcloud/", why: "google cloud credentials"},

	// Initiative projects are the container repositories are imported into.
	{prefix: "/v1/projects", why: "initiative projects"},

	// Repositories: importing, deleting, re-pointing, deploy targets, hosting
	// links, pipeline mapping, lifecycle gates, test strategy, incident policy,
	// profile proposals, and dispatching or rolling back a deploy.
	//
	// Everything under /tasks is the board and belongs to every member — that
	// is the whole point of the exemption list, and getting it wrong in either
	// direction is the most likely way this file breaks the product.
	// /index/search is a read that happens to be a POST (it carries a query
	// body); /incidents and /webhook are machine ingest paths that arrive with
	// no human role attached.
	{
		prefix:         "/v1/repositories",
		memberSuffixes: []string{"/tasks", "/index/search", "/incidents", "/webhook"},
		why:            "repository configuration",
	},

	// /v1/agents/:id/subscriptions wires an agent onto board columns. The other
	// two branches of this prefix are a member's own work: their agent's
	// memories, and asking an agent to reflect on itself.
	{
		prefix:         "/v1/agents/",
		memberSuffixes: []string{"/memories", "/reflect"},
		why:            "agent board wiring",
	},
}

// roleMiddleware refuses a mutation of tenant-wide configuration to a member.
//
// It reads the role off the request context, which is where tenantMiddleware
// put the value it verified from X-Internal-Role — never off a body, a query
// parameter or a local database row. The mirror in tenant_members is a
// convenience for rendering names; a role that has just been revoked in the
// control plane must not survive in it, so the header on the request being
// served is the only thing consulted here.
//
// 403 rather than 404: the member is legitimately in this tenant and knows the
// route exists. Hiding it would only turn a clear refusal into a bug report.
func (h *Handler) roleMiddleware(c *fiber.Ctx) error {
	// Fiber routes on a LOWERCASED copy of the path (CaseSensitive is off by
	// default) while c.Path() returns the raw one, so the router and everything
	// reading c.Path() can disagree about which path a request is. `/V1/settings`
	// routed straight to the admin handler, matched no rule below, and — worse —
	// isPublicPath's closing `not /v1 and not /admin` test called it public, which
	// is how it also skipped the gateway signature and the tenant. Normalising
	// once here, to the form the router matched on, is what keeps the two views
	// from drifting; runtime.go additionally pins CaseSensitive on so the router
	// cannot accept a spelling the middlewares upstream of this one never see.
	path := strings.ToLower(c.Path())
	if h.isPublicPath(path) || isReadMethod(c.Method()) {
		return c.Next()
	}
	rule, ok := matchAdminRoute(path)
	if !ok {
		return c.Next()
	}
	if tenant.RoleOf(c.UserContext()).CanAdmin() {
		return c.Next()
	}
	log.Warn().Str("path", c.Path()).Str("method", c.Method()).
		Str("group", rule.why).Msg("role check refused a member mutation")
	return c.Status(fiber.StatusForbidden).JSON(errorResponse{
		Error: errorDetail{
			Message: "changing " + rule.why + " requires the admin or owner role",
			Type:    "permission_error",
		},
	})
}

// isReadMethod is the set that changes nothing. HEAD and OPTIONS are here for
// completeness; the router answers OPTIONS itself.
func isReadMethod(method string) bool {
	switch method {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return true
	}
	return false
}

// matchAdminRoute normalises the path itself rather than trusting its caller to
// have done it: a caller handing it a raw c.Path() is the bug this whole file
// exists to keep closed, and it fails open and silently.
func matchAdminRoute(path string) (adminRule, bool) {
	path = strings.ToLower(path)
	for _, rule := range adminRoutes {
		if !strings.HasPrefix(path, rule.prefix) {
			continue
		}
		rest := path[len(rule.prefix):]
		exempt := false
		for _, suffix := range rule.memberSuffixes {
			if strings.Contains(rest, suffix) {
				exempt = true
				break
			}
		}
		if !exempt {
			return rule, true
		}
	}
	return adminRule{}, false
}
