package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func localPreviewDisabled(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "local preview is not available on this server", Type: "unavailable"},
	})
}

// StartLocalPreview — POST /v1/repositories/:id/tasks/:taskId/local-preview/start
//
// Checks out the task's branch (the same checkout the board runner, the
// pipeline and the task's own chat already use) and runs whatever dev/start
// command the repository is detected to have, so a human_uat reviewer can
// poke at the actual branch before approving. See localpreview.Service.
func (h *Handler) StartLocalPreview(c *fiber.Ctx) error {
	if h.localPreviewSvc == nil {
		return localPreviewDisabled(c)
	}
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	preview, err := h.localPreviewSvc.Start(h.enrichContext(c), repositoryID, taskID, "")
	if err != nil {
		return badRequestErr(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(preview)
}

// StopLocalPreview — POST /v1/repositories/:id/local-preview/stop
//
// Not an error when nothing is running — the button that calls this cannot
// always tell, and a 204 either way is simpler than a client having to
// distinguish "stopped" from "there was nothing to stop".
func (h *Handler) StopLocalPreview(c *fiber.Ctx) error {
	if h.localPreviewSvc == nil {
		return localPreviewDisabled(c)
	}
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, err.Error())
	}
	h.localPreviewSvc.Stop(repositoryID)
	return c.SendStatus(fiber.StatusNoContent)
}

// GetLocalPreview — GET /v1/repositories/:id/local-preview
//
// The repository's current preview, whichever task started it. One per
// repository (see localpreview.Service), so the client compares the
// returned task_id against the task it is showing to know whether this
// preview is about that task or a different one.
func (h *Handler) GetLocalPreview(c *fiber.Ctx) error {
	if h.localPreviewSvc == nil {
		return localPreviewDisabled(c)
	}
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, err.Error())
	}
	preview, ok := h.localPreviewSvc.Status(repositoryID)
	if !ok {
		return c.JSON(fiber.Map{"active": false})
	}
	return c.JSON(fiber.Map{"active": true, "preview": preview})
}
