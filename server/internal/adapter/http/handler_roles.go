package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerWorkflowRoutes(app *fiber.App) {
	if h.workflowSvc == nil {
		return
	}
	app.Get("/v1/roles", h.ListRoles)
	app.Post("/v1/roles", h.CreateRole)
	app.Put("/v1/roles/:id", h.UpdateRole)
	app.Delete("/v1/roles/:id", h.DeleteRole)
	app.Put("/v1/roles/:id/assignments", h.SetRoleAssignments)
	app.Get("/v1/agents/:agentId/roles", h.GetAgentRoles)
	app.Put("/v1/agents/:agentId/roles", h.SetAgentRoles)
	app.Get("/v1/role-purposes", h.ListRolePurposes)
	app.Put("/v1/role-purposes/:purpose", h.SetRolePurpose)

	app.Get("/v1/task-types", h.ListTaskTypes)
	app.Post("/v1/task-types", h.CreateTaskType)
	app.Put("/v1/task-types/:key", h.UpdateTaskType)
	app.Delete("/v1/task-types/:key", h.DeleteTaskType)
	app.Get("/v1/task-types/:key/workflow", h.GetTaskTypeWorkflow)
	app.Put("/v1/task-types/:key/workflow", h.PutTaskTypeWorkflow)
	app.Get("/v1/workflows", h.ListWorkflows)
	app.Get("/v1/workflow/behaviours", h.ListWorkflowBehaviours)
}

// ---- JSON shapes ----

type roleAssignmentJSON struct {
	AgentID   uuid.UUID `json:"agent_id"`
	AgentName string    `json:"agent_name,omitempty"`
	Areas     []string  `json:"areas"`
	Priority  int       `json:"priority"`
}

type roleJSON struct {
	ID            uuid.UUID            `json:"id"`
	Key           string               `json:"key"`
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	RequiredTools []string             `json:"required_tools"`
	Assignments   []roleAssignmentJSON `json:"assignments"`
	Purposes      []string             `json:"purposes"`
}

func toRoleJSON(r domain.AgentRole) roleJSON {
	out := roleJSON{
		ID: r.ID, Key: r.Key, Name: r.Name, Description: r.Description,
		RequiredTools: orEmptyStrings(r.RequiredTools),
		Assignments:   make([]roleAssignmentJSON, 0, len(r.Assignments)),
		Purposes:      make([]string, 0, len(r.Purposes)),
	}
	for _, a := range r.Assignments {
		out.Assignments = append(out.Assignments, roleAssignmentJSON{
			AgentID: a.AgentID, AgentName: a.AgentName, Areas: a.Areas, Priority: a.Priority,
		})
	}
	for _, p := range r.Purposes {
		out.Purposes = append(out.Purposes, string(p))
	}
	return out
}

func orEmptyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// ---- Roles ----

func (h *Handler) ListRoles(c *fiber.Ctx) error {
	roles, err := h.workflowSvc.ListRoles(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	out := make([]roleJSON, 0, len(roles))
	for _, r := range roles {
		out = append(out, toRoleJSON(r))
	}
	return c.JSON(fiber.Map{"roles": out})
}

func (h *Handler) CreateRole(c *fiber.Ctx) error {
	var req struct {
		Key           string   `json:"key"`
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		RequiredTools []string `json:"required_tools"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	role, err := h.workflowSvc.CreateRole(h.enrichContext(c), domain.AgentRole{
		Key: req.Key, Name: req.Name, Description: req.Description, RequiredTools: req.RequiredTools,
	})
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(toRoleJSON(role))
}

func (h *Handler) UpdateRole(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid role id")
	}
	var req struct {
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		RequiredTools []string `json:"required_tools"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	role, err := h.workflowSvc.UpdateRole(h.enrichContext(c), domain.AgentRole{
		ID: id, Name: req.Name, Description: req.Description, RequiredTools: req.RequiredTools,
	})
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(toRoleJSON(role))
}

func (h *Handler) DeleteRole(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid role id")
	}
	if err := h.workflowSvc.DeleteRole(h.enrichContext(c), id); err != nil {
		return internalError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) SetRoleAssignments(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid role id")
	}
	var req struct {
		Assignments []struct {
			AgentID  uuid.UUID `json:"agent_id"`
			Areas    []string  `json:"areas"`
			Priority int       `json:"priority"`
		} `json:"assignments"`
		ConfirmGrantTools bool `json:"confirm_grant_tools"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	assignments := make([]domain.RoleAssignment, 0, len(req.Assignments))
	for _, a := range req.Assignments {
		assignments = append(assignments, domain.RoleAssignment{AgentID: a.AgentID, Areas: a.Areas, Priority: a.Priority})
	}
	granted, err := h.workflowSvc.SetRoleAssignmentsChecked(h.enrichContext(c), id, assignments, req.ConfirmGrantTools)
	if err != nil {
		var missingErr *workflow.MissingToolsError
		if errors.As(err, &missingErr) {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
				"saved": false, "missing_tools": missingErr.Missing,
			})
		}
		return badRequest(c, err.Error())
	}
	role, err := h.workflowSvc.GetRole(h.enrichContext(c), id)
	if err != nil {
		return internalError(c, err)
	}
	resp := fiber.Map{"saved": true, "role": toRoleJSON(role)}
	if len(granted) > 0 {
		resp["granted_tools"] = granted
	}
	return c.JSON(resp)
}

// ---- Agent-centric role membership ----

type agentRoleJSON struct {
	RoleID uuid.UUID `json:"role_id"`
	Key    string    `json:"key"`
	Name   string    `json:"name"`
	Areas  []string  `json:"areas"`
}

func (h *Handler) GetAgentRoles(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	roles, err := h.workflowSvc.ListAssignmentsByAgent(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	out := make([]agentRoleJSON, 0, len(roles))
	for _, r := range roles {
		var areas []string
		if len(r.Assignments) > 0 {
			areas = r.Assignments[0].Areas
		}
		out = append(out, agentRoleJSON{RoleID: r.ID, Key: r.Key, Name: r.Name, Areas: areas})
	}
	return c.JSON(fiber.Map{"roles": out})
}

func (h *Handler) SetAgentRoles(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req struct {
		Roles []struct {
			RoleID uuid.UUID `json:"role_id"`
			Areas  []string  `json:"areas"`
		} `json:"roles"`
		ConfirmGrantTools bool `json:"confirm_grant_tools"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	memberships := make([]domain.AgentRoleMembership, 0, len(req.Roles))
	for _, r := range req.Roles {
		memberships = append(memberships, domain.AgentRoleMembership{RoleID: r.RoleID, Areas: r.Areas})
	}
	granted, err := h.workflowSvc.SetAgentRolesChecked(h.enrichContext(c), agentID, memberships, req.ConfirmGrantTools)
	if err != nil {
		var missingErr *workflow.MissingToolsError
		if errors.As(err, &missingErr) {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
				"saved": false, "missing_tools": missingErr.Missing,
			})
		}
		return badRequest(c, err.Error())
	}
	roles, err := h.workflowSvc.ListAssignmentsByAgent(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	out := make([]agentRoleJSON, 0, len(roles))
	for _, r := range roles {
		var areas []string
		if len(r.Assignments) > 0 {
			areas = r.Assignments[0].Areas
		}
		out = append(out, agentRoleJSON{RoleID: r.ID, Key: r.Key, Name: r.Name, Areas: areas})
	}
	resp := fiber.Map{"saved": true, "roles": out}
	if len(granted) > 0 {
		resp["granted_tools"] = granted
	}
	return c.JSON(resp)
}

// ---- Role purposes ----

type rolePurposeJSON struct {
	Purpose string     `json:"purpose"`
	RoleID  *uuid.UUID `json:"role_id"`
}

func (h *Handler) ListRolePurposes(c *fiber.Ctx) error {
	purposes, err := h.workflowSvc.ListPurposes(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	out := make([]rolePurposeJSON, 0, len(purposes))
	for _, p := range purposes {
		out = append(out, rolePurposeJSON{Purpose: string(p.Purpose), RoleID: p.RoleID})
	}
	return c.JSON(fiber.Map{"purposes": out})
}

func (h *Handler) SetRolePurpose(c *fiber.Ctx) error {
	purpose := domain.RolePurposeKey(c.Params("purpose"))
	var req struct {
		RoleID *uuid.UUID `json:"role_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workflowSvc.SetPurpose(h.enrichContext(c), purpose, req.RoleID); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
