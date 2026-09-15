package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/vercelops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerVercelOpsRoutes exposes the Vercel project picker and each
// repository's (and sub-project's) Vercel project binding. Same auth/middleware
// chain as every other route — none of these invent a new one.
func (h *Handler) registerVercelOpsRoutes(app fiber.Router) {
	if h.vercelOpsSvc == nil {
		return
	}
	app.Get("/v1/vercel/projects", h.ListVercelProjects)
	app.Put("/v1/repositories/:id/vercel/project", h.LinkVercelProject)
	app.Get("/v1/repositories/:id/vercel/project", h.VercelProjectDetails)
	app.Delete("/v1/repositories/:id/vercel/project", h.UnlinkVercelProject)
}

// vercelOpsError maps the service's sentinels onto the codes the console
// branches on, the same errors.Is-dispatch storeOpsAppError uses: the caller's
// own mistake (a blank project id, a sub-project path this repository does not
// have, a project the token cannot read) is a 400; "connect Vercel first" is a
// 409, matching hostingError on the neighbouring Vercel surface, because it is
// a configuration gap the caller can go fix rather than a malformed request;
// an absent repository or link row is a 404; anything else — a database
// failure, a Vercel outage, a cipher that will not decrypt — is this server's
// problem and must report 500 rather than telling the caller their request was
// wrong.
//
// ErrListingUnavailable is deliberately absent: it never reaches here, because
// the one route that can raise it answers 200 (see ListVercelProjects).
func vercelOpsError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, vercelops.ErrInvalidInput), errors.Is(err, vercelops.ErrProjectUnreachable):
		return badRequest(c, err.Error())
	case errors.Is(err, vercelops.ErrNotConnected):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, port.ErrNotFound):
		return notFound(c, err.Error())
	default:
		return internalError(c, err)
	}
}

// vercelProjectListing is the ListVercelProjects response, and is deliberately
// the same shape as storeAppListing: listing_available stated rather than
// inferred from an empty array, and the same two reasons, because the console
// reacts to them the same way on both surfaces. "this account has no projects"
// and "these projects cannot be enumerated" are different answers and must not
// collapse into one "listing failed" screen.
type vercelProjectListing struct {
	Available bool `json:"listing_available"`
	// Reason says WHY the list is empty when Available is false:
	// "not_connected" sends the operator to Settings to paste a token,
	// "listing_unsupported" means a token IS stored and Vercel refuses it, so
	// the next step is a fresh token rather than a retry. Empty when Available
	// is true.
	Reason   string                 `json:"reason,omitempty"`
	Projects []domain.VercelProject `json:"projects"`
}

// ListVercelProjects — GET /v1/vercel/projects
// The picker's source: every project the connected token can see, across the
// personal account and each team it belongs to.
//
// Neither empty case is a 5xx. No token is the state every install starts in,
// and a token Vercel refuses is a stable property of that token — the
// operator's next step is Settings, not a retry, and a 500 would tell them to
// wait for something that will never start working on its own.
func (h *Handler) ListVercelProjects(c *fiber.Ctx) error {
	projects, err := h.vercelOpsSvc.ListProjects(h.enrichContext(c))
	if err != nil {
		if errors.Is(err, vercelops.ErrNotConnected) {
			return c.JSON(vercelProjectListing{
				Available: false, Reason: listingReasonNotConnected, Projects: []domain.VercelProject{},
			})
		}
		if errors.Is(err, vercelops.ErrListingUnavailable) {
			return c.JSON(vercelProjectListing{
				Available: false, Reason: listingReasonUnsupported, Projects: []domain.VercelProject{},
			})
		}
		return vercelOpsError(c, err)
	}
	if projects == nil {
		projects = []domain.VercelProject{}
	}
	return c.JSON(vercelProjectListing{Available: true, Projects: projects})
}

// linkVercelProjectRequest is the LinkVercelProject body: one project as the
// picker listed it. sub_project_path is optional and defaults to "" — the
// repository as a whole. No team is carried: the service resolves the project
// in whichever scope the token can actually read it, so a picker working from
// a stale listing cannot bind a project the server never confirmed.
type linkVercelProjectRequest struct {
	ProjectID      string `json:"project_id"`
	SubProjectPath string `json:"sub_project_path"`
}

// LinkVercelProject — PUT /v1/repositories/:id/vercel/project
// Body: {"project_id": "prj_…", "sub_project_path": "web"}.
func (h *Handler) LinkVercelProject(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req linkVercelProjectRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	link, err := h.vercelOpsSvc.LinkProject(h.enrichContext(c), id, req.SubProjectPath, req.ProjectID)
	if err != nil {
		return vercelOpsError(c, err)
	}
	return c.JSON(link)
}

// VercelProjectDetails — GET /v1/repositories/:id/vercel/project?sub_project_path=
// The linked project plus what Vercel currently says about it: the production
// address, the last deployment's state / time / commit, the framework, and the
// last failed build if there is one.
//
// A live read that fails comes back 200 with the recorded values and a warning
// rather than an error, because the link row is durable and worth rendering on
// its own — see vercelops.Service.ProjectDetails. Only a missing link is a 404.
func (h *Handler) VercelProjectDetails(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	details, err := h.vercelOpsSvc.ProjectDetails(h.enrichContext(c), id, c.Query("sub_project_path"))
	if err != nil {
		return vercelOpsError(c, err)
	}
	return c.JSON(details)
}

// UnlinkVercelProject — DELETE /v1/repositories/:id/vercel/project?sub_project_path=
func (h *Handler) UnlinkVercelProject(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	if err := h.vercelOpsSvc.Unlink(h.enrichContext(c), id, c.Query("sub_project_path")); err != nil {
		return vercelOpsError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
