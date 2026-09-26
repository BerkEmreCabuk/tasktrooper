package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerAnnotationRoutes(app fiber.Router) {
	app.Get("/v1/repositories/:id/tasks/:taskId/annotations", h.ListTaskAnnotations)
	app.Post("/v1/repositories/:id/tasks/:taskId/documents/:docId/annotations", h.CreateTaskAnnotation)
	app.Post("/v1/repositories/:id/tasks/:taskId/annotations/submit", h.SubmitTaskAnnotations)
	app.Patch("/v1/repositories/:id/tasks/:taskId/annotations/:annId", h.UpdateTaskAnnotation)
	app.Delete("/v1/repositories/:id/tasks/:taskId/annotations/:annId", h.DeleteTaskAnnotation)
}

// annotationError maps the review flow's failures onto the statuses the task
// drawer tells apart: a missing task/document/comment (404), a request the
// current state refuses (409), a malformed one (400). A refusal from the
// column move inside submit is a board rule, so it keeps badRequestErr's
// typed codes.
func annotationError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrBoardTaskNotFound),
		errors.Is(err, domain.ErrTaskDocumentNotFound),
		errors.Is(err, domain.ErrAnnotationNotFound):
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	case errors.Is(err, domain.ErrAnnotationConflict):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "conflict"},
		})
	case errors.Is(err, domain.ErrAnnotationInvalid), errors.Is(err, domain.ErrNoOpenAnnotations):
		return badRequest(c, err.Error())
	}
	return badRequestErr(c, err)
}

func (h *Handler) ListTaskAnnotations(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var documentID *uuid.UUID
	if raw := c.Query("document_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid document_id")
		}
		documentID = &id
	}
	items, err := h.repositorySvc.ListAnnotations(h.enrichContext(c), repositoryID, taskID, documentID)
	if err != nil {
		return annotationError(c, err)
	}
	if items == nil {
		items = []domain.TaskDocumentAnnotation{}
	}
	return c.JSON(fiber.Map{"annotations": items})
}

func (h *Handler) CreateTaskAnnotation(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	docID, err := uuid.Parse(c.Params("docId"))
	if err != nil {
		return badRequest(c, "invalid document id")
	}
	var req domain.CreateDocumentAnnotationRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ann, err := h.repositorySvc.AddAnnotation(h.enrichContext(c), repositoryID, taskID, docID, req)
	if err != nil {
		return annotationError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(ann)
}

func (h *Handler) UpdateTaskAnnotation(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	annID, err := uuid.Parse(c.Params("annId"))
	if err != nil {
		return badRequest(c, "invalid annotation id")
	}
	var req domain.UpdateDocumentAnnotationRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ann, err := h.repositorySvc.UpdateAnnotation(h.enrichContext(c), repositoryID, taskID, annID, req)
	if err != nil {
		return annotationError(c, err)
	}
	return c.JSON(ann)
}

func (h *Handler) DeleteTaskAnnotation(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	annID, err := uuid.Parse(c.Params("annId"))
	if err != nil {
		return badRequest(c, "invalid annotation id")
	}
	if err := h.repositorySvc.DeleteAnnotation(h.enrichContext(c), repositoryID, taskID, annID); err != nil {
		return annotationError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SubmitTaskAnnotations — POST /v1/repositories/:id/tasks/:taskId/annotations/submit
//
// The human's "request changes" on an analysis: every open comment goes to
// the architect at once and the task moves to need_revision. The body is
// optional; an empty POST is a submit with no covering note.
func (h *Handler) SubmitTaskAnnotations(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req domain.SubmitAnnotationsRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	n, task, err := h.repositorySvc.SubmitAnnotations(h.enrichContext(c), repositoryID, taskID, req)
	if err != nil {
		return annotationError(c, err)
	}
	return c.JSON(fiber.Map{"submitted": n, "task": task})
}
