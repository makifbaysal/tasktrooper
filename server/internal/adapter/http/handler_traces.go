package http

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
)

// registerTraceRoutes mounts a component's traces, read through its
// confirmed traces.spans binding, and the Logs explorer's trace link. See
// .ai/api-spec.md ("Logs and traces data sources").
func (h *Handler) registerTraceRoutes(app fiber.Router) {
	if h.signalsSvc == nil {
		return
	}
	app.Get("/v1/components/:componentId/traces", h.ComponentTraces)
	app.Get("/v1/components/:componentId/traces/:traceId", h.ComponentTrace)
	app.Get("/v1/operations/traces/:traceId", h.OperationsTrace)
}

// tracesErr: a trace read with no binding to go through is 422 "unbound",
// one that ran out of time 504 "timeout"; the rest are the signal routes'.
func tracesErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, signals.ErrUnbound):
		return cErrCode(c, fiber.StatusUnprocessableEntity, err, "unbound")
	case errors.Is(err, context.DeadlineExceeded):
		return cErrCode(c, fiber.StatusGatewayTimeout, err, "timeout")
	default:
		return signalsErr(c, err)
	}
}

// ComponentTraces — GET /v1/components/:componentId/traces?env=&since=&until=&min_duration_ms=&limit=
func (h *Handler) ComponentTraces(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	fe := datasource.FieldErrors{}
	since, ok := optionalRFC3339(c.Query("since"))
	if !ok {
		fe["since"] = datasource.FieldInvalid
	}
	until, ok := optionalRFC3339(c.Query("until"))
	if !ok {
		fe["until"] = datasource.FieldInvalid
	}
	q := signals.TracesQuery{Environment: opsEnvironment(c), Since: since, Until: until}
	if raw := c.Query("min_duration_ms"); raw != "" {
		ms, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || ms < 0 || ms > datasource.MaxTraceMinDuration.Milliseconds() {
			fe["min_duration_ms"] = datasource.FieldInvalid
		} else {
			q.MinDuration = time.Duration(ms) * time.Millisecond
		}
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			fe["limit"] = datasource.FieldInvalid
		}
		q.Limit = n
	}
	if len(fe) > 0 {
		return dataSourceErr(c, fe)
	}
	out, err := h.signalsSvc.Traces(h.enrichContext(c), id, q)
	if err != nil {
		return tracesErr(c, err)
	}
	return c.JSON(out)
}

// ComponentTrace — GET /v1/components/:componentId/traces/:traceId?env=
func (h *Handler) ComponentTrace(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	out, err := h.signalsSvc.Trace(h.enrichContext(c), id, opsEnvironment(c), c.Params("traceId"))
	if err != nil {
		return tracesErr(c, err)
	}
	return c.JSON(out)
}

// OperationsTrace — GET /v1/operations/traces/:traceId?component_id=&env=
// The Logs explorer's trace link: through that component's binding, or
// without component_id through each traces source the environment's
// bindings read.
func (h *Handler) OperationsTrace(c *fiber.Ctx) error {
	ctx := h.enrichContext(c)
	traceID := c.Params("traceId")
	raw := c.Query("component_id")
	if raw == "" {
		out, err := h.signalsSvc.FindTrace(ctx, opsEnvironment(c), traceID)
		if err != nil {
			return tracesErr(c, err)
		}
		return c.JSON(out)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component_id")
	}
	out, err := h.signalsSvc.Trace(ctx, id, opsEnvironment(c), traceID)
	if err != nil {
		return tracesErr(c, err)
	}
	return c.JSON(out)
}
