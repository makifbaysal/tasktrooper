package http

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// registerDesignSystemRoutes exposes the project and repository Design System
// tabs: the approved versions and pending proposals, the design task that
// derives them from existing code, and which project's base a repository
// builds on. Approval itself is the design task's analiz_review approval.
func (h *Handler) registerDesignSystemRoutes(app fiber.Router) {
	if h.designSystemSvc == nil {
		return
	}
	app.Get("/v1/projects/:projectId/design-system", h.GetProjectDesignSystem)
	app.Post("/v1/projects/:projectId/design-system/generate", h.GenerateProjectDesignSystem)
	app.Get("/v1/repositories/:id/design-system", h.GetRepositoryDesignSystem)
	app.Post("/v1/repositories/:id/design-system/generate", h.GenerateRepositoryDesignSystem)
	app.Put("/v1/repositories/:id/design-system/base-project", h.SetRepositoryDesignBaseProject)
	app.Get("/v1/design-systems/:designSystemId", h.GetDesignSystemVersion)
	app.Get("/v1/repositories/:id/tasks/:taskId/design", h.GetTaskDesign)
	app.Get("/v1/repositories/:id/design-system/files", h.GetRepositoryDesignSystemFiles)
}

func designSystemError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrDesignSystemNotFound):
		return notFound(c, err.Error())
	case errors.Is(err, domain.ErrDesignSystemInvalid):
		return badRequest(c, err.Error())
	case errors.Is(err, domain.ErrDesignSystemNoRepository):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "conflict"},
		})
	case strings.Contains(err.Error(), "not found"), strings.Contains(err.Error(), "no rows"):
		return notFound(c, err.Error())
	default:
		return internalError(c, err)
	}
}

type designSystemGenerateRequest struct {
	Notes string `json:"notes"`
}

// GetProjectDesignSystem — GET /v1/projects/:projectId/design-system
func (h *Handler) GetProjectDesignSystem(c *fiber.Ctx) error {
	projectID, err := uuid.Parse(c.Params("projectId"))
	if err != nil {
		return badRequest(c, "invalid project id")
	}
	view, err := h.designSystemSvc.ProjectView(h.enrichContext(c), projectID)
	if err != nil {
		return designSystemError(c, err)
	}
	return c.JSON(view)
}

// GenerateProjectDesignSystem — POST /v1/projects/:projectId/design-system/generate
// Body: {"notes": "..."}. 201 with the new design task, 200 with the one
// already open for this project.
func (h *Handler) GenerateProjectDesignSystem(c *fiber.Ctx) error {
	projectID, err := uuid.Parse(c.Params("projectId"))
	if err != nil {
		return badRequest(c, "invalid project id")
	}
	var req designSystemGenerateRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	res, err := h.designSystemSvc.RequestForProject(h.enrichContext(c), projectID, req.Notes)
	if err != nil {
		return designSystemError(c, err)
	}
	status := fiber.StatusOK
	if res.Created {
		status = fiber.StatusCreated
	}
	return c.Status(status).JSON(res)
}

// GetRepositoryDesignSystem — GET /v1/repositories/:id/design-system
func (h *Handler) GetRepositoryDesignSystem(c *fiber.Ctx) error {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	view, err := h.designSystemSvc.RepositoryView(h.enrichContext(c), repositoryID)
	if err != nil {
		return designSystemError(c, err)
	}
	return c.JSON(view)
}

// GenerateRepositoryDesignSystem — POST /v1/repositories/:id/design-system/generate
func (h *Handler) GenerateRepositoryDesignSystem(c *fiber.Ctx) error {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req designSystemGenerateRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	res, err := h.designSystemSvc.RequestForRepository(h.enrichContext(c), repositoryID, req.Notes)
	if err != nil {
		return designSystemError(c, err)
	}
	status := fiber.StatusOK
	if res.Created {
		status = fiber.StatusCreated
	}
	return c.Status(status).JSON(res)
}

// SetRepositoryDesignBaseProject — PUT /v1/repositories/:id/design-system/base-project
// Body: {"project_id": "<uuid>"} or {"project_id": null} to go back to the
// automatic choice.
func (h *Handler) SetRepositoryDesignBaseProject(c *fiber.Ctx) error {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		ProjectID *string `json:"project_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	var projectID *uuid.UUID
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		id, err := uuid.Parse(strings.TrimSpace(*req.ProjectID))
		if err != nil {
			return badRequest(c, "invalid project id")
		}
		projectID = &id
	}
	view, err := h.designSystemSvc.SetRepositoryBaseProject(h.enrichContext(c), repositoryID, projectID)
	if err != nil {
		return designSystemError(c, err)
	}
	return c.JSON(view)
}

// GetDesignSystemVersion — GET /v1/design-systems/:designSystemId
func (h *Handler) GetDesignSystemVersion(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("designSystemId"))
	if err != nil {
		return badRequest(c, "invalid design system id")
	}
	ds, err := h.designSystemSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return designSystemError(c, err)
	}
	return c.JSON(ds)
}

type taskDesignSystemSummary struct {
	ProjectID    string `json:"project_id,omitempty"`
	ProjectName  string `json:"project_name,omitempty"`
	BaseVersion  int    `json:"base_version,omitempty"`
	LayerVersion int    `json:"layer_version,omitempty"`
	Ambiguous    bool   `json:"ambiguous,omitempty"`
}

// GetTaskDesign — GET /v1/repositories/:id/tasks/:taskId/design
// The approved designs a task builds (design tasks it derives from or that
// block it, with their documents) and the design system its repository
// follows — what the task detail shows under the task and what every run on
// it already carries.
func (h *Handler) GetTaskDesign(c *fiber.Ctx) error {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	taskID, err := uuid.Parse(c.Params("taskId"))
	if err != nil {
		return badRequest(c, "invalid task id")
	}
	ctx := h.enrichContext(c)
	if h.repositorySvc == nil {
		return internalError(c, errors.New("board is not available"))
	}
	if _, err := h.repositorySvc.GetTask(ctx, repositoryID, taskID); err != nil {
		return notFound(c, err.Error())
	}
	refs, err := h.repositorySvc.AnalysisReferences(ctx, taskID)
	if err != nil {
		return internalError(c, err)
	}
	designs := []domain.AnalysisReference{}
	for _, ref := range refs {
		if ref.IsDesign() {
			designs = append(designs, ref)
		}
	}
	out := fiber.Map{"references": designs}
	eff, err := h.designSystemSvc.Effective(ctx, repositoryID)
	if err == nil && !eff.Empty() {
		summary := taskDesignSystemSummary{Ambiguous: eff.Ambiguous}
		if eff.Project != nil {
			summary.ProjectID, summary.ProjectName = eff.Project.ID.String(), eff.Project.Name
		}
		if eff.Base != nil {
			summary.BaseVersion = eff.Base.Version
		}
		if eff.Layer != nil {
			summary.LayerVersion = eff.Layer.Version
		}
		out["design_system"] = summary
	}
	return c.JSON(out)
}

// GetRepositoryDesignSystemFiles — GET /v1/repositories/:id/design-system/files
// The files the repository keeps its design system in (DESIGN.md, the token
// tree, CSS variables, the inventory), rendered from the effective version.
func (h *Handler) GetRepositoryDesignSystemFiles(c *fiber.Ctx) error {
	repositoryID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	files, err := h.designSystemSvc.Files(h.enrichContext(c), repositoryID)
	if err != nil {
		return designSystemError(c, err)
	}
	return c.JSON(fiber.Map{"files": files})
}
