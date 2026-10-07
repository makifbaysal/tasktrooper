package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerLLMProviderRoutes(app *fiber.App) {
	app.Get("/v1/llm/providers", h.ListLLMProviders)
	app.Post("/v1/llm/providers/:type/connect", h.ConnectLLMProvider)
	app.Post("/v1/llm/providers/:type/activate", h.ActivateLLMProvider)
	app.Put("/v1/llm/embedding-provider", h.SetEmbeddingProvider)
	app.Get("/v1/llm/embedding-models", h.ListEmbeddingModels)
	app.Get("/v1/llm/embedding-status", h.EmbeddingStatus)
	app.Post("/v1/llm/reindex-all", h.ReindexAllEmbeddings)
	app.Post("/v1/llm/providers/:type/test", h.TestLLMProvider)
	app.Delete("/v1/llm/providers/:type", h.DisconnectLLMProvider)
}

func (h *Handler) ListLLMProviders(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
		})
	}
	out, err := h.llmProviderSvc.List(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) ConnectLLMProvider(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
		})
	}
	providerType := domain.LLMProviderType(c.Params("type"))
	if !domain.ValidLLMProviderType(string(providerType)) {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: "invalid provider type", Type: "invalid_request"},
		})
	}
	var req domain.ConnectLLMProviderRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: "invalid request body", Type: "invalid_request"},
		})
	}
	out, err := h.llmProviderSvc.Connect(h.enrichContext(c), providerType, req)
	if err != nil {
		return internalError(c, err)
	}
	h.invalidateProviderHealth()
	return c.JSON(out)
}

func (h *Handler) ActivateLLMProvider(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
		})
	}
	providerType := domain.LLMProviderType(c.Params("type"))
	if !domain.ValidLLMProviderType(string(providerType)) {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: "invalid provider type", Type: "invalid_request"},
		})
	}
	out, err := h.llmProviderSvc.Activate(h.enrichContext(c), providerType)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) SetEmbeddingProvider(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
		})
	}
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.llmProviderSvc.SetEmbedding(h.enrichContext(c), domain.LLMProviderType(req.Provider), req.Model)
	if err != nil {
		return badRequestErr(c, err)
	}
	return c.JSON(out)
}

// ReindexAllEmbeddings re-embeds every code repository (used after changing the
// embedding model/provider, which invalidates existing vectors).
func (h *Handler) ReindexAllEmbeddings(c *fiber.Ctx) error {
	if h.repositorySvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "repositories not enabled", Type: "service_unavailable"},
		})
	}
	ctx := h.enrichContext(c)
	repos, err := h.repositorySvc.List(ctx)
	if err != nil {
		return internalError(c, err)
	}
	count := 0
	for _, repo := range repos {
		if err := h.repositorySvc.Reindex(ctx, repo.ID); err != nil {
			continue
		}
		count++
	}
	return c.JSON(fiber.Map{"reindexed_repositories": count})
}

func (h *Handler) ListEmbeddingModels(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
		})
	}
	// A named endpoint is referenced by its uuid, so anything non-empty is a
	// valid ref here — the service resolves natives and endpoints alike.
	providerType := domain.LLMProviderType(c.Query("provider"))
	if providerType == "" {
		return badRequest(c, "provider is required")
	}
	models, err := h.llmProviderSvc.ListEmbeddingModels(h.enrichContext(c), providerType)
	if err != nil {
		// An exhausted quota, a rejected key and an unreachable host all end up
		// here; the kind travels alongside the message so the UI can name the one
		// that happened instead of showing the same generic warning for all three.
		var provErr *llmprovider.ProviderError
		if errors.As(err, &provErr) && provErr.Kind != llmprovider.ProviderErrorUnknown {
			return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
				Error: errorDetail{Message: err.Error(), Type: string(provErr.Kind)},
			})
		}
		return badRequest(c, err.Error())
	}
	if models == nil {
		models = []string{}
	}
	return c.JSON(fiber.Map{"models": models})
}

// EmbeddingStatus answers "what is producing these embeddings, and can it do
// it right now".
//
// A separate call from ListLLMProviders on purpose. When embeddings resolve to
// a member's Mac this reaches that Mac through the tunnel, and folding a
// laptop round trip into the settings page's main load would make the whole
// page wait on somebody's Wi-Fi. Here it is one independent request the client
// can render late, or not at all.
//
// It never fails because the Mac is unreachable — that is one of the states it
// reports. An error from here means the stored settings could not be read,
// which is a 500 and not a fact about anybody's laptop.
func (h *Handler) EmbeddingStatus(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
		})
	}
	out, err := h.llmProviderSvc.EmbeddingStatus(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) TestLLMProvider(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
		})
	}
	providerType := domain.LLMProviderType(c.Params("type"))
	if !domain.ValidLLMProviderType(string(providerType)) {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: "invalid provider type", Type: "invalid_request"},
		})
	}
	var req domain.TestLLMProviderRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: "invalid request body", Type: "invalid_request"},
		})
	}
	if err := h.llmProviderSvc.Test(h.enrichContext(c), providerType, req); err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *Handler) DisconnectLLMProvider(c *fiber.Ctx) error {
	if h.llmProviderSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "llm provider management not enabled", Type: "service_unavailable"},
		})
	}
	providerType := domain.LLMProviderType(c.Params("type"))
	if !domain.ValidLLMProviderType(string(providerType)) {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: "invalid provider type", Type: "invalid_request"},
		})
	}
	out, err := h.llmProviderSvc.Disconnect(h.enrichContext(c), providerType)
	if err != nil {
		return internalError(c, err)
	}
	h.invalidateProviderHealth()
	return c.JSON(out)
}
