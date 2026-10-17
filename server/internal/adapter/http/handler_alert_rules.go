package http

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/alerting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// registerAlertRuleRoutes mounts alert rules and their evaluation history.
// See .ai/api-spec.md ("Alert rules (migration 189)") for the contract.
func (h *Handler) registerAlertRuleRoutes(app fiber.Router) {
	if h.alertingSvc == nil {
		return
	}
	app.Get("/v1/alert-rules", h.ListAlertRules)
	app.Post("/v1/alert-rules", h.CreateAlertRule)
	app.Post("/v1/alert-rules/backtest", h.BacktestAlertRule)
	app.Post("/v1/alert-rules/suggest", h.SuggestAlertRules)
	app.Get("/v1/alert-rules/:ruleId", h.GetAlertRule)
	app.Patch("/v1/alert-rules/:ruleId", h.UpdateAlertRule)
	app.Delete("/v1/alert-rules/:ruleId", h.DeleteAlertRule)
	app.Get("/v1/alert-rules/:ruleId/events", h.ListAlertRuleEvents)
	app.Get("/v1/notification-channels", h.ListNotificationChannels)
}

// ListNotificationChannels — GET /v1/notification-channels: every channel kind
// a rule may name and whether this server can deliver on it; a rule naming
// an unavailable one is refused with field error "unavailable".
func (h *Handler) ListNotificationChannels(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"channels": h.alertingSvc.Channels()})
}

// alertingErr: field errors in the data source routes' shape, 422 code
// "unbound" for a signal with no confirmed binding to read, then the signal
// and data source routes' own cases.
func alertingErr(c *fiber.Ctx, err error) error {
	var fe alerting.FieldErrors
	switch {
	case errors.As(err, &fe):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error(), "fields": fe})
	case errors.Is(err, signals.ErrUnbound):
		return cErrCode(c, fiber.StatusUnprocessableEntity, err, "unbound")
	case errors.Is(err, alerting.ErrInvalidInput):
		return cErr(c, fiber.StatusBadRequest, err)
	default:
		return signalsErr(c, err)
	}
}

// ListAlertRules — GET /v1/alert-rules?component_id=&env=
func (h *Handler) ListAlertRules(c *fiber.Ctx) error {
	var filter domain.AlertRuleFilter
	if raw := c.Query("component_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return alertingErr(c, alerting.FieldErrors{"component_id": datasource.FieldInvalid})
		}
		filter.ComponentID = &id
	}
	filter.Environment = domain.DeployEnvironment(c.Query("env"))
	rules, err := h.alertingSvc.List(h.enrichContext(c), filter)
	if err != nil {
		return alertingErr(c, err)
	}
	return c.JSON(fiber.Map{"rules": rules})
}

// CreateAlertRule — POST /v1/alert-rules → 201 Rule
func (h *Handler) CreateAlertRule(c *fiber.Ctx) error {
	var req alerting.RuleRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	rule, err := h.alertingSvc.Create(h.enrichContext(c), req)
	if err != nil {
		return alertingErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(rule)
}

// GetAlertRule — GET /v1/alert-rules/:ruleId
func (h *Handler) GetAlertRule(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "ruleId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid alert rule id")
	}
	rule, err := h.alertingSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return alertingErr(c, err)
	}
	return c.JSON(rule)
}

// UpdateAlertRule — PATCH /v1/alert-rules/:ruleId → Rule
func (h *Handler) UpdateAlertRule(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "ruleId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid alert rule id")
	}
	var req alerting.RuleRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	rule, err := h.alertingSvc.Update(h.enrichContext(c), id, req)
	if err != nil {
		return alertingErr(c, err)
	}
	return c.JSON(rule)
}

// DeleteAlertRule — DELETE /v1/alert-rules/:ruleId → 204
func (h *Handler) DeleteAlertRule(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "ruleId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid alert rule id")
	}
	if err := h.alertingSvc.Delete(h.enrichContext(c), id); err != nil {
		return alertingErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListAlertRuleEvents — GET /v1/alert-rules/:ruleId/events?limit=
func (h *Handler) ListAlertRuleEvents(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "ruleId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid alert rule id")
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil || limit < 1 {
			return alertingErr(c, alerting.FieldErrors{"limit": datasource.FieldInvalid})
		}
	}
	events, err := h.alertingSvc.Events(h.enrichContext(c), id, limit)
	if err != nil {
		return alertingErr(c, err)
	}
	return c.JSON(fiber.Map{"events": events})
}

// BacktestAlertRule — POST /v1/alert-rules/backtest {…rule, days?}
func (h *Handler) BacktestAlertRule(c *fiber.Ctx) error {
	var req alerting.BacktestRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	out, err := h.alertingSvc.Backtest(h.enrichContext(c), req)
	if err != nil {
		return alertingErr(c, err)
	}
	return c.JSON(out)
}

// SuggestAlertRules — POST /v1/alert-rules/suggest {component_id, environment}
func (h *Handler) SuggestAlertRules(c *fiber.Ctx) error {
	var req alerting.SuggestRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	rules, err := h.alertingSvc.Suggest(h.enrichContext(c), req)
	if err != nil {
		return alertingErr(c, err)
	}
	return c.JSON(fiber.Map{"rules": rules})
}
