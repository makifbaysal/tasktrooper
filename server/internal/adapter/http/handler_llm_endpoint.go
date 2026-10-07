package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerLLMEndpointRoutes(app *fiber.App) {
	app.Post("/v1/llm/endpoints", h.CreateLLMEndpoint)
	app.Put("/v1/llm/endpoints/:id", h.UpdateLLMEndpoint)
	app.Delete("/v1/llm/endpoints/:id", h.DeleteLLMEndpoint)
	app.Post("/v1/llm/endpoints/:id/test", h.TestLLMEndpoint)
	app.Post("/v1/llm/endpoints/:id/activate", h.ActivateLLMEndpoint)
}

func (h *Handler) llmProviderUnavailable(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
	})
}

func (h *Handler) CreateLLMEndpoint(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return h.llmProviderUnavailable(c)
	}
	var req domain.SaveLLMEndpointRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.llmProviderSvc.CreateEndpoint(h.enrichContext(c), req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	h.invalidateProviderHealth()
	return c.JSON(out)
}

func (h *Handler) UpdateLLMEndpoint(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return h.llmProviderUnavailable(c)
	}
	id := c.Params("id")
	var req domain.SaveLLMEndpointRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.llmProviderSvc.UpdateEndpoint(h.enrichContext(c), id, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	h.invalidateProviderHealth()
	return c.JSON(out)
}

func (h *Handler) DeleteLLMEndpoint(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return h.llmProviderUnavailable(c)
	}
	out, err := h.llmProviderSvc.DeleteEndpoint(h.enrichContext(c), c.Params("id"))
	if err != nil {
		return internalError(c, err)
	}
	h.invalidateProviderHealth()
	return c.JSON(out)
}

func (h *Handler) TestLLMEndpoint(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return h.llmProviderUnavailable(c)
	}
	id := c.Params("id")
	if id == "new" {
		id = "" // test a not-yet-created endpoint from the add form
	}
	var req domain.SaveLLMEndpointRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.llmProviderSvc.TestEndpoint(h.enrichContext(c), id, req); err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) ActivateLLMEndpoint(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return h.llmProviderUnavailable(c)
	}
	out, err := h.llmProviderSvc.Activate(h.enrichContext(c), domain.LLMProviderType(c.Params("id")))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}
