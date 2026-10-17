package http

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// registerOpsQueryRoutes mounts the Operations console's cross-component
// reads: the Errors inbox and the global Logs explorer, each one fan-out over
// the environments the cloud routes read one at a time. Errors use the cloud
// routes' flat {"error"[, "code"]} shape.
func (h *Handler) registerOpsQueryRoutes(app fiber.Router) {
	if h.opsQuerySvc == nil {
		return
	}
	app.Get("/v1/operations/errors", h.OperationsErrors)
	app.Get("/v1/operations/logs", h.OperationsLogs)
}

func opsQueryErr(c *fiber.Ctx, err error) error {
	var tooMany *opsquery.TooManyEnvironmentsError
	switch {
	case errors.As(err, &tooMany):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":            "too_many_environments",
			"code":             "too_many_environments",
			"matched":          tooMany.Matched,
			"max_environments": tooMany.Max,
		})
	case errors.Is(err, opsquery.ErrInvalidInput):
		return cErr(c, fiber.StatusBadRequest, err)
	default:
		return cErr(c, fiber.StatusInternalServerError, err)
	}
}

// opsEnvironment: an absent env reads production, the console's default view.
func opsEnvironment(c *fiber.Ctx) domain.DeployEnvironment {
	if raw := c.Query("env"); raw != "" {
		return domain.DeployEnvironment(raw)
	}
	return domain.EnvironmentProduction
}

// opsComponentIDs takes component_id repeated, comma-separated, or both.
func opsComponentIDs(c *fiber.Ctx) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	for _, raw := range c.Context().QueryArgs().PeekMulti("component_id") {
		for _, part := range strings.Split(string(raw), ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := uuid.Parse(part)
			if err != nil {
				return nil, errors.New("invalid component_id")
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func opsTime(c *fiber.Ctx, key string) (time.Time, error) {
	raw := c.Query(key)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errors.New("invalid " + key + ": must be RFC3339")
	}
	return t, nil
}

// OperationsErrors — GET /v1/operations/errors?env=&since=&component_id=…&include_ignored=
func (h *Handler) OperationsErrors(c *fiber.Ctx) error {
	ids, err := opsComponentIDs(c)
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	since, err := opsTime(c, "since")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	out, err := h.opsQuerySvc.Errors(h.enrichContext(c), opsquery.ErrorsQuery{
		Environment:  opsEnvironment(c),
		Since:        since,
		ComponentIDs: ids,
	})
	if err != nil {
		return opsQueryErr(c, err)
	}
	if h.errorDefsSvc != nil {
		includeIgnored := c.Query("include_ignored") == "1" || c.Query("include_ignored") == "true"
		out = h.errorDefsSvc.Annotate(h.enrichContext(c), out, includeIgnored)
	}
	return c.JSON(out)
}

// OperationsLogs — GET /v1/operations/logs?env=&since=&until=&component_id=…&min_severity=&text=&limit=
func (h *Handler) OperationsLogs(c *fiber.Ctx) error {
	ids, err := opsComponentIDs(c)
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	since, err := opsTime(c, "since")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	until, err := opsTime(c, "until")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	q := opsquery.LogsQuery{
		Environment:  opsEnvironment(c),
		ComponentIDs: ids,
		Since:        since,
		Until:        until,
		Text:         c.Query("text"),
	}
	if raw := c.Query("min_severity"); raw != "" {
		sev := domain.LogSeverity(raw)
		if !validLogSeverity(sev) {
			return cErrMsg(c, fiber.StatusBadRequest, "invalid min_severity")
		}
		q.MinSeverity = sev
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return cErrMsg(c, fiber.StatusBadRequest, "invalid limit")
		}
		q.Limit = n
	}
	out, err := h.opsQuerySvc.Logs(h.enrichContext(c), q)
	if err != nil {
		return opsQueryErr(c, err)
	}
	return c.JSON(out)
}
