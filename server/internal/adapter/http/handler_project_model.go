package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerProjectModelRoutes mounts the project model: repositories' scanned
// component/check/link structure, the workspace-wide projects overview, and
// the human's edits on top. Registered before registerInitiativeRoutes:
// fiber matches routes in registration order, so /v1/projects/overview must
// be mounted before /v1/projects/:projectId swallows "overview" as an id.
func (h *Handler) registerProjectModelRoutes(app fiber.Router) {
	if h.projectModelSvc == nil {
		return
	}
	app.Get("/v1/projects/overview", h.ProjectsOverview)
	app.Get("/v1/projects/:projectId/overview", h.ProjectOverview)

	app.Get("/v1/repositories/:id/model", h.RepositoryModel)
	app.Get("/v1/repositories/:id/brief", h.RepositoryBrief)

	app.Post("/v1/repositories/:id/scans", h.StartRepositoryScan)
	app.Get("/v1/repositories/:id/scans/latest", h.LatestRepositoryScan)
	app.Get("/v1/scans/:scanId", h.GetProjectScan)

	app.Post("/v1/repositories/:id/components", h.AddProjectComponent)
	app.Patch("/v1/components/:componentId", h.UpdateProjectComponent)
	app.Post("/v1/components/:componentId/checks", h.AddComponentCheck)
	app.Patch("/v1/checks/:checkId", h.UpdateComponentCheck)
	app.Delete("/v1/checks/:checkId", h.DeleteComponentCheck)
	app.Post("/v1/links", h.AddComponentLink)
	app.Patch("/v1/links/:linkId", h.UpdateComponentLink)
	app.Delete("/v1/links/:linkId", h.DeleteComponentLink)
	app.Put("/v1/repositories/:id/notes", h.SaveProjectNote)
	app.Patch("/v1/notes/:noteId", h.UpdateProjectNote)
	app.Delete("/v1/notes/:noteId", h.DeleteProjectNote)
}

// pmErr writes the flat `{"error": "message"}` shape the project-model
// contract specifies, distinct from the rest of this package's nested
// errorResponse — this endpoint family is new and the UI it serves reads the
// flat shape.
func pmErr(c *fiber.Ctx, status int, err error) error {
	return c.Status(status).JSON(fiber.Map{"error": err.Error()})
}

func pmErrMsg(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

// projectModelErr maps a service error to its contract status: ErrInvalidInput
// and ErrNoteLocked are the caller's fault (400), ErrConflict is a concurrent
// edit (409), port.ErrNotFound is an unknown id (404), anything else is ours.
func projectModelErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, projectmodel.ErrInvalidInput), errors.Is(err, projectmodel.ErrNoteLocked):
		return pmErr(c, fiber.StatusBadRequest, err)
	case errors.Is(err, projectmodel.ErrConflict):
		return pmErr(c, fiber.StatusConflict, err)
	case errors.Is(err, port.ErrNotFound):
		return pmErr(c, fiber.StatusNotFound, err)
	default:
		return pmErr(c, fiber.StatusInternalServerError, err)
	}
}

func parseUUIDParam(c *fiber.Ctx, name string) (uuid.UUID, error) {
	return uuid.Parse(c.Params(name))
}

// ProjectsOverview — GET /v1/projects/overview
func (h *Handler) ProjectsOverview(c *fiber.Ctx) error {
	overview, err := h.projectModelSvc.ProjectsOverview(h.enrichContext(c))
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(overview)
}

// ProjectOverview — GET /v1/projects/:projectId/overview
func (h *Handler) ProjectOverview(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "projectId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid project id")
	}
	detail, err := h.projectModelSvc.ProjectOverview(h.enrichContext(c), id)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(detail)
}

// RepositoryModel — GET /v1/repositories/:id/model
func (h *Handler) RepositoryModel(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	model, err := h.projectModelSvc.RepositoryModel(h.enrichContext(c), id)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(model)
}

// RepositoryBrief — GET /v1/repositories/:id/brief?component_id=&area=
func (h *Handler) RepositoryBrief(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	scope := projectmodel.BriefScope{Area: c.Query("area")}
	if raw := c.Query("component_id"); raw != "" {
		componentID, err := uuid.Parse(raw)
		if err != nil {
			return pmErrMsg(c, fiber.StatusBadRequest, "invalid component_id")
		}
		scope.ComponentID = &componentID
	}
	brief, err := h.projectModelSvc.Brief(h.enrichContext(c), id, scope)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(fiber.Map{"brief": brief})
}

// StartRepositoryScan — POST /v1/repositories/:id/scans
// 202 {scan} once a new scan has been kicked off; 409 {scan, error} when one
// is already running (scan is that in-flight scan, not a new one).
func (h *Handler) StartRepositoryScan(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	scan, started, err := h.projectModelSvc.StartScan(h.enrichContext(c), id, domain.ScanTriggerManual)
	if err != nil {
		return projectModelErr(c, err)
	}
	if !started {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"scan": scan, "error": "a scan is already running"})
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"scan": scan})
}

// LatestRepositoryScan — GET /v1/repositories/:id/scans/latest
func (h *Handler) LatestRepositoryScan(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	scan, err := h.projectModelSvc.LatestScan(h.enrichContext(c), id)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(fiber.Map{"scan": scan})
}

// GetProjectScan — GET /v1/scans/:scanId
func (h *Handler) GetProjectScan(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "scanId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid scan id")
	}
	scan, err := h.projectModelSvc.GetScan(h.enrichContext(c), id)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(fiber.Map{"scan": scan})
}

// AddProjectComponent — POST /v1/repositories/:id/components
func (h *Handler) AddProjectComponent(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	var req domain.NewComponentRequest
	if err := c.BodyParser(&req); err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	comp, err := h.projectModelSvc.AddComponent(h.enrichContext(c), id, req)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(comp)
}

// UpdateProjectComponent — PATCH /v1/components/:componentId
func (h *Handler) UpdateProjectComponent(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	var patch domain.ComponentPatch
	if err := c.BodyParser(&patch); err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	comp, err := h.projectModelSvc.UpdateComponent(h.enrichContext(c), id, patch)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(comp)
}

// AddComponentCheck — POST /v1/components/:componentId/checks
func (h *Handler) AddComponentCheck(c *fiber.Ctx) error {
	componentID, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	var req domain.NewCheckRequest
	if err := c.BodyParser(&req); err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	check, err := h.projectModelSvc.AddCheck(h.enrichContext(c), componentID, req)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(check)
}

// UpdateComponentCheck — PATCH /v1/checks/:checkId
func (h *Handler) UpdateComponentCheck(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "checkId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid check id")
	}
	var patch domain.CheckPatch
	if err := c.BodyParser(&patch); err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	check, err := h.projectModelSvc.UpdateCheck(h.enrichContext(c), id, patch)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(check)
}

// DeleteComponentCheck — DELETE /v1/checks/:checkId
// Manual checks only; a CI-sourced check is refused (400) — dismiss it
// instead of deleting it.
func (h *Handler) DeleteComponentCheck(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "checkId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid check id")
	}
	if err := h.projectModelSvc.DeleteCheck(h.enrichContext(c), id); err != nil {
		return projectModelErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// AddComponentLink — POST /v1/links
func (h *Handler) AddComponentLink(c *fiber.Ctx) error {
	var req domain.NewLinkRequest
	if err := c.BodyParser(&req); err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	link, err := h.projectModelSvc.AddLink(h.enrichContext(c), req)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(link)
}

// UpdateComponentLink — PATCH /v1/links/:linkId
func (h *Handler) UpdateComponentLink(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "linkId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid link id")
	}
	var patch domain.LinkPatch
	if err := c.BodyParser(&patch); err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	link, err := h.projectModelSvc.UpdateLink(h.enrichContext(c), id, patch)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(link)
}

// DeleteComponentLink — DELETE /v1/links/:linkId
// User-created links only; a scan-detected link is refused (400) — dismiss it
// instead of deleting it.
func (h *Handler) DeleteComponentLink(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "linkId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid link id")
	}
	if err := h.projectModelSvc.DeleteLink(h.enrichContext(c), id); err != nil {
		return projectModelErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SaveProjectNote — PUT /v1/repositories/:id/notes
func (h *Handler) SaveProjectNote(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	var req domain.SaveNoteRequest
	if err := c.BodyParser(&req); err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	note, err := h.projectModelSvc.SaveUserNote(h.enrichContext(c), id, req)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(note)
}

// UpdateProjectNote — PATCH /v1/notes/:noteId
func (h *Handler) UpdateProjectNote(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "noteId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid note id")
	}
	var patch domain.NotePatch
	if err := c.BodyParser(&patch); err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	note, err := h.projectModelSvc.UpdateNote(h.enrichContext(c), id, patch)
	if err != nil {
		return projectModelErr(c, err)
	}
	return c.JSON(note)
}

// DeleteProjectNote — DELETE /v1/notes/:noteId
func (h *Handler) DeleteProjectNote(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "noteId")
	if err != nil {
		return pmErrMsg(c, fiber.StatusBadRequest, "invalid note id")
	}
	if err := h.projectModelSvc.DeleteNote(h.enrichContext(c), id); err != nil {
		return projectModelErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
