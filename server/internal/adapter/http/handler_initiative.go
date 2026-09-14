package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) ListInitiativeProjects(c *fiber.Ctx) error {
	projects, err := h.initiativeSvc.List(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"projects": projects, "count": len(projects)})
}

func (h *Handler) CreateInitiativeProject(c *fiber.Ctx) error {
	var req domain.CreateInitiativeProjectRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	project, err := h.initiativeSvc.Create(h.enrichContext(c), req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(project)
}

func (h *Handler) GetInitiativeProject(c *fiber.Ctx) error {
	projectID, err := uuid.Parse(c.Params("projectId"))
	if err != nil {
		return badRequest(c, "invalid project id")
	}
	project, err := h.initiativeSvc.Get(h.enrichContext(c), projectID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.JSON(project)
}

func (h *Handler) UpdateInitiativeProject(c *fiber.Ctx) error {
	projectID, err := uuid.Parse(c.Params("projectId"))
	if err != nil {
		return badRequest(c, "invalid project id")
	}
	var req domain.UpdateInitiativeProjectRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	project, err := h.initiativeSvc.Update(h.enrichContext(c), projectID, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(project)
}

func (h *Handler) DeleteInitiativeProject(c *fiber.Ctx) error {
	projectID, err := uuid.Parse(c.Params("projectId"))
	if err != nil {
		return badRequest(c, "invalid project id")
	}
	if err := h.initiativeSvc.Delete(h.enrichContext(c), projectID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "not_found"}})
	}
	return c.SendStatus(fiber.StatusNoContent)
}
