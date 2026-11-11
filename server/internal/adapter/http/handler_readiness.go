package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (h *Handler) registerReadinessRoutes(app fiber.Router) {
	if h.readinessSvc == nil {
		return
	}
	app.Get("/v1/repositories/:id/readiness", h.GetRepositoryReadiness)
	app.Put("/v1/repositories/:id/readiness", h.SetReadinessDismissed)
	app.Post("/v1/repositories/:id/readiness/run-all", h.RunReadinessAll)
	app.Put("/v1/repositories/:id/readiness/steps/:stepId", h.SetReadinessStepSkipped)
	app.Post("/v1/repositories/:id/readiness/steps/:stepId/run", h.RunReadinessStep)
}

func readinessErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrReadinessUnknownStep), errors.Is(err, port.ErrNotFound):
		return notFound(c, err.Error())
	case errors.Is(err, domain.ErrReadinessNotApproved):
		return badRequest(c, err.Error())
	case errors.Is(err, domain.ErrReadinessNotRunnable), errors.Is(err, domain.ErrReadinessPlanChanged):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "conflict"},
		})
	default:
		return badRequestErr(c, err)
	}
}

// GetRepositoryReadiness — GET /v1/repositories/:id/readiness
// The "get this project ready" checklist, computed from live data.
func (h *Handler) GetRepositoryReadiness(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	report, err := h.readinessSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return readinessErr(c, err)
	}
	return c.JSON(report)
}

// SetReadinessDismissed — PUT /v1/repositories/:id/readiness {"dismissed": bool}
func (h *Handler) SetReadinessDismissed(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		Dismissed bool `json:"dismissed"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ctx := h.enrichContext(c)
	if err := h.readinessSvc.SetDismissed(ctx, id, req.Dismissed); err != nil {
		return readinessErr(c, err)
	}
	return h.GetRepositoryReadiness(c)
}

// SetReadinessStepSkipped — PUT /v1/repositories/:id/readiness/steps/:stepId {"skipped": bool}
func (h *Handler) SetReadinessStepSkipped(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		Skipped bool `json:"skipped"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.readinessSvc.SetSkipped(h.enrichContext(c), id, c.Params("stepId"), req.Skipped); err != nil {
		return readinessErr(c, err)
	}
	return h.GetRepositoryReadiness(c)
}

// RunReadinessStep — POST /v1/repositories/:id/readiness/steps/:stepId/run {"approve": true}
// Starts one step. A step that opens a board task is refused without
// approve: true, because the click is the person's approval to spend an
// agent's time.
func (h *Handler) RunReadinessStep(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		Approve bool `json:"approve"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	res, err := h.readinessSvc.Run(h.enrichContext(c), id, c.Params("stepId"), req.Approve)
	if err != nil {
		return readinessErr(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(res)
}

// RunReadinessAll — POST /v1/repositories/:id/readiness/run-all {"approve_tasks": n}
// Starts every runnable step in order. approve_tasks must equal the number of
// tasks the checklist said it would open (run_all.tasks).
func (h *Handler) RunReadinessAll(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		ApproveTasks *int `json:"approve_tasks"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	res, err := h.readinessSvc.RunAll(h.enrichContext(c), id, req.ApproveTasks)
	if err != nil {
		return readinessErr(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(res)
}
