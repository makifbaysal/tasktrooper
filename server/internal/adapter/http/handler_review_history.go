package http

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerReviewHistoryRoutes(app fiber.Router) {
	app.Get("/v1/repositories/:id/tasks/:taskId/review-history", h.ReviewHistory)
	app.Post("/v1/repositories/:id/tasks/:taskId/review-flags/:flagId/reviewed", h.MarkReviewFlagReviewed)
}

// ReviewHistory — GET /v1/repositories/:id/tasks/:taskId/review-history
//
// The task's whole review loop read from the records the board keeps: every
// round with its verdicts, findings (and what became of each), the moves it
// caused, the runs that answered it and what each reviewer covered; plus the
// flags a person should look at, with who marked them reviewed.
func (h *Handler) ReviewHistory(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	history, err := h.repositorySvc.ReviewHistory(h.enrichContext(c), repositoryID, taskID)
	if err != nil {
		if errors.Is(err, domain.ErrBoardTaskNotFound) {
			return notFound(c, err.Error())
		}
		return internalError(c, err)
	}
	return c.JSON(history)
}

// MarkReviewFlagReviewed — POST /v1/repositories/:id/tasks/:taskId/review-flags/:flagId/reviewed
//
// Records who looked at one flag and when. Idempotent: the first person to
// mark it keeps the credit. Informational only; nothing moves because of it.
func (h *Handler) MarkReviewFlagReviewed(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	ctx := h.enrichContext(c)
	by := strings.TrimSpace(registry.ActorUserIDFromContext(ctx))
	if by == "" {
		by = "local"
	}
	// fiber hands out c.Params from a buffer it reuses; the key is stored.
	flag, err := h.repositorySvc.MarkReviewFlagReviewed(ctx, repositoryID, taskID, strings.Clone(c.Params("flagId")), by)
	if err != nil {
		if errors.Is(err, domain.ErrBoardTaskNotFound) || errors.Is(err, domain.ErrReviewFlagNotFound) {
			return notFound(c, err.Error())
		}
		return internalError(c, err)
	}
	return c.JSON(flag)
}
