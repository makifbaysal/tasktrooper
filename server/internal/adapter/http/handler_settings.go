package http

import (
	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerSettingsRoutes(app *fiber.App) {
	app.Get("/v1/settings", h.GetSettings)
	app.Put("/v1/settings", h.UpdateSettings)
	app.Put("/v1/settings/analiz-assignment", h.UpdateAnalizAssignment)
	app.Get("/v1/settings/github", h.GitHubStatus)
	app.Put("/v1/settings/github", h.SetGitHubToken)
	app.Delete("/v1/settings/github", h.DeleteGitHubToken)
	app.Post("/v1/settings/github/device", h.StartGitHubDeviceFlow)
	app.Get("/v1/settings/github/device/:id", h.PollGitHubDeviceFlow)
	app.Get("/v1/settings/github/owners", h.GitHubOwners)
	app.Get("/v1/settings/github/repos", h.GitHubOwnerRepos)
}

func (h *Handler) GetSettings(c *fiber.Ctx) error {
	if h.settingsSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "settings not enabled", Type: "service_unavailable"},
		})
	}
	out, err := h.settingsSvc.Get(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) UpdateSettings(c *fiber.Ctx) error {
	if h.settingsSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "settings not enabled", Type: "service_unavailable"},
		})
	}
	var req domain.UpdateSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.WorkspaceRoot == "" && req.DefaultLanguage == "" && req.PipelineContainerRuntime == "" && req.BoilerplateCatalogRepo == "" &&
		req.MaxConcurrentAgents == nil && req.MaxConcurrentTasks == nil {
		return badRequest(c, "at least one settings field is required")
	}
	out, err := h.settingsSvc.Update(h.enrichContext(c), req)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

// UpdateAnalizAssignment is gone: which agent an analiz task is assigned to
// is now the "analyst" role's job, configured at /v1/roles rather than a
// backend/frontend/mobile app_settings triple. The route stays registered so
// an old client gets a clear redirect instead of a generic 404.
func (h *Handler) UpdateAnalizAssignment(c *fiber.Ctx) error {
	return c.Status(fiber.StatusGone).JSON(errorResponse{
		Error: errorDetail{
			Message: "analiz assignment moved to roles: configure the \"analyst\" role's agent assignments at /v1/roles instead.",
			Type:    "gone",
		},
	})
}
