package http

import (
	"github.com/gofiber/fiber/v2"
)

// UsageSummary serves token usage aggregates for the cost dashboard.
// GET /v1/usage?days=30
func (h *Handler) UsageSummary(c *fiber.Ctx) error {
	if h.usageStore == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "usage tracking is disabled")
	}
	days := c.QueryInt("days", 30)
	if days < 1 || days > 365 {
		days = 30
	}
	summary, err := h.usageStore.Summary(c.UserContext(), days)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(summary)
}
