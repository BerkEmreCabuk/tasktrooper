package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerDeployOpsRoutes exposes the operations console: the cross-repository
// deploy matrix and the two actions that write to production. Same auth and
// middleware chain as every other route.
func (h *Handler) registerDeployOpsRoutes(app fiber.Router) {
	if h.deployOpsSvc == nil {
		return
	}
	app.Get("/v1/operations/deployments", h.GetDeployMatrix)
	app.Get("/v1/operations/audit", h.ListOpsAudit)
	app.Get("/v1/repositories/:id/deploy/:env/runs", h.ListDeployRuns)
	app.Post("/v1/repositories/:id/deploy/:env/dispatch", h.DispatchDeploy)
	app.Post("/v1/repositories/:id/deploy/:env/rollback", h.RollbackDeploy)
}

// deployOpsError maps the service's sentinels onto status codes the UI can
// branch on: the caller's typo is 400, a configuration gap the caller can go
// fix is 409, an absent repository is 404, and anything else is ours.
func deployOpsError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, deployops.ErrConfirmMismatch):
		return badRequest(c, err.Error())
	case errors.Is(err, deployops.ErrNoWorkflowMapping), errors.Is(err, deployops.ErrNoRollbackTarget):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, port.ErrNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "repository not found"})
	case errors.Is(err, deployops.ErrProvider):
		// GitHub failed, not us and not the caller. 502 lets the UI say so.
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	default:
		return internalError(c, err)
	}
}

// consoleActor is who a console dispatch is recorded as. Requests carry no user
// identity, so it is always the system.
const consoleActor = "system"

// parseDeployRouteParams parses the repository UUID and validates the :env
// path segment against domain.DeployEnvs() — the guard every deploy-console
// route needs before it ever reaches the service.
func parseDeployRouteParams(c *fiber.Ctx) (uuid.UUID, string, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, "", badRequest(c, "invalid repository id")
	}
	env := c.Params("env")
	if !domain.ValidDeployEnv(env) {
		return uuid.Nil, "", badRequest(c, "invalid environment")
	}
	return id, env, nil
}

// GetDeployMatrix — GET /v1/operations/deployments
func (h *Handler) GetDeployMatrix(c *fiber.Ctx) error {
	view, err := h.deployOpsSvc.Matrix(h.enrichContext(c))
	if err != nil {
		return deployOpsError(c, err)
	}
	return c.JSON(view)
}

// ListOpsAudit — GET /v1/operations/audit?repository_id=&limit=
func (h *Handler) ListOpsAudit(c *fiber.Ctx) error {
	var repositoryID *uuid.UUID
	if raw := c.Query("repository_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid repository id")
		}
		repositoryID = &id
	}
	entries, err := h.deployOpsSvc.Audit(h.enrichContext(c), repositoryID, c.QueryInt("limit", 0))
	if err != nil {
		return deployOpsError(c, err)
	}
	return c.JSON(entries)
}

// ListDeployRuns — GET /v1/repositories/:id/deploy/:env/runs?limit=
func (h *Handler) ListDeployRuns(c *fiber.Ctx) error {
	id, env, err := parseDeployRouteParams(c)
	if err != nil {
		return err
	}
	runs, err := h.deployOpsSvc.Runs(h.enrichContext(c), id, env, c.QueryInt("limit", 0))
	if err != nil {
		return deployOpsError(c, err)
	}
	return c.JSON(runs)
}

// dispatchDeployRequest is the DispatchDeploy body. An empty Ref defaults to
// "main" (deployops.defaultDispatchRef); Confirm is required only for
// production-class dispatches — enforced server-side by the service, not
// duplicated here.
type dispatchDeployRequest struct {
	Ref     string `json:"ref"`
	Confirm string `json:"confirm"`
}

// DispatchDeploy — POST /v1/repositories/:id/deploy/:env/dispatch
func (h *Handler) DispatchDeploy(c *fiber.Ctx) error {
	id, env, err := parseDeployRouteParams(c)
	if err != nil {
		return err
	}
	var req dispatchDeployRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	dispatch, err := h.deployOpsSvc.Dispatch(h.enrichContext(c), deployops.DispatchInput{
		RepositoryID: id,
		Env:          env,
		Ref:          req.Ref,
		Confirm:      req.Confirm,
		Actor:        consoleActor,
	})
	if err != nil {
		return deployOpsError(c, err)
	}
	return c.JSON(dispatch)
}

// rollbackDeployRequest is the RollbackDeploy body. Confirm is required by
// every rollback regardless of environment — the service treats rollback as
// always production-class.
type rollbackDeployRequest struct {
	Confirm string `json:"confirm"`
}

// RollbackDeploy — POST /v1/repositories/:id/deploy/:env/rollback
func (h *Handler) RollbackDeploy(c *fiber.Ctx) error {
	id, env, err := parseDeployRouteParams(c)
	if err != nil {
		return err
	}
	var req rollbackDeployRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	dispatch, err := h.deployOpsSvc.Rollback(h.enrichContext(c), deployops.RollbackInput{
		RepositoryID: id,
		Env:          env,
		Confirm:      req.Confirm,
		Actor:        consoleActor,
	})
	if err != nil {
		return deployOpsError(c, err)
	}
	return c.JSON(dispatch)
}
