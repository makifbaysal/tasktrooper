package http

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/smokegen"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// SmokeGenService is application/smokegen.Service narrowed to what the routes
// call, so a handler test can stand in for it.
type SmokeGenService interface {
	Generate(ctx context.Context, componentID uuid.UUID, existing []domain.SmokeCheck) (smokegen.Job, error)
	Get(jobID uuid.UUID) (smokegen.Job, error)
	Cancel(jobID uuid.UUID)
}

func (h *Handler) registerSmokeGenRoutes(app fiber.Router) {
	if h.smokeGenSvc == nil {
		return
	}
	app.Post("/v1/components/:componentId/smoke-checks/generate", h.GenerateSmokeChecks)
	app.Get("/v1/smoke-check-generations/:jobId", h.GetSmokeCheckGeneration)
	app.Delete("/v1/smoke-check-generations/:jobId", h.CancelSmokeCheckGeneration)
}

func smokeGenError(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(errorResponse{Error: errorDetail{Message: msg, Type: "invalid_request_error"}})
}

// GenerateSmokeChecks — POST /v1/components/:componentId/smoke-checks/generate
// Body: {"existing": SmokeCheck[]}. Starts (or returns the already running)
// generation; 202 with the job either way.
func (h *Handler) GenerateSmokeChecks(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return badRequest(c, "invalid component id")
	}
	var req struct {
		Existing []domain.SmokeCheck `json:"existing"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	job, err := h.smokeGenSvc.Generate(h.enrichContext(c), id, req.Existing)
	switch {
	case err == nil:
		return c.Status(fiber.StatusAccepted).JSON(job)
	case errors.Is(err, port.ErrNotFound):
		return smokeGenError(c, fiber.StatusNotFound, "component not found")
	case errors.Is(err, smokegen.ErrNoAgent):
		return smokeGenError(c, fiber.StatusConflict, err.Error())
	default:
		return internalError(c, err)
	}
}

// GetSmokeCheckGeneration — GET /v1/smoke-check-generations/:jobId
func (h *Handler) GetSmokeCheckGeneration(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "jobId")
	if err != nil {
		return badRequest(c, "invalid job id")
	}
	job, err := h.smokeGenSvc.Get(id)
	if err != nil {
		return smokeGenError(c, fiber.StatusNotFound, err.Error())
	}
	return c.JSON(job)
}

// CancelSmokeCheckGeneration — DELETE /v1/smoke-check-generations/:jobId
// Idempotent: an unknown or finished job is already as stopped as it gets.
func (h *Handler) CancelSmokeCheckGeneration(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "jobId")
	if err != nil {
		return badRequest(c, "invalid job id")
	}
	h.smokeGenSvc.Cancel(id)
	return c.SendStatus(fiber.StatusNoContent)
}
