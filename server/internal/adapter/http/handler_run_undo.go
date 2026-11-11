package http

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// RunUndoControl takes a run's work back. Declared here, like BoardRunControl,
// to keep this package clear of the board package.
type RunUndoControl interface {
	Preview(ctx context.Context, repositoryID, taskID, runID uuid.UUID) (domain.RunUndoPreview, error)
	Undo(ctx context.Context, repositoryID, taskID, runID uuid.UUID, req domain.RunUndoRequest) (domain.RunUndoResult, error)
	Revert(ctx context.Context, repositoryID, taskID, runID uuid.UUID, req domain.RunUndoRequest) (domain.RunRevertResult, error)
}

func (h *Handler) registerRunUndoRoutes(app fiber.Router) {
	app.Get("/v1/repositories/:id/tasks/:taskId/runs/:runId/undo/preview", h.RunUndoPreview)
	app.Post("/v1/repositories/:id/tasks/:taskId/runs/:runId/undo", h.RunUndo)
	app.Post("/v1/repositories/:id/tasks/:taskId/runs/:runId/undo/revert", h.RunUndoRevert)
}

func undoError(c *fiber.Ctx, err error) error {
	var blocked *domain.UndoBlockedError
	switch {
	case errors.Is(err, domain.ErrUndoConfirmRequired):
		return badRequest(c, err.Error())
	case errors.As(err, &blocked), errors.Is(err, domain.ErrUndoUseRevert), errors.Is(err, domain.ErrUndoNotMerged),
		errors.Is(err, domain.ErrUndoRemoteAhead):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{Error: errorDetail{Message: err.Error(), Type: "conflict"}})
	case errors.Is(err, domain.ErrTaskAgentRunNotFound):
		return runControlError(c, err)
	default:
		return internalError(c, err)
	}
}

// RunUndoPreview — GET /v1/repositories/:id/tasks/:taskId/runs/:runId/undo/preview
//
// What undoing the run would do, computed without changing anything: the
// commits and files that go back, the backup it would write first, whether the
// branch was pushed or its pull request merged, and the effects no reset takes
// back. `supported: false` carries a `reason` code.
func (h *Handler) RunUndoPreview(c *fiber.Ctx) error {
	if h.runUndo == nil {
		return boardRunControlDisabled(c)
	}
	repositoryID, taskID, runID, err := parseRepositoryTaskRunParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	preview, err := h.runUndo.Preview(h.enrichContext(c), repositoryID, taskID, runID)
	if err != nil {
		return undoError(c, err)
	}
	return c.JSON(fiber.Map{"preview": preview})
}

// RunUndo — POST /v1/repositories/:id/tasks/:taskId/runs/:runId/undo
//
// Body {confirm: true, force_push?, keep_column?}. The click is the approval.
// Resets the run's own task branch to the commit the run started from, behind a
// backup branch, and never touches the default branch.
func (h *Handler) RunUndo(c *fiber.Ctx) error {
	if h.runUndo == nil {
		return boardRunControlDisabled(c)
	}
	repositoryID, taskID, runID, err := parseRepositoryTaskRunParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req domain.RunUndoRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	result, err := h.runUndo.Undo(h.enrichContext(c), repositoryID, taskID, runID, req)
	if err != nil {
		return undoError(c, err)
	}
	return c.JSON(fiber.Map{"result": result})
}

// RunUndoRevert — POST /v1/repositories/:id/tasks/:taskId/runs/:runId/undo/revert
//
// For a merged pull request: history is not rewritten; a revert branch is cut
// from the default branch, pushed, and a pull request opened.
func (h *Handler) RunUndoRevert(c *fiber.Ctx) error {
	if h.runUndo == nil {
		return boardRunControlDisabled(c)
	}
	repositoryID, taskID, runID, err := parseRepositoryTaskRunParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req domain.RunUndoRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	result, err := h.runUndo.Revert(h.enrichContext(c), repositoryID, taskID, runID, req)
	if err != nil {
		return undoError(c, err)
	}
	return c.JSON(fiber.Map{"result": result})
}
