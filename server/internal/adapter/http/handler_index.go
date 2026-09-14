package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func (h *Handler) registerIndexRoutes(app fiber.Router) {
	h.registerDeployOpsRoutes(app)
	if h.indexSvc == nil {
		return
	}
	app.Post("/v1/sessions/:id/index", h.IndexSession)
	app.Get("/v1/sessions/:id/index/status", h.IndexStatus)
	app.Patch("/v1/sessions/:id/project-root", h.SetProjectRoot)
}

type setProjectRootRequest struct {
	ProjectRoot string `json:"project_root"`
}

func (h *Handler) IndexSession(c *fiber.Ctx) error {
	if h.sessionSvc == nil || h.indexSvc == nil {
		return sessionsDisabled(c)
	}
	sessionID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid session id")
	}
	sess, _, err := h.sessionSvc.Get(c.UserContext(), sessionID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	root := sess.ProjectRoot
	if root == "" {
		root = sess.WorkspaceDir
	}
	idx, err := h.indexSvc.IndexSession(c.UserContext(), sessionID, root)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(idx)
}

func (h *Handler) IndexStatus(c *fiber.Ctx) error {
	if h.indexSvc == nil {
		return sessionsDisabled(c)
	}
	sessionID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid session id")
	}
	idx, err := h.indexSvc.GetStatus(c.UserContext(), sessionID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.JSON(idx)
}

func (h *Handler) SetProjectRoot(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	sessionID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid session id")
	}
	var req setProjectRootRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.ProjectRoot == "" {
		return badRequest(c, "project_root is required")
	}
	sess, err := h.sessionSvc.SetProjectRoot(c.UserContext(), sessionID, req.ProjectRoot, h.indexAllowedRoots)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(sess)
}
