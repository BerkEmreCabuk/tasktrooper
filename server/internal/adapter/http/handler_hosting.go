package http

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/hosting"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerHostingRoutes exposes the Vercel connection (Settings) and each
// repository's hosting links: where its frontend/backend actually live.
func (h *Handler) registerHostingRoutes(app fiber.Router) {
	if h.hostingSvc == nil {
		return
	}
	app.Get("/v1/settings/vercel", h.VercelStatus)
	app.Put("/v1/settings/vercel", h.ConnectVercel)
	app.Delete("/v1/settings/vercel", h.DisconnectVercel)
	app.Get("/v1/settings/vercel/teams", h.VercelTeams)
	app.Get("/v1/settings/vercel/projects", h.VercelProjects)
	app.Get("/v1/repositories/:id/hosting/detect", h.DetectHosting)
	app.Get("/v1/repositories/:id/hosting/links", h.ListHostingLinks)
	app.Put("/v1/repositories/:id/hosting/links", h.SaveHostingLink)
	app.Delete("/v1/repositories/:id/hosting/links/:area", h.DeleteHostingLink)
}

// hostingError maps the service's sentinels: a caller mistake is 400, "connect
// Vercel first" is 409 (a configuration gap the caller can go fix), an absent
// repository is 404, and anything else is ours.
func hostingError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, hosting.ErrInvalidInput):
		return badRequest(c, err.Error())
	case errors.Is(err, hosting.ErrNotConnected):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, port.ErrNotFound):
		return notFound(c, err.Error())
	default:
		return internalError(c, err)
	}
}

// VercelStatus — GET /v1/settings/vercel
func (h *Handler) VercelStatus(c *fiber.Ctx) error {
	st, err := h.hostingSvc.Status(h.enrichContext(c))
	if err != nil {
		return hostingError(c, err)
	}
	return c.JSON(st)
}

// connectVercelRequest is the ConnectVercel body. A token connects (or
// replaces the connection) and pins the team; team_id alone re-scopes an
// existing connection. team_id is a pointer so "" (personal account) can be
// told apart from "not sent".
type connectVercelRequest struct {
	Token  string  `json:"token"`
	TeamID *string `json:"team_id"`
}

// ConnectVercel — PUT /v1/settings/vercel {"token":"…","team_id":"team_…"}
func (h *Handler) ConnectVercel(c *fiber.Ctx) error {
	var req connectVercelRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ctx := h.enrichContext(c)
	token := strings.TrimSpace(req.Token)
	switch {
	case token != "":
		team := ""
		if req.TeamID != nil {
			team = *req.TeamID
		}
		st, err := h.hostingSvc.Connect(ctx, token, team)
		if err != nil {
			return hostingError(c, err)
		}
		return c.JSON(st)
	case req.TeamID != nil:
		st, err := h.hostingSvc.SetTeam(ctx, *req.TeamID)
		if err != nil {
			return hostingError(c, err)
		}
		return c.JSON(st)
	default:
		return badRequest(c, "token or team_id is required")
	}
}

// DisconnectVercel — DELETE /v1/settings/vercel
func (h *Handler) DisconnectVercel(c *fiber.Ctx) error {
	if err := h.hostingSvc.Disconnect(h.enrichContext(c)); err != nil {
		return hostingError(c, err)
	}
	return c.JSON(domain.VercelConnectionStatus{Connected: false})
}

// VercelTeams — GET /v1/settings/vercel/teams
func (h *Handler) VercelTeams(c *fiber.Ctx) error {
	teams, err := h.hostingSvc.Teams(h.enrichContext(c))
	if err != nil {
		return hostingError(c, err)
	}
	return c.JSON(fiber.Map{"teams": teams})
}

// VercelProjects — GET /v1/settings/vercel/projects?team_id=
// Without team_id the connection's default team is listed; team_id= (empty)
// lists the personal account.
func (h *Handler) VercelProjects(c *fiber.Ctx) error {
	var teamID *string
	if c.Request().URI().QueryArgs().Has("team_id") {
		v := c.Query("team_id")
		teamID = &v
	}
	projects, err := h.hostingSvc.Projects(h.enrichContext(c), teamID)
	if err != nil {
		return hostingError(c, err)
	}
	return c.JSON(fiber.Map{"projects": projects, "count": len(projects)})
}

// DetectHosting — GET /v1/repositories/:id/hosting/detect
func (h *Handler) DetectHosting(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	det, err := h.hostingSvc.Detect(h.enrichContext(c), id)
	if err != nil {
		return hostingError(c, err)
	}
	return c.JSON(det)
}

// ListHostingLinks — GET /v1/repositories/:id/hosting/links
func (h *Handler) ListHostingLinks(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	links, err := h.hostingSvc.Links(h.enrichContext(c), id)
	if err != nil {
		return hostingError(c, err)
	}
	return c.JSON(fiber.Map{"links": links})
}

// SaveHostingLink — PUT /v1/repositories/:id/hosting/links
func (h *Handler) SaveHostingLink(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req domain.SaveHostingLinkRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	link, err := h.hostingSvc.Link(h.enrichContext(c), id, req)
	if err != nil {
		return hostingError(c, err)
	}
	return c.JSON(link)
}

// DeleteHostingLink — DELETE /v1/repositories/:id/hosting/links/:area
// ("root" addresses the whole-repository area).
func (h *Handler) DeleteHostingLink(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	if err := h.hostingSvc.Unlink(h.enrichContext(c), id, c.Params("area")); err != nil {
		return hostingError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
