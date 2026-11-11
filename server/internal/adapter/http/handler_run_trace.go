package http

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (h *Handler) registerRunTraceRoutes(app fiber.Router) {
	app.Get("/v1/repositories/:id/tasks/:taskId/runs/:runId/trace", h.RunTrace)
	app.Get("/v1/repositories/:id/tasks/:taskId/runs/:runId/trace/summary", h.RunTraceSummary)
	app.Get("/v1/repositories/:id/tasks/:taskId/runs/:runId/trace.json", h.RunTraceExport)
}

// ownedRun resolves the run named in the URL and checks it belongs to the task
// named there; a run of another task is a 404, like the run-control routes.
func (h *Handler) ownedRun(c *fiber.Ctx) (context.Context, uuid.UUID, bool, error) {
	_, taskID, runID, err := parseRepositoryTaskRunParams(c)
	if err != nil {
		return nil, uuid.Nil, false, badRequest(c, err.Error())
	}
	ctx := h.enrichContext(c)
	if h.runTraces == nil || h.taskRuns == nil {
		return ctx, runID, false, nil
	}
	run, err := h.taskRuns.GetByID(ctx, runID)
	if err != nil {
		return nil, uuid.Nil, false, runControlError(c, err)
	}
	if run.TaskID != taskID {
		return nil, uuid.Nil, false, runControlError(c, domain.ErrTaskAgentRunNotFound)
	}
	return ctx, runID, true, nil
}

func notRecorded(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"recorded": false, "events": []domain.RunTraceEvent{}, "count": 0})
}

// RunTrace — GET /v1/repositories/:id/tasks/:taskId/runs/:runId/trace
//
// The run's tool calls in order, recorded at the moment of each call. Query:
// category (read|change|exec|external|board|undo), after (seq of the last row
// already read), limit (<= 1000).
func (h *Handler) RunTrace(c *fiber.Ctx) error {
	ctx, runID, ok, err := h.ownedRun(c)
	if err != nil {
		return err
	}
	if !ok {
		return notRecorded(c)
	}
	filter := port.RunTraceFilter{Category: c.Query("category"), Limit: 200}
	if filter.Category != "" && !domain.ValidTraceCategory(filter.Category) {
		return badRequest(c, "invalid category")
	}
	if n, perr := strconv.Atoi(c.Query("after")); perr == nil && n > 0 {
		filter.After = n
	}
	if n, perr := strconv.Atoi(c.Query("limit")); perr == nil && n > 0 {
		filter.Limit = min(n, 1000)
	}
	if _, err := h.runTraces.Header(ctx, runID); err != nil {
		if errors.Is(err, domain.ErrRunTraceNotFound) {
			return notRecorded(c)
		}
		return internalError(c, err)
	}
	events, err := h.runTraces.List(ctx, runID, filter)
	if err != nil {
		return internalError(c, err)
	}
	next := 0
	if len(events) == filter.Limit {
		next = events[len(events)-1].Seq
	}
	return c.JSON(fiber.Map{"recorded": true, "events": events, "count": len(events), "next_after": next})
}

func (h *Handler) buildTraceSummary(ctx context.Context, runID uuid.UUID) (domain.RunTraceSummary, error) {
	header, err := h.runTraces.Header(ctx, runID)
	if err != nil {
		return domain.RunTraceSummary{}, err
	}
	counts, err := h.runTraces.Counts(ctx, runID)
	if err != nil {
		return domain.RunTraceSummary{}, err
	}
	effects, err := h.runTraces.Effects(ctx, runID)
	if err != nil {
		return domain.RunTraceSummary{}, err
	}
	return domain.RunTraceSummary{
		Run: header, Counts: counts, Verification: header.Verification,
		NonUndoable: runtrace.SummaryEffects(effects),
	}, nil
}

// RunTraceSummary — GET /v1/repositories/:id/tasks/:taskId/runs/:runId/trace/summary
//
// Counts per category, git's own list of changed files compared with the
// recorded changes, and the effects no git reset takes back.
func (h *Handler) RunTraceSummary(c *fiber.Ctx) error {
	ctx, runID, ok, err := h.ownedRun(c)
	if err != nil {
		return err
	}
	if !ok {
		return c.JSON(fiber.Map{"recorded": false})
	}
	summary, err := h.buildTraceSummary(ctx, runID)
	if errors.Is(err, domain.ErrRunTraceNotFound) {
		return c.JSON(fiber.Map{"recorded": false})
	}
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"recorded": true, "summary": summary})
}

// RunTraceExport — GET /v1/repositories/:id/tasks/:taskId/runs/:runId/trace.json
//
// The whole trace as one downloadable JSON document.
func (h *Handler) RunTraceExport(c *fiber.Ctx) error {
	ctx, runID, ok, err := h.ownedRun(c)
	if err != nil {
		return err
	}
	if !ok {
		return notFound(c, "no trace recorded for this run")
	}
	summary, err := h.buildTraceSummary(ctx, runID)
	if errors.Is(err, domain.ErrRunTraceNotFound) {
		return notFound(c, "no trace recorded for this run")
	}
	if err != nil {
		return internalError(c, err)
	}
	events := []domain.RunTraceEvent{}
	after := 0
	for {
		page, err := h.runTraces.List(ctx, runID, port.RunTraceFilter{After: after, Limit: 1000})
		if err != nil {
			return internalError(c, err)
		}
		events = append(events, page...)
		if len(page) < 1000 {
			break
		}
		after = page[len(page)-1].Seq
	}
	c.Set("Content-Disposition", `attachment; filename="run-`+runID.String()+`-trace.json"`)
	return c.JSON(fiber.Map{"exported_at": time.Now().UTC(), "summary": summary, "events": events})
}
