package http

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerSignalRoutes mounts signal bindings, a component's series and its
// queue, endpoints, web and crashes tables, and the explorer's golden values.
// See .ai/api-spec.md ("Signal bindings and series", "Queue tables",
// "Component tables") for the contract.
func (h *Handler) registerSignalRoutes(app fiber.Router) {
	if h.signalsSvc == nil {
		return
	}
	app.Get("/v1/components/:componentId/signals", h.ListComponentSignals)
	app.Post("/v1/components/:componentId/signals/suggest", h.SuggestComponentSignals)
	app.Post("/v1/components/:componentId/signals/preview", h.PreviewComponentSignal)
	app.Put("/v1/components/:componentId/signals/:signal", h.PutComponentSignal)
	app.Get("/v1/components/:componentId/series", h.ComponentSeries)
	app.Get("/v1/components/:componentId/queues", h.ComponentQueues)
	app.Get("/v1/components/:componentId/endpoints", h.ComponentEndpoints)
	app.Get("/v1/components/:componentId/web", h.ComponentWeb)
	app.Get("/v1/components/:componentId/crashes", h.ComponentCrashes)
	app.Post("/v1/signal-bindings/:bindingId/confirm", h.ConfirmSignalBinding)
	app.Post("/v1/signal-bindings/:bindingId/dismiss", h.DismissSignalBinding)
	app.Delete("/v1/signal-bindings/:bindingId", h.DeleteSignalBinding)
	app.Get("/v1/operations/golden", h.OperationsGolden)
}

// signalsErr adds the signal service's own cases to the data source routes'
// error shapes, which it otherwise shares (field errors, bad_query,
// unsupported, upstream).
func signalsErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, signals.ErrConfirmed):
		return cErrCode(c, fiber.StatusConflict, err, "confirmed")
	case errors.Is(err, port.ErrSignalBindingConflict):
		return cErrCode(c, fiber.StatusConflict, err, "conflict")
	case errors.Is(err, signals.ErrInvalidInput):
		return cErr(c, fiber.StatusBadRequest, err)
	default:
		return dataSourceErr(c, err)
	}
}

// ListComponentSignals — GET /v1/components/:componentId/signals?env=
func (h *Handler) ListComponentSignals(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	out, err := h.signalsSvc.List(h.enrichContext(c), id, opsEnvironment(c))
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

// SuggestComponentSignals — POST /v1/components/:componentId/signals/suggest?env=
// Runs the detector, then answers as GET does.
func (h *Handler) SuggestComponentSignals(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	out, err := h.signalsSvc.Suggest(h.enrichContext(c), id, opsEnvironment(c))
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

// PutComponentSignal — PUT /v1/components/:componentId/signals/:signal?env=
// {data_source_id, recipe_id?, params?, query?} → the confirmed binding.
func (h *Handler) PutComponentSignal(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	var req signals.BindingRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	out, err := h.signalsSvc.Put(h.enrichContext(c), id, opsEnvironment(c), c.Params("signal"), req)
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

type signalPreviewRequest struct {
	signals.BindingRequest
	Signal      string  `json:"signal"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	StepSeconds float64 `json:"step_seconds"`
}

// PreviewComponentSignal — POST /v1/components/:componentId/signals/preview
// {signal, data_source_id, recipe_id?, params?, query?, from, to, step_seconds?}
func (h *Handler) PreviewComponentSignal(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	var req signalPreviewRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	fe := datasource.FieldErrors{}
	from, ok := optionalRFC3339(req.From)
	if !ok {
		fe["from"] = datasource.FieldInvalid
	}
	to, ok := optionalRFC3339(req.To)
	if !ok {
		fe["to"] = datasource.FieldInvalid
	}
	if req.StepSeconds < 0 {
		fe["step_seconds"] = datasource.FieldInvalid
	}
	if len(fe) > 0 {
		return dataSourceErr(c, fe)
	}
	out, err := h.signalsSvc.Preview(h.enrichContext(c), id, signals.PreviewRequest{
		BindingRequest: req.BindingRequest,
		Signal:         req.Signal,
		From:           from,
		To:             to,
		Step:           time.Duration(req.StepSeconds * float64(time.Second)),
	})
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

func optionalRFC3339(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	return t, err == nil
}

// ComponentSeries — GET /v1/components/:componentId/series?env=&signals=a,b&from=&to=&step_seconds=&compare=1
func (h *Handler) ComponentSeries(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	from, err := opsTime(c, "from")
	if err != nil {
		return dataSourceErr(c, datasource.FieldErrors{"from": datasource.FieldInvalid})
	}
	to, err := opsTime(c, "to")
	if err != nil {
		return dataSourceErr(c, datasource.FieldErrors{"to": datasource.FieldInvalid})
	}
	var step time.Duration
	if raw := c.Query("step_seconds"); raw != "" {
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || n < 0 {
			return dataSourceErr(c, datasource.FieldErrors{"step_seconds": datasource.FieldInvalid})
		}
		step = time.Duration(n * float64(time.Second))
	}
	var ids []string
	for _, raw := range c.Context().QueryArgs().PeekMulti("signals") {
		ids = append(ids, strings.Split(string(raw), ",")...)
	}
	compare := c.Query("compare")
	out, err := h.signalsSvc.Series(h.enrichContext(c), id, signals.SeriesQuery{
		Environment: opsEnvironment(c),
		Signals:     ids,
		From:        from,
		To:          to,
		Step:        step,
		Compare:     compare == "1" || compare == "true",
	})
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

// ConfirmSignalBinding — POST /v1/signal-bindings/:bindingId/confirm
func (h *Handler) ConfirmSignalBinding(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "bindingId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid binding id")
	}
	out, err := h.signalsSvc.Confirm(h.enrichContext(c), id)
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

// DismissSignalBinding — POST /v1/signal-bindings/:bindingId/dismiss
// 409 code "confirmed" for the confirmed binding, which is deleted instead.
func (h *Handler) DismissSignalBinding(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "bindingId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid binding id")
	}
	if err := h.signalsSvc.Dismiss(h.enrichContext(c), id); err != nil {
		return signalsErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// DeleteSignalBinding — DELETE /v1/signal-bindings/:bindingId
func (h *Handler) DeleteSignalBinding(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "bindingId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid binding id")
	}
	if err := h.signalsSvc.Delete(h.enrichContext(c), id); err != nil {
		return signalsErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// OperationsGolden — GET /v1/operations/golden?env=&from=&to=&component_id=…
func (h *Handler) OperationsGolden(c *fiber.Ctx) error {
	ids, err := opsComponentIDs(c)
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	from, err := opsTime(c, "from")
	if err != nil {
		return dataSourceErr(c, datasource.FieldErrors{"from": datasource.FieldInvalid})
	}
	to, err := opsTime(c, "to")
	if err != nil {
		return dataSourceErr(c, datasource.FieldErrors{"to": datasource.FieldInvalid})
	}
	out, err := h.signalsSvc.Golden(h.enrichContext(c), signals.GoldenQuery{
		Environment:  opsEnvironment(c),
		From:         from,
		To:           to,
		ComponentIDs: ids,
	})
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

// ComponentQueues — GET /v1/components/:componentId/queues?env=
func (h *Handler) ComponentQueues(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	out, err := h.signalsSvc.Queues(h.enrichContext(c), id, opsEnvironment(c))
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}
