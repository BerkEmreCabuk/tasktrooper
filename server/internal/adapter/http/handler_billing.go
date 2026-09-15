package http

import (
	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// BillingStatus — GET /v1/billing — the user-facing usage view (token-denominated).
func (h *Handler) BillingStatus(c *fiber.Ctx) error {
	if h.billingSvc == nil {
		return c.JSON(domain.BillingStatus{Unlimited: true, PlanName: "unlimited"})
	}
	status, err := h.billingSvc.Status(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(status)
}

// GetBillingPlan — GET /admin/billing/plan.
func (h *Handler) GetBillingPlan(c *fiber.Ctx) error {
	if h.billingSvc == nil {
		return badRequest(c, "billing disabled")
	}
	plan, err := h.billingSvc.Plan(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(plan)
}

// UpdateBillingPlan — PUT /admin/billing/plan.
func (h *Handler) UpdateBillingPlan(c *fiber.Ctx) error {
	if h.billingSvc == nil {
		return badRequest(c, "billing disabled")
	}
	var req domain.UpdateBillingPlanRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	plan, err := h.billingSvc.UpdatePlan(h.enrichContext(c), req)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(plan)
}

// ListModelPrices — GET /admin/billing/model-prices.
func (h *Handler) ListModelPrices(c *fiber.Ctx) error {
	if h.billingSvc == nil {
		return badRequest(c, "billing disabled")
	}
	prices, err := h.billingSvc.ListModelPrices(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if prices == nil {
		prices = []domain.ModelPrice{}
	}
	return c.JSON(fiber.Map{"prices": prices})
}

// UpsertModelPrice — PUT /admin/billing/model-prices.
func (h *Handler) UpsertModelPrice(c *fiber.Ctx) error {
	if h.billingSvc == nil {
		return badRequest(c, "billing disabled")
	}
	var req domain.ModelPrice
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Model == "" {
		return badRequest(c, "model is required")
	}
	price, err := h.billingSvc.UpsertModelPrice(h.enrichContext(c), req)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(price)
}

// DeleteModelPrice — DELETE /admin/billing/model-prices/:model.
func (h *Handler) DeleteModelPrice(c *fiber.Ctx) error {
	if h.billingSvc == nil {
		return badRequest(c, "billing disabled")
	}
	model := c.Params("model")
	if model == "" {
		return badRequest(c, "model is required")
	}
	if err := h.billingSvc.DeleteModelPrice(h.enrichContext(c), model); err != nil {
		return internalError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
