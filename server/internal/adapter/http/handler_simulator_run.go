package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// registerSimulatorRunRoutes exposes "build this task and open it on a
// simulator of this machine". Absent when the host has no simulator service.
func (h *Handler) registerSimulatorRunRoutes(app fiber.Router) {
	if h.simRunSvc == nil {
		return
	}
	app.Get("/v1/simulator/devices", h.ListSimulatorDevices)
	app.Post("/v1/repositories/:id/tasks/:taskId/simulator-run", h.StartSimulatorRun)
	app.Get("/v1/repositories/:id/tasks/:taskId/simulator-run", h.GetSimulatorRun)
}

// simulatorRunError: a machine with nothing to run on, a repository with no
// app, or a run already going are all the state of the world (409), not a
// malformed request.
func simulatorRunError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrSimulatorRunNotFound):
		return notFound(c, err.Error())
	case errors.Is(err, domain.ErrNoSimulators), errors.Is(err, domain.ErrSimulatorRunBusy), errors.Is(err, domain.ErrSimulatorNotMobile):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	default:
		return internalError(c, err)
	}
}

// ListSimulatorDevices — GET /v1/simulator/devices
func (h *Handler) ListSimulatorDevices(c *fiber.Ctx) error {
	devices, err := h.simRunSvc.Devices(h.enrichContext(c))
	if err != nil {
		return simulatorRunError(c, err)
	}
	return c.JSON(devices)
}

// StartSimulatorRun — POST /v1/repositories/:id/tasks/:taskId/simulator-run
// Body: {"platform": "ios"|"android", "device_id": "..."}. 202: the build runs
// on its own and is read back with the GET.
func (h *Handler) StartSimulatorRun(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	taskID, err := uuid.Parse(c.Params("taskId"))
	if err != nil {
		return badRequest(c, "invalid task id")
	}
	var req struct {
		Platform string `json:"platform"`
		DeviceID string `json:"device_id"`
	}
	if err := c.BodyParser(&req); err != nil || req.Platform == "" || req.DeviceID == "" {
		return badRequest(c, "platform and device_id are required")
	}
	run, err := h.simRunSvc.Start(h.enrichContext(c), id, taskID, req.Platform, req.DeviceID)
	if err != nil {
		return simulatorRunError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(run)
}

// GetSimulatorRun — GET /v1/repositories/:id/tasks/:taskId/simulator-run
func (h *Handler) GetSimulatorRun(c *fiber.Ctx) error {
	taskID, err := uuid.Parse(c.Params("taskId"))
	if err != nil {
		return badRequest(c, "invalid task id")
	}
	run, err := h.simRunSvc.Latest(taskID)
	if err != nil {
		return simulatorRunError(c, err)
	}
	if run.RepositoryID.String() != c.Params("id") {
		return notFound(c, domain.ErrSimulatorRunNotFound.Error())
	}
	return c.JSON(run)
}
