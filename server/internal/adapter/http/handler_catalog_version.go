package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type restoreVersionRequest struct {
	Version int    `json:"version"`
	Reason  string `json:"reason"`
}

func (h *Handler) ListAgentSkillVersions(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	skillID, err := uuid.Parse(c.Params("skillId"))
	if err != nil {
		return badRequest(c, "invalid skill id")
	}
	versions, err := h.catalogSvc.ListSkillVersions(h.enrichContext(c), agentID, skillID, c.QueryInt("limit", 50))
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(fiber.Map{"versions": orEmptyVersions(versions)})
}

func (h *Handler) RestoreAgentSkillVersion(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	skillID, err := uuid.Parse(c.Params("skillId"))
	if err != nil {
		return badRequest(c, "invalid skill id")
	}
	var req restoreVersionRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Version <= 0 {
		return badRequest(c, "version is required")
	}
	restored, err := h.catalogSvc.RestoreSkillVersion(h.enrichContext(c), agentID, skillID, req.Version)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(restored)
}

func (h *Handler) ListAgentRuleVersions(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	ruleID, err := uuid.Parse(c.Params("ruleId"))
	if err != nil {
		return badRequest(c, "invalid rule id")
	}
	versions, err := h.catalogSvc.ListRuleVersions(h.enrichContext(c), agentID, ruleID, c.QueryInt("limit", 50))
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(fiber.Map{"versions": orEmptyVersions(versions)})
}

func (h *Handler) RestoreAgentRuleVersion(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	ruleID, err := uuid.Parse(c.Params("ruleId"))
	if err != nil {
		return badRequest(c, "invalid rule id")
	}
	var req restoreVersionRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Version <= 0 {
		return badRequest(c, "version is required")
	}
	restored, err := h.catalogSvc.RestoreRuleVersion(h.enrichContext(c), agentID, ruleID, req.Version)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(restored)
}

func orEmptyVersions(versions []domain.CatalogVersion) []domain.CatalogVersion {
	if versions == nil {
		return []domain.CatalogVersion{}
	}
	return versions
}
