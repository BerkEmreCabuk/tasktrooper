package http

import (
	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerSettingsRoutes(app *fiber.App) {
	app.Get("/v1/settings", h.GetSettings)
	app.Put("/v1/settings", h.UpdateSettings)
	app.Get("/v1/settings/github", h.GitHubStatus)
	app.Put("/v1/settings/github", h.SetGitHubToken)
	app.Delete("/v1/settings/github", h.DeleteGitHubToken)
	app.Get("/v1/settings/github/owners", h.GitHubOwners)
	app.Get("/v1/settings/github/repos", h.GitHubOwnerRepos)
}

func (h *Handler) GetSettings(c *fiber.Ctx) error {
	if h.settingsSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "settings not enabled", Type: "service_unavailable"},
		})
	}
	out, err := h.settingsSvc.Get(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) UpdateSettings(c *fiber.Ctx) error {
	if h.settingsSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "settings not enabled", Type: "service_unavailable"},
		})
	}
	var req domain.UpdateSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.WorkspaceRoot == "" && req.DefaultLanguage == "" && req.PipelineContainerRuntime == "" && req.BoilerplateCatalogRepo == "" {
		return badRequest(c, "at least one settings field is required")
	}
	out, err := h.settingsSvc.Update(h.enrichContext(c), req)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}
