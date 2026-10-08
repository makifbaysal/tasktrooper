package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerMCPRoutes(app *fiber.App) {
	app.Get("/v1/mcp/servers", h.ListMCPServers)
	app.Get("/admin/mcp-servers", h.ListMCPServers)
	app.Post("/admin/mcp-servers", h.CreateMCPServer)
	app.Put("/admin/mcp-servers/:id", h.UpdateMCPServer)
	app.Delete("/admin/mcp-servers/:id", h.DeleteMCPServer)
	app.Post("/v1/mcp/servers/:id/oauth/start", h.StartMCPOAuth)
	app.Post("/v1/mcp/servers/:id/oauth/disconnect", h.DisconnectMCPOAuth)
	// Public (state-authenticated) — see isPublicPath and MCPOAuthCallbackPath.
	app.Get(MCPOAuthCallbackPath, h.MCPOAuthCallback)
}

func (h *Handler) ListMCPServers(c *fiber.Ctx) error {
	if h.mcpSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "mcp management requires postgres", Type: "service_unavailable"},
		})
	}
	var health []map[string]interface{}
	if h.mcpManager != nil {
		health = h.mcpManager.Health()
	}
	servers, err := h.mcpSvc.ListViews(h.enrichContext(c), health)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(domain.MCPServerListResponse{
		Servers:          servers,
		Count:            len(servers),
		OAuthRedirectURI: h.mcpSvc.OAuthRedirectURI(),
	})
}

func (h *Handler) CreateMCPServer(c *fiber.Ctx) error {
	if h.mcpSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "mcp management requires postgres", Type: "service_unavailable"},
		})
	}
	var req domain.CreateMCPServerRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	server, err := h.mcpSvc.Create(h.enrichContext(c), req)
	if err != nil {
		return mcpHandlerError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(server)
}

func (h *Handler) UpdateMCPServer(c *fiber.Ctx) error {
	if h.mcpSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "mcp management requires postgres", Type: "service_unavailable"},
		})
	}
	id := c.Params("id")
	var req domain.UpdateMCPServerRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	server, err := h.mcpSvc.Update(h.enrichContext(c), id, req)
	if err != nil {
		return mcpHandlerError(c, err)
	}
	return c.JSON(server)
}

func (h *Handler) DeleteMCPServer(c *fiber.Ctx) error {
	if h.mcpSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "mcp management requires postgres", Type: "service_unavailable"},
		})
	}
	id := c.Params("id")
	if err := h.mcpSvc.Delete(h.enrichContext(c), id); err != nil {
		return mcpHandlerError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func mcpHandlerError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrMCPServerNotFound):
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	case errors.Is(err, domain.ErrMCPServerAlreadyExists):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "conflict"},
		})
	case errors.Is(err, domain.ErrMCPInvalidRequest), errors.Is(err, domain.ErrMCPOAuthNotHTTP):
		return badRequest(c, err.Error())
	case errors.Is(err, domain.ErrMCPOAuthClientRequired):
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "oauth_client_required"},
		})
	case errors.Is(err, domain.ErrMCPOAuthDiscovery):
		return c.Status(fiber.StatusBadGateway).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "upstream_error"},
		})
	case errors.Is(err, domain.ErrMCPOAuthUnavailable):
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "service_unavailable"},
		})
	default:
		return internalError(c, err)
	}
}
