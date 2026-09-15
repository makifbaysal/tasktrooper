package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodependency"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerRepoDependencyRoutes exposes each repository's dependency edges
// (sub-project, repository, or manually-recorded database) and the
// project-wide view they feed.
func (h *Handler) registerRepoDependencyRoutes(app fiber.Router) {
	if h.repoDependencySvc == nil {
		return
	}
	app.Get("/v1/repositories/:id/dependencies", h.ListRepoDependencies)
	app.Post("/v1/repositories/:id/dependencies", h.CreateRepoDependency)
	app.Patch("/v1/repositories/:id/dependencies/:depId", h.UpdateRepoDependency)
	app.Delete("/v1/repositories/:id/dependencies/:depId", h.DeleteRepoDependency)
	app.Get("/v1/projects/:projectId/dependencies", h.ListProjectDependencies)
}

// repoDependencyError maps the service's sentinels: a caller mistake or a
// same-project/sub-project rule violation is 400, an absent repository or
// dependency is 404, anything else is ours.
func repoDependencyError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, repodependency.ErrInvalidInput),
		errors.Is(err, repodependency.ErrNotSameProject),
		errors.Is(err, repodependency.ErrSubProjectNotFound):
		return badRequest(c, err.Error())
	case errors.Is(err, port.ErrNotFound):
		return notFound(c, err.Error())
	default:
		return internalError(c, err)
	}
}

// ListRepoDependencies — GET /v1/repositories/:id/dependencies
func (h *Handler) ListRepoDependencies(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	deps, err := h.repoDependencySvc.List(h.enrichContext(c), id)
	if err != nil {
		return repoDependencyError(c, err)
	}
	return c.JSON(fiber.Map{"dependencies": deps})
}

// CreateRepoDependency — POST /v1/repositories/:id/dependencies
func (h *Handler) CreateRepoDependency(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req domain.SaveRepoDependencyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	dep, err := h.repoDependencySvc.Create(h.enrichContext(c), id, req)
	if err != nil {
		return repoDependencyError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(dep)
}

// UpdateRepoDependency — PATCH /v1/repositories/:id/dependencies/:depId
func (h *Handler) UpdateRepoDependency(c *fiber.Ctx) error {
	depID, err := uuid.Parse(c.Params("depId"))
	if err != nil {
		return badRequest(c, "invalid dependency id")
	}
	var req domain.SaveRepoDependencyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	dep, err := h.repoDependencySvc.Update(h.enrichContext(c), depID, req)
	if err != nil {
		return repoDependencyError(c, err)
	}
	return c.JSON(dep)
}

// DeleteRepoDependency — DELETE /v1/repositories/:id/dependencies/:depId
func (h *Handler) DeleteRepoDependency(c *fiber.Ctx) error {
	depID, err := uuid.Parse(c.Params("depId"))
	if err != nil {
		return badRequest(c, "invalid dependency id")
	}
	if err := h.repoDependencySvc.Delete(h.enrichContext(c), depID); err != nil {
		return repoDependencyError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListProjectDependencies — GET /v1/projects/:projectId/dependencies
func (h *Handler) ListProjectDependencies(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("projectId"))
	if err != nil {
		return badRequest(c, "invalid project id")
	}
	outgoing, incoming, err := h.repoDependencySvc.ListForProject(h.enrichContext(c), id)
	if err != nil {
		return repoDependencyError(c, err)
	}
	return c.JSON(fiber.Map{"outgoing": outgoing, "incoming": incoming})
}
