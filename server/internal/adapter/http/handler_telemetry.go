package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	telemetryapp "github.com/makifbaysal/tasktrooper/server/internal/application/telemetry"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	telemetryNotConfiguredCode = "telemetry_not_configured"
	telemetryUnreachableCode   = "telemetry_unreachable"
	telemetryUnauthorizedCode  = "telemetry_unauthorized"
	telemetryPathCode          = "telemetry_path_not_allowed"
)

func (h *Handler) registerTelemetryRoutes(app fiber.Router) {
	if h.telemetrySvc == nil {
		return
	}
	app.Get("/v1/telemetry/settings", h.GetTelemetrySettings)
	app.Put("/v1/telemetry/settings", h.SetTelemetrySettings)
	app.Post("/v1/telemetry/settings/test", h.TestTelemetrySettings)
	app.Get("/v1/telemetry/status", h.GetTelemetryStatus)
	app.Get("/v1/telemetry/proxy/*", h.TelemetryProxy)
}

func telemetryCoded(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{"code": code, "error": message})
}

// GetTelemetrySettings — GET /v1/telemetry/settings
func (h *Handler) GetTelemetrySettings(c *fiber.Ctx) error {
	out, err := h.telemetrySvc.Settings(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

type setTelemetryRequest struct {
	BaseURL string `json:"base_url"`
	// APIKey nil keeps the stored key; "" clears it.
	APIKey *string `json:"api_key"`
}

// SetTelemetrySettings — PUT /v1/telemetry/settings
func (h *Handler) SetTelemetrySettings(c *fiber.Ctx) error {
	var req setTelemetryRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.telemetrySvc.Update(h.enrichContext(c), req.BaseURL, req.APIKey)
	if errors.Is(err, telemetryapp.ErrInvalidURL) {
		return badRequest(c, err.Error())
	}
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

type testTelemetryRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

// TestTelemetrySettings — POST /v1/telemetry/settings/test
// Always 200: a failed test is the answer, not a failed request.
func (h *Handler) TestTelemetrySettings(c *fiber.Ctx) error {
	var req testTelemetryRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	out, err := h.telemetrySvc.Test(h.enrichContext(c), telemetryapp.TestInput{BaseURL: req.BaseURL, APIKey: req.APIKey})
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

// GetTelemetryStatus — GET /v1/telemetry/status
func (h *Handler) GetTelemetryStatus(c *fiber.Ctx) error {
	out, err := h.telemetrySvc.Status(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

// TelemetryProxy — GET /v1/telemetry/proxy/*
// A read-only passthrough to the telemetry API with the key injected here. The
// wildcard is the upstream path relative to /api/v1/.
func (h *Handler) TelemetryProxy(c *fiber.Ctx) error {
	resp, err := h.telemetrySvc.Proxy(h.enrichContext(c), c.Params("*"), string(c.Request().URI().QueryString()))
	switch {
	case errors.Is(err, telemetryapp.ErrNotConfigured):
		return telemetryCoded(c, fiber.StatusConflict, telemetryNotConfiguredCode, "TaskTrooper Telemetry is not configured")
	case errors.Is(err, telemetryapp.ErrPathNotAllowed):
		return telemetryCoded(c, fiber.StatusForbidden, telemetryPathCode, "this telemetry path is not available")
	case errors.Is(err, telemetryapp.ErrUnauthorized):
		return telemetryCoded(c, fiber.StatusBadGateway, telemetryUnauthorizedCode, "the telemetry API rejected the saved key")
	case errors.Is(err, port.ErrTelemetryUnreachable):
		log.Warn().Err(err).Msg("telemetry proxy: upstream unreachable")
		return telemetryCoded(c, fiber.StatusBadGateway, telemetryUnreachableCode, "the telemetry API could not be reached")
	case err != nil:
		return internalError(c, err)
	}
	if resp.ContentType != "" {
		c.Set(fiber.HeaderContentType, resp.ContentType)
	}
	if resp.Truncated {
		c.Set("X-Telemetry-Truncated", "true")
	}
	return c.Status(resp.Status).Send(resp.Body)
}
