package http

import (
	"context"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
)

// GitHub bağlantısı doğrudan yapıştırılan bir personal access token ile
// kurulur (SetGitHubToken); kullanıcı token'ı GitHub'da oluşturup buraya
// yapıştırır. Token doğrulanır (githubapi.User) ve şifreli olarak
// app_settings'te saklanır; gh CLI kullanılmaz.

type githubStatusResponse struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// GitHubStatus — GET /v1/settings/github
func (h *Handler) GitHubStatus(c *fiber.Ctx) error {
	if h.githubTokens == nil {
		return c.JSON(githubStatusResponse{Connected: false, Detail: "token store not configured"})
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
	defer cancel()

	token, err := h.githubTokens.GitHubToken(ctx)
	if err != nil {
		return internalError(c, err)
	}
	if token == "" {
		return c.JSON(githubStatusResponse{Connected: false})
	}
	login, err := githubapi.User(ctx, token)
	if err != nil {
		return c.JSON(githubStatusResponse{Connected: false, Detail: "token is invalid or GitHub is unreachable: " + err.Error()})
	}
	return c.JSON(githubStatusResponse{Connected: true, Login: login})
}

// SetGitHubToken — PUT /v1/settings/github {"token":"..."}
func (h *Handler) SetGitHubToken(c *fiber.Ctx) error {
	if h.githubTokens == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "token store not configured", Type: "service_unavailable"},
		})
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return badRequest(c, "token is required")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
	defer cancel()

	login, err := githubapi.User(ctx, token)
	if err != nil {
		return badRequest(c, "token could not be verified: "+err.Error())
	}
	if err := h.githubTokens.SetGitHubToken(ctx, token); err != nil {
		return internalError(c, err)
	}
	return c.JSON(githubStatusResponse{Connected: true, Login: login})
}

// GitHubOwners — GET /v1/settings/github/owners
// Token sahibi + üyesi olduğu org'lar (repo import/create hedefleri).
func (h *Handler) GitHubOwners(c *fiber.Ctx) error {
	token, ok, err := h.requireGitHubToken(c)
	if err != nil || !ok {
		return err
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	owners, aerr := githubapi.ListOwners(ctx, token)
	if aerr != nil {
		return badRequest(c, "failed to list GitHub owners: "+aerr.Error())
	}
	return c.JSON(fiber.Map{"owners": owners})
}

// GitHubOwnerRepos — GET /v1/settings/github/repos?owner=<login>
func (h *Handler) GitHubOwnerRepos(c *fiber.Ctx) error {
	token, ok, err := h.requireGitHubToken(c)
	if err != nil || !ok {
		return err
	}
	owner := strings.TrimSpace(c.Query("owner"))
	if owner == "" {
		return badRequest(c, "owner is required")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
	defer cancel()
	repos, aerr := githubapi.ListOwnerRepos(ctx, token, owner)
	if aerr != nil {
		return badRequest(c, "failed to list GitHub repositories: "+aerr.Error())
	}
	return c.JSON(fiber.Map{"repos": repos})
}

// requireGitHubToken: token yoksa 400 döner (ok=false); hata zaten yazılmıştır.
func (h *Handler) requireGitHubToken(c *fiber.Ctx) (string, bool, error) {
	if h.githubTokens == nil {
		return "", false, c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "token store not configured", Type: "service_unavailable"},
		})
	}
	token, err := h.githubTokens.GitHubToken(c.UserContext())
	if err != nil {
		return "", false, internalError(c, err)
	}
	if token == "" {
		return "", false, badRequest(c, "GitHub is not connected — connect it from Settings")
	}
	return token, true, nil
}

// DeleteGitHubToken — DELETE /v1/settings/github
func (h *Handler) DeleteGitHubToken(c *fiber.Ctx) error {
	if h.githubTokens == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "token store not configured", Type: "service_unavailable"},
		})
	}
	if err := h.githubTokens.DeleteGitHubToken(c.UserContext()); err != nil {
		return internalError(c, err)
	}
	return c.JSON(githubStatusResponse{Connected: false})
}
