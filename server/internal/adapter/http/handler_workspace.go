package http

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (h *Handler) registerWorkspaceRoutes(app fiber.Router) {
	if h.workspaceSvc == nil {
		return
	}
	app.Get("/v1/board/config", h.GetBoardConfig)
	app.Get("/v1/board/settings", h.GetBoardSettings)
	app.Put("/v1/board/settings", h.UpdateBoardSettings)
	app.Get("/v1/board/columns", h.ListBoardColumns)
	app.Put("/v1/board/columns", h.UpdateBoardColumns)
	app.Get("/v1/board/members", h.GetBoardMembers)
	app.Put("/v1/board/members", h.SetBoardMembers)
	app.Get("/v1/board/subscriptions", h.GetBoardSubscriptions)
	app.Put("/v1/board/subscriptions", h.SetBoardSubscriptions)
	app.Get("/v1/board/transitions", h.GetBoardTransitions)
	app.Put("/v1/board/transitions", h.SetBoardTransitions)
	app.Get("/v1/activity", h.ListActivity)
	app.Get("/v1/tasks", h.ListAllBoardTasks)
	app.Get("/v1/tasks/released", h.ListReleasedArchive)
	app.Get("/v1/agents/:agentId/subscriptions", h.GetAgentSubscriptions)
	app.Put("/v1/agents/:agentId/subscriptions", h.SetAgentSubscriptions)
}

func (h *Handler) GetAgentSubscriptions(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	slugs, err := h.workspaceSvc.ListAgentSubscriptions(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"column_slugs": slugs})
}

func (h *Handler) SetAgentSubscriptions(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req struct {
		ColumnSlugs []string `json:"column_slugs"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.SetAgentSubscriptions(h.enrichContext(c), agentID, req.ColumnSlugs); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListAllBoardTasks serves the board. Released tasks older than
// domain.ReleasedBoardWindow are left out — they are in the released archive
// below, which is where the board links to for anything older.
func (h *Handler) ListAllBoardTasks(c *fiber.Ctx) error {
	if h.repositorySvc == nil {
		return internalError(c, fmt.Errorf("repositories unavailable"))
	}
	tasks, err := h.repositorySvc.ListBoardTasks(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"tasks": tasks, "count": len(tasks)})
}

// ListReleasedArchive is every released task, newest first, with an optional
// `q` search over key, title and description.
func (h *Handler) ListReleasedArchive(c *fiber.Ctx) error {
	if h.repositorySvc == nil {
		return internalError(c, fmt.Errorf("repositories unavailable"))
	}
	tasks, err := h.repositorySvc.ListReleasedArchive(h.enrichContext(c), c.Query("q"), c.QueryInt("limit", 100))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"tasks": tasks, "count": len(tasks)})
}

func (h *Handler) GetBoardConfig(c *fiber.Ctx) error {
	config, err := h.workspaceSvc.GetConfig(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(config)
}

func (h *Handler) GetBoardSettings(c *fiber.Ctx) error {
	settings, err := h.workspaceSvc.GetSettings(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(settings)
}

func (h *Handler) UpdateBoardSettings(c *fiber.Ctx) error {
	var req domain.UpdateBoardSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	settings, err := h.workspaceSvc.UpdateSettings(h.enrichContext(c), req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(settings)
}

func (h *Handler) ListBoardColumns(c *fiber.Ctx) error {
	columns, err := h.workspaceSvc.ListColumns(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if columns == nil {
		columns = []domain.BoardColumn{}
	}
	return c.JSON(fiber.Map{"columns": columns, "count": len(columns)})
}

func (h *Handler) UpdateBoardColumns(c *fiber.Ctx) error {
	var req domain.UpdateBoardColumnsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.UpdateColumns(h.enrichContext(c), req); err != nil {
		return badRequest(c, err.Error())
	}
	columns, err := h.workspaceSvc.ListColumns(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"columns": columns, "count": len(columns)})
}

func (h *Handler) GetBoardMembers(c *fiber.Ctx) error {
	members, err := h.workspaceSvc.ListMembers(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if members == nil {
		members = []domain.BoardMember{}
	}
	return c.JSON(fiber.Map{"members": members, "count": len(members)})
}

func (h *Handler) SetBoardMembers(c *fiber.Ctx) error {
	var req domain.SetBoardMembersRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.SetMembers(h.enrichContext(c), req); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetBoardSubscriptions(c *fiber.Ctx) error {
	subs, err := h.workspaceSvc.ListSubscriptions(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if subs == nil {
		subs = []domain.BoardSubscription{}
	}
	return c.JSON(fiber.Map{"subscriptions": subs, "count": len(subs)})
}

func (h *Handler) SetBoardSubscriptions(c *fiber.Ctx) error {
	var req domain.SetBoardSubscriptionsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.SetSubscriptions(h.enrichContext(c), req); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetBoardTransitions(c *fiber.Ctx) error {
	transitions, err := h.workspaceSvc.ListTransitions(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if transitions == nil {
		transitions = []domain.BoardTransition{}
	}
	return c.JSON(fiber.Map{"transitions": transitions})
}

func (h *Handler) SetBoardTransitions(c *fiber.Ctx) error {
	var req domain.SetBoardTransitionsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.SetTransitions(h.enrichContext(c), req); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListActivity(c *fiber.Ctx) error {
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if n, parseErr := strconv.Atoi(raw); parseErr == nil && n > 0 {
			limit = n
		}
	}
	items, err := h.workspaceSvc.ListActivity(h.enrichContext(c), h.boardEvents, h.taskRuns, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"items": items, "count": len(items)})
}

func (h *Handler) ListTaskComments(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	comments, err := h.repositorySvc.ListComments(h.enrichContext(c), repositoryID, taskID)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"comments": comments, "count": len(comments)})
}

func (h *Handler) CreateTaskComment(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req domain.CreateTaskCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Content == "" {
		return badRequest(c, "content is required")
	}
	comment, err := h.repositorySvc.AddComment(h.enrichContext(c), repositoryID, taskID, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(comment)
}

// ListTaskEvents serves a task's history: which column it moved between, who
// moved it, when it was assigned and commented on.
func (h *Handler) ListTaskEvents(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		if n, parseErr := strconv.Atoi(raw); parseErr == nil && n > 0 {
			limit = n
		}
	}
	events, err := h.repositorySvc.ListTaskEvents(h.enrichContext(c), repositoryID, taskID, h.boardEvents, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"events": events, "count": len(events)})
}

func (h *Handler) ListTaskAgentRuns(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if n, parseErr := strconv.Atoi(raw); parseErr == nil && n > 0 {
			limit = n
		}
	}
	var store port.TaskAgentRunStore
	if h.taskRuns != nil {
		store = h.taskRuns
	}
	runs, err := h.repositorySvc.ListTaskRuns(h.enrichContext(c), repositoryID, taskID, store, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"runs": runs, "count": len(runs)})
}
