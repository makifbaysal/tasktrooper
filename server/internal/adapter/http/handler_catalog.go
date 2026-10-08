package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
)

func (h *Handler) registerCatalogRoutes(app *fiber.App) {
	app.Post("/v1/catalog/sync", h.SyncExternalCatalog)
	app.Get("/v1/catalog/status", h.ExternalCatalogStatus)
	app.Get("/v1/catalog/pending", h.ListCatalogPending)
	app.Delete("/v1/catalog/pending/:id", h.DeleteCatalogPending)
	app.Post("/v1/catalog/pending/:id/apply", h.ApplyCatalogPending)
}

func (h *Handler) catalogConfigured() bool {
	return h.catalogSvc != nil && h.catalogRepo != nil && h.catalogSyncStore != nil
}

func (h *Handler) SyncExternalCatalog(c *fiber.Ctx) error {
	if !h.catalogConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "external catalog not configured (set AGENT_CATALOG_REPO)", Type: "service_unavailable"},
		})
	}
	res, started, err := h.catalogSvc.TrySyncFromCatalog(h.enrichContext(c), h.catalogRepo, h.catalogSyncStore)
	if !started {
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"running": true, "progress": h.catalogSvc.SyncProgress()})
	}
	if err != nil {
		// A sync that failed is still reported through the state row; the
		// in-flight response carries the same sentence the status endpoint will.
		state, _ := h.catalogSyncStore.GetCatalogSyncState(h.enrichContext(c))
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error(), "state": state})
	}
	state, _ := h.catalogSyncStore.GetCatalogSyncState(h.enrichContext(c))
	return c.JSON(fiber.Map{"result": res, "state": state})
}

func (h *Handler) ExternalCatalogStatus(c *fiber.Ctx) error {
	if !h.catalogConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "external catalog not configured (set AGENT_CATALOG_REPO)", Type: "service_unavailable"},
		})
	}
	state, err := h.catalogSyncStore.GetCatalogSyncState(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"configured": true, "state": state, "progress": h.catalogSvc.SyncProgress()})
}

func (h *Handler) ListCatalogPending(c *fiber.Ctx) error {
	if !h.catalogConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "external catalog not configured (set AGENT_CATALOG_REPO)", Type: "service_unavailable"},
		})
	}
	items, err := h.catalogSyncStore.ListCatalogPending(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"items": items, "count": len(items)})
}

func (h *Handler) DeleteCatalogPending(c *fiber.Ctx) error {
	if !h.catalogConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "external catalog not configured (set AGENT_CATALOG_REPO)", Type: "service_unavailable"},
		})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid pending id")
	}
	if err := h.catalogSyncStore.DeleteCatalogPending(h.enrichContext(c), id); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ApplyCatalogPending(c *fiber.Ctx) error {
	if !h.catalogConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "external catalog not configured (set AGENT_CATALOG_REPO)", Type: "service_unavailable"},
		})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid pending id")
	}
	if err := h.catalogSvc.ApplyCatalogPending(h.enrichContext(c), h.catalogRepo, h.catalogSyncStore, id); err != nil {
		if errors.Is(err, catalog.ErrCatalogPendingNotFound) {
			return notFound(c, err.Error())
		}
		return catalogError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
