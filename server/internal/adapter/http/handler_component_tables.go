package http

import (
	"github.com/gofiber/fiber/v2"
)

// ComponentEndpoints — GET /v1/components/:componentId/endpoints?env=
func (h *Handler) ComponentEndpoints(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	out, err := h.signalsSvc.Endpoints(h.enrichContext(c), id, opsEnvironment(c))
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

// ComponentWeb — GET /v1/components/:componentId/web?env=&table=pages|referrers|countries|devices
// (table defaults to pages).
func (h *Handler) ComponentWeb(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	out, err := h.signalsSvc.Web(h.enrichContext(c), id, opsEnvironment(c), c.Query("table", "pages"))
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}

// ComponentCrashes — GET /v1/components/:componentId/crashes?env=
func (h *Handler) ComponentCrashes(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	out, err := h.signalsSvc.Crashes(h.enrichContext(c), id, opsEnvironment(c))
	if err != nil {
		return signalsErr(c, err)
	}
	return c.JSON(out)
}
