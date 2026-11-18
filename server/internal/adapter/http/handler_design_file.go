package http

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/designfile"
)

// registerDesignFileRoutes serves the FigCraft files design tasks draw in:
// their pages, a PNG of each page, and opening one in the FigCraft app on
// this computer. scope is task, project or repository; id is that one's id.
func (h *Handler) registerDesignFileRoutes(app fiber.Router) {
	if h.designFileSvc == nil {
		return
	}
	app.Get("/v1/design-files/:scope/:id", h.GetDesignFile)
	app.Get("/v1/design-files/:scope/:id/pages/:pageId/preview", h.GetDesignFilePreview)
	app.Post("/v1/design-files/:scope/:id/open", h.OpenDesignFile)
	app.Post("/v1/design-files/:scope/:id/reveal", h.RevealDesignFile)
}

func (h *Handler) designFilePath(c *fiber.Ctx) (string, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return "", badRequest(c, "invalid id")
	}
	switch c.Params("scope") {
	case "task":
		return h.designFileSvc.TaskFile(id), nil
	case "project":
		return h.designFileSvc.ProjectFile(id), nil
	case "repository":
		return h.designFileSvc.RepositoryFile(id), nil
	default:
		return "", badRequest(c, "scope must be task, project or repository")
	}
}

func designFileError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, designfile.ErrNoFile), errors.Is(err, designfile.ErrUnknownPage):
		return notFound(c, err.Error())
	case errors.Is(err, designfile.ErrNotInstalled):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "figcraft_not_installed"},
		})
	default:
		return internalError(c, err)
	}
}

// GetDesignFile — GET /v1/design-files/:scope/:id
func (h *Handler) GetDesignFile(c *fiber.Ctx) error {
	file, err := h.designFilePath(c)
	if file == "" {
		return err
	}
	return c.JSON(h.designFileSvc.Manifest(h.enrichContext(c), file))
}

// GetDesignFilePreview — GET /v1/design-files/:scope/:id/pages/:pageId/preview?scale=1
func (h *Handler) GetDesignFilePreview(c *fiber.Ctx) error {
	file, err := h.designFilePath(c)
	if file == "" {
		return err
	}
	pageID, err := strconv.ParseInt(c.Params("pageId"), 10, 64)
	if err != nil {
		return badRequest(c, "invalid page id")
	}
	scale, _ := strconv.ParseFloat(c.Query("scale", "1"), 64)
	png, err := h.designFileSvc.Preview(h.enrichContext(c), file, pageID, scale)
	if err != nil {
		return designFileError(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "private, max-age=60")
	c.Type("png")
	return c.SendFile(png, false)
}

// OpenDesignFile — POST /v1/design-files/:scope/:id/open
func (h *Handler) OpenDesignFile(c *fiber.Ctx) error {
	file, err := h.designFilePath(c)
	if file == "" {
		return err
	}
	if err := h.designFileSvc.Open(h.enrichContext(c), file); err != nil {
		return designFileError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// RevealDesignFile — POST /v1/design-files/:scope/:id/reveal
func (h *Handler) RevealDesignFile(c *fiber.Ctx) error {
	file, err := h.designFilePath(c)
	if file == "" {
		return err
	}
	if err := h.designFileSvc.Reveal(h.enrichContext(c), file); err != nil {
		return designFileError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
