package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/issuesync"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (h *Handler) registerIssueSyncRoutes(app fiber.Router) {
	if h.issueSyncSvc == nil {
		return
	}
	app.Get("/v1/issues/search", h.SearchIssues)
	app.Post("/v1/issues/import", h.ImportIssue)
	app.Post("/v1/issues/imports/:id/convert", h.ConvertIssueImport)
	app.Get("/v1/repositories/:id/tasks/:taskId/issue-link", h.GetTaskIssueLink)
	app.Get("/v1/settings/jira", h.GetJiraSettings)
	app.Put("/v1/settings/jira", h.SetJiraSettings)
	app.Delete("/v1/settings/jira", h.DeleteJiraSettings)
	app.Get("/v1/settings/jira/projects", h.ListJiraProjects)
	app.Get("/v1/settings/issue-sync", h.GetIssueSyncSettings)
	app.Put("/v1/settings/issue-sync", h.SetIssueSyncSettings)
}

// SearchIssues — GET /v1/issues/search
func (h *Handler) SearchIssues(c *fiber.Ctx) error {
	provider := domain.IssueProvider(c.Query("provider"))
	if provider != domain.IssueProviderGitHub && provider != domain.IssueProviderJira {
		return badRequest(c, "provider must be \"github\" or \"jira\"")
	}
	var repositoryID *uuid.UUID
	if raw := c.Query("repository_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid repository_id")
		}
		repositoryID = &id
	}
	issues, err := h.issueSyncSvc.Search(h.enrichContext(c), provider, repositoryID, c.Query("project"), c.Query("q"))
	if err != nil {
		return issueSyncError(c, err)
	}
	if issues == nil {
		issues = []domain.ExternalIssue{}
	}
	return c.JSON(fiber.Map{"issues": issues})
}

type importIssueRequest struct {
	Provider     domain.IssueProvider `json:"provider"`
	Key          string               `json:"key"`
	RepositoryID uuid.UUID            `json:"repository_id"`
}

// ImportIssue — POST /v1/issues/import
func (h *Handler) ImportIssue(c *fiber.Ctx) error {
	var req importIssueRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ctx := h.enrichContext(c)
	actor := registry.ActorUserIDFromContext(ctx)
	createdBy := actor
	if createdBy == "" {
		createdBy = "user"
	}
	out, err := h.issueSyncSvc.Import(ctx, req.Provider, req.Key, req.RepositoryID, createdBy, actor)
	if err != nil {
		return issueSyncError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

// ConvertIssueImport — POST /v1/issues/imports/:id/convert
func (h *Handler) ConvertIssueImport(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid import id")
	}
	imp, err := h.issueSyncSvc.RetryConversion(h.enrichContext(c), id)
	if err != nil {
		return issueSyncError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"import": imp})
}

// GetTaskIssueLink — GET /v1/repositories/:id/tasks/:taskId/issue-link
func (h *Handler) GetTaskIssueLink(c *fiber.Ctx) error {
	taskID, err := uuid.Parse(c.Params("taskId"))
	if err != nil {
		return badRequest(c, "invalid task id")
	}
	link, imp, err := h.issueSyncSvc.IssueForTask(h.enrichContext(c), taskID)
	if err != nil {
		if errors.Is(err, issuesync.ErrNoLink) {
			return c.JSON(fiber.Map{"link": nil, "import": nil})
		}
		return internalError(c, err)
	}
	var importBody any
	if imp.ID != uuid.Nil {
		importBody = imp
	}
	return c.JSON(fiber.Map{"link": link, "import": importBody})
}

// GetJiraSettings — GET /v1/settings/jira
func (h *Handler) GetJiraSettings(c *fiber.Ctx) error {
	status, err := h.issueSyncSvc.GetJiraStatus(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(status)
}

type setJiraRequest struct {
	SiteURL  string `json:"site_url"`
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
}

// SetJiraSettings — PUT /v1/settings/jira
func (h *Handler) SetJiraSettings(c *fiber.Ctx) error {
	var req setJiraRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	status, err := h.issueSyncSvc.SetJira(h.enrichContext(c), req.SiteURL, req.Email, req.APIToken)
	if err != nil {
		return issueSyncError(c, err)
	}
	return c.JSON(status)
}

// DeleteJiraSettings — DELETE /v1/settings/jira
func (h *Handler) DeleteJiraSettings(c *fiber.Ctx) error {
	if err := h.issueSyncSvc.DeleteJira(h.enrichContext(c)); err != nil {
		return internalError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListJiraProjects — GET /v1/settings/jira/projects
func (h *Handler) ListJiraProjects(c *fiber.Ctx) error {
	projects, err := h.issueSyncSvc.ListJiraProjects(h.enrichContext(c))
	if err != nil {
		return issueSyncError(c, err)
	}
	if projects == nil {
		projects = []domain.JiraProjectRef{}
	}
	return c.JSON(fiber.Map{"projects": projects})
}

// GetIssueSyncSettings — GET /v1/settings/issue-sync
func (h *Handler) GetIssueSyncSettings(c *fiber.Ctx) error {
	settings, err := h.issueSyncSvc.GetIssueSync(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(settings)
}

// SetIssueSyncSettings — PUT /v1/settings/issue-sync
func (h *Handler) SetIssueSyncSettings(c *fiber.Ctx) error {
	var req domain.IssueSyncSettings
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	settings, err := h.issueSyncSvc.SetIssueSync(h.enrichContext(c), req)
	if err != nil {
		return issueSyncError(c, err)
	}
	return c.JSON(settings)
}

// issueSyncError checks AlreadyImportedError first: it wraps nothing, so it
// is always the most specific match.
func issueSyncError(c *fiber.Ctx, err error) error {
	var already *issuesync.AlreadyImportedError
	if errors.As(err, &already) {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error":         errorDetail{Message: err.Error(), Type: "issue_already_imported"},
			"task_id":       already.TaskID,
			"task_key":      already.TaskKey,
			"repository_id": already.RepositoryID,
		})
	}
	switch {
	case errors.Is(err, issuesync.ErrSourceNotConfigured):
		return typedBadRequestResponse(c, "issue_source_not_configured", err.Error())
	case errors.Is(err, issuesync.ErrInvalidJiraSite):
		return typedBadRequestResponse(c, "invalid_jira_site", err.Error())
	case errors.Is(err, issuesync.ErrJiraAuthFailed):
		return typedBadRequestResponse(c, "jira_auth_failed", err.Error())
	case errors.Is(err, issuesync.ErrInvalidIssue):
		return typedBadRequestResponse(c, "invalid_issue", err.Error())
	case errors.Is(err, issuesync.ErrInvalidSettings):
		return badRequest(c, err.Error())
	case errors.Is(err, issuesync.ErrConversionUnavailable), errors.Is(err, issuesync.ErrConversionNotRetryable):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "issue_conversion_unavailable"},
		})
	case errors.Is(err, port.ErrNotFound):
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	case errors.Is(err, issuesync.ErrUpstream):
		return c.Status(fiber.StatusBadGateway).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "issue_source_error"},
		})
	default:
		return internalError(c, err)
	}
}

func typedBadRequestResponse(c *fiber.Ctx, errType, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
		Error: errorDetail{Message: message, Type: errType},
	})
}
