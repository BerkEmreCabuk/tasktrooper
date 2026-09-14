package http

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerAttachmentRoutes(app fiber.Router) {
	if h.attachmentSvc == nil {
		return
	}
	app.Post("/v1/attachments", h.UploadAttachment)
	app.Get("/v1/attachments/:id", h.GetAttachment)
	app.Delete("/v1/attachments/:id", h.DeleteAttachment)
	app.Get("/v1/repositories/:id/tasks/:taskId/attachments", h.ListTaskAttachments)
	app.Post("/v1/repositories/:id/tasks/:taskId/attachments", h.LinkTaskAttachment)
	app.Delete("/v1/repositories/:id/tasks/:taskId/attachments/:attachmentId", h.UnlinkTaskAttachment)
}

// attachmentError maps the domain's validation errors onto their HTTP shapes:
// 413 for the size cap, 415 for a type off the allowlist.
func attachmentError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrAttachmentTooLarge):
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "attachment_too_large"},
		})
	case errors.Is(err, domain.ErrAttachmentTypeNotAllowed):
		return c.Status(fiber.StatusUnsupportedMediaType).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "attachment_type_not_allowed"},
		})
	default:
		return badRequest(c, err.Error())
	}
}

// UploadAttachment — POST /v1/attachments (multipart: `file`, optional
// `repository_id`). Returns AttachmentMeta; the bytes live in Postgres.
func (h *Handler) UploadAttachment(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return badRequest(c, "file field is required")
	}
	// Cheap early refusal before buffering the body into memory.
	if file.Size > domain.MaxAttachmentBytes {
		return attachmentError(c, domain.ErrAttachmentTooLarge)
	}
	var repositoryID *uuid.UUID
	if raw := strings.TrimSpace(c.FormValue("repository_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid repository_id")
		}
		repositoryID = &id
	}
	f, err := file.Open()
	if err != nil {
		return internalError(c, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return internalError(c, err)
	}
	meta, err := h.attachmentSvc.Upload(
		h.enrichContext(c),
		file.Filename,
		file.Header.Get("Content-Type"),
		data,
		repositoryID,
		"user",
		"",
	)
	if err != nil {
		return attachmentError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(meta)
}

// GetAttachment — GET /v1/attachments/:id. Serves the raw bytes. The id is a
// content-addressed immutable row (uploads never mutate), so long-lived
// private caching is safe.
func (h *Handler) GetAttachment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid attachment id")
	}
	att, err := h.attachmentSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	c.Set("Content-Type", att.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", sanitizeFilename(att.Filename)))
	c.Set("Cache-Control", "private, max-age=31536000, immutable")
	return c.Send(att.Data)
}

// sanitizeFilename keeps the Content-Disposition header a single, quotable
// token: no quotes, no CR/LF header injection.
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer("\"", "", "\r", "", "\n", "", "\\", "")
	cleaned := strings.TrimSpace(replacer.Replace(name))
	if cleaned == "" {
		return "attachment"
	}
	return cleaned
}

// DeleteAttachment — DELETE /v1/attachments/:id. Links cascade away with it.
func (h *Handler) DeleteAttachment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid attachment id")
	}
	if err := h.attachmentSvc.Delete(h.enrichContext(c), id); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListTaskAttachments — GET /v1/repositories/:id/tasks/:taskId/attachments
func (h *Handler) ListTaskAttachments(c *fiber.Ctx) error {
	_, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	metas, err := h.attachmentSvc.ListByTask(h.enrichContext(c), taskID)
	if err != nil {
		return internalError(c, err)
	}
	if metas == nil {
		metas = []domain.AttachmentMeta{}
	}
	return c.JSON(fiber.Map{"attachments": metas, "count": len(metas)})
}

// LinkTaskAttachment — POST /v1/repositories/:id/tasks/:taskId/attachments
// with {"attachment_id": "..."} links an already-uploaded attachment.
func (h *Handler) LinkTaskAttachment(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req struct {
		AttachmentID string `json:"attachment_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	attachmentID, err := uuid.Parse(strings.TrimSpace(req.AttachmentID))
	if err != nil {
		return badRequest(c, "invalid attachment_id")
	}
	meta, err := h.attachmentSvc.LinkTask(h.enrichContext(c), repositoryID, taskID, attachmentID)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(meta)
}

// UnlinkTaskAttachment — DELETE /v1/repositories/:id/tasks/:taskId/attachments/:attachmentId
// removes the link only; the attachment row (and any chat message links)
// survive.
func (h *Handler) UnlinkTaskAttachment(c *fiber.Ctx) error {
	_, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	attachmentID, err := uuid.Parse(c.Params("attachmentId"))
	if err != nil {
		return badRequest(c, "invalid attachment id")
	}
	if err := h.attachmentSvc.UnlinkTask(h.enrichContext(c), taskID, attachmentID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}
