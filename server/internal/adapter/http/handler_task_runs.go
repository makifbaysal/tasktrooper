package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// CancelTaskAgentRun — POST /v1/repositories/:id/tasks/:taskId/runs/:runId/cancel
//
// Stops a pending/running run and parks its task as blocked. The optional
// reason is shown on the blocked card.
func (h *Handler) CancelTaskAgentRun(c *fiber.Ctx) error {
	if h.runControl == nil {
		return boardRunControlDisabled(c)
	}
	repositoryID, taskID, runID, err := parseRepositoryTaskRunParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	// An empty body is the normal case (the drawer's stop button sends none),
	// so a parse failure only means "no reason given".
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.BodyParser(&req)

	run, err := h.runControl.CancelRun(h.enrichContext(c), repositoryID, taskID, runID, req.Reason)
	if err != nil {
		return runControlError(c, err)
	}
	return c.JSON(fiber.Map{"run": run})
}

// InterruptTaskAgentRun — POST /v1/repositories/:id/tasks/:taskId/runs/:runId/interrupt
//
// Breaks into a live run with guidance: the run stops, the message is recorded
// on the task and the same agent starts again. The task is NOT blocked.
func (h *Handler) InterruptTaskAgentRun(c *fiber.Ctx) error {
	if h.runControl == nil {
		return boardRunControlDisabled(c)
	}
	repositoryID, taskID, runID, err := parseRepositoryTaskRunParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	run, err := h.runControl.InterruptRun(h.enrichContext(c), repositoryID, taskID, runID, req.Message)
	if err != nil {
		if errors.Is(err, domain.ErrInterruptMessageRequired) {
			return badRequest(c, err.Error())
		}
		return runControlError(c, err)
	}
	return c.JSON(fiber.Map{"run": run})
}

// RerunTaskAgentRun — POST /v1/repositories/:id/tasks/:taskId/runs/:runId/rerun
//
// Queues a new run of the same agent on the task's current state and returns
// that NEW run; the run named in the URL stays untouched history.
func (h *Handler) RerunTaskAgentRun(c *fiber.Ctx) error {
	if h.runControl == nil {
		return boardRunControlDisabled(c)
	}
	repositoryID, taskID, runID, err := parseRepositoryTaskRunParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	run, err := h.runControl.RerunRun(h.enrichContext(c), repositoryID, taskID, runID)
	if err != nil {
		return runControlError(c, err)
	}
	return c.JSON(fiber.Map{"run": run})
}

func parseRepositoryTaskRunParams(c *fiber.Ctx) (uuid.UUID, uuid.UUID, uuid.UUID, error) {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	runID, err := uuid.Parse(c.Params("runId"))
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	return repositoryID, taskID, runID, nil
}

// runControlError maps the refusals to status codes. All three 409s are races
// or state the caller could not have known about from the run list it rendered,
// so the sentinel's own message is the body — it is what the UI shows.
func runControlError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrTaskAgentRunNotFound):
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	case errors.Is(err, domain.ErrRunNotLive),
		errors.Is(err, domain.ErrRunNotTerminal),
		errors.Is(err, domain.ErrTaskBlockedForRerun):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "conflict"},
		})
	default:
		return internalError(c, err)
	}
}

// boardRunControlDisabled: a desktop build with no board runner has runs to
// list but nothing to stop or start.
func boardRunControlDisabled(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "board runs not enabled", Type: "service_unavailable"},
	})
}
