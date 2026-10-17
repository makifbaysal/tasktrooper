package http

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/errordef"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsai"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ErrorDefinitionService is errordef.Service.
type ErrorDefinitionService interface {
	List(ctx context.Context, f errordef.ListFilter) ([]domain.ErrorDefinition, error)
	Get(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error)
	Create(ctx context.Context, req errordef.DefinitionRequest) (domain.ErrorDefinition, error)
	Update(ctx context.Context, id uuid.UUID, req errordef.DefinitionRequest) (domain.ErrorDefinition, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Accept(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error)
	Dismiss(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error)
	Annotate(ctx context.Context, in domain.OpsErrors, includeIgnored bool) domain.OpsErrors
}

// OpsAIService is opsai.Service.
type OpsAIService interface {
	SuggestErrors(ctx context.Context, req opsai.SuggestErrorsRequest) (domain.AIJob, error)
	SuggestSignals(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.AIJob, error)
	Job(ctx context.Context, id uuid.UUID) (domain.AIJob, error)
	Enable(ctx context.Context, componentID uuid.UUID, req opsai.EnableRequest) (domain.BoardTask, error)
	Readiness(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ObservabilityReadiness, error)
}

// registerErrorDefinitionRoutes mounts error definitions, the release
// engineer's suggestion jobs and the component observability routes. See
// .ai/api-spec.md ("Error definitions and AI suggestions").
func (h *Handler) registerErrorDefinitionRoutes(app fiber.Router) {
	if h.errorDefsSvc != nil {
		app.Get("/v1/error-definitions", h.ListErrorDefinitions)
		app.Post("/v1/error-definitions", h.CreateErrorDefinition)
		if h.opsAISvc != nil {
			app.Post("/v1/error-definitions/suggest", h.SuggestErrorDefinitions)
		}
		app.Patch("/v1/error-definitions/:definitionId", h.UpdateErrorDefinition)
		app.Delete("/v1/error-definitions/:definitionId", h.DeleteErrorDefinition)
		app.Post("/v1/error-definitions/:definitionId/accept", h.AcceptErrorDefinition)
		app.Post("/v1/error-definitions/:definitionId/dismiss", h.DismissErrorDefinition)
	}
	if h.opsAISvc != nil {
		app.Get("/v1/ai-jobs/:jobId", h.GetAIJob)
		app.Post("/v1/components/:componentId/signals/ai-suggest", h.SuggestComponentSignalsAI)
		app.Get("/v1/components/:componentId/observability", h.ComponentObservability)
		app.Post("/v1/components/:componentId/observability/enable", h.EnableComponentObservability)
	}
}

// errorDefErr: field errors in the data source routes' shape, plus the
// suggestion and release-engineer conflicts.
func errorDefErr(c *fiber.Ctx, err error) error {
	var fe errordef.FieldErrors
	var ofe opsai.FieldErrors
	switch {
	case errors.As(err, &fe):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error(), "fields": fe})
	case errors.As(err, &ofe):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error(), "fields": ofe})
	case errors.Is(err, port.ErrNotFound):
		return cErr(c, fiber.StatusNotFound, err)
	case errors.Is(err, errordef.ErrNotSuggested):
		return cErrCode(c, fiber.StatusConflict, err, "not_suggested")
	case errors.Is(err, errordef.ErrTooManyDefinitions):
		return cErrCode(c, fiber.StatusConflict, err, "too_many_definitions")
	case errors.Is(err, opsai.ErrNoReleaseEngineer):
		return cErrCode(c, fiber.StatusConflict, err, "no_release_engineer")
	case errors.Is(err, opsai.ErrUnavailable):
		return cErrCode(c, fiber.StatusServiceUnavailable, err, "unavailable")
	default:
		return cErr(c, fiber.StatusInternalServerError, err)
	}
}

func optionalUUIDQuery(c *fiber.Ctx, key string) (*uuid.UUID, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid " + key)
	}
	return &id, nil
}

// ListErrorDefinitions — GET /v1/error-definitions?repository_id=&component_id=&status=
// component_id answers that component's definitions and the repository-wide ones.
func (h *Handler) ListErrorDefinitions(c *fiber.Ctx) error {
	repo, err := optionalUUIDQuery(c, "repository_id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	comp, err := optionalUUIDQuery(c, "component_id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	out, err := h.errorDefsSvc.List(h.enrichContext(c), errordef.ListFilter{
		RepositoryID: repo,
		ComponentID:  comp,
		Status:       domain.ErrorDefinitionStatus(strings.TrimSpace(c.Query("status"))),
	})
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.JSON(fiber.Map{"definitions": out})
}

// CreateErrorDefinition — POST /v1/error-definitions → 201, source human, status active.
func (h *Handler) CreateErrorDefinition(c *fiber.Ctx) error {
	var req errordef.DefinitionRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	out, err := h.errorDefsSvc.Create(h.enrichContext(c), req)
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

func (h *Handler) definitionID(c *fiber.Ctx) (uuid.UUID, bool) {
	id, err := parseUUIDParam(c, "definitionId")
	return id, err == nil
}

// UpdateErrorDefinition — PATCH /v1/error-definitions/:definitionId
func (h *Handler) UpdateErrorDefinition(c *fiber.Ctx) error {
	id, ok := h.definitionID(c)
	if !ok {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid error definition id")
	}
	var req errordef.DefinitionRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	out, err := h.errorDefsSvc.Update(h.enrichContext(c), id, req)
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.JSON(out)
}

// DeleteErrorDefinition — DELETE /v1/error-definitions/:definitionId → 204
func (h *Handler) DeleteErrorDefinition(c *fiber.Ctx) error {
	id, ok := h.definitionID(c)
	if !ok {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid error definition id")
	}
	if err := h.errorDefsSvc.Delete(h.enrichContext(c), id); err != nil {
		return errorDefErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// AcceptErrorDefinition — POST /v1/error-definitions/:definitionId/accept
func (h *Handler) AcceptErrorDefinition(c *fiber.Ctx) error {
	id, ok := h.definitionID(c)
	if !ok {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid error definition id")
	}
	out, err := h.errorDefsSvc.Accept(h.enrichContext(c), id)
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.JSON(out)
}

// DismissErrorDefinition — POST /v1/error-definitions/:definitionId/dismiss
func (h *Handler) DismissErrorDefinition(c *fiber.Ctx) error {
	id, ok := h.definitionID(c)
	if !ok {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid error definition id")
	}
	out, err := h.errorDefsSvc.Dismiss(h.enrichContext(c), id)
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.JSON(out)
}

// SuggestErrorDefinitions — POST /v1/error-definitions/suggest
// {repository_id, component_id?, environment?, mode?: errors|code} → 202 {job_id, job}
func (h *Handler) SuggestErrorDefinitions(c *fiber.Ctx) error {
	var req opsai.SuggestErrorsRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	job, err := h.opsAISvc.SuggestErrors(h.enrichContext(c), req)
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"job_id": job.ID, "job": job})
}

// GetAIJob — GET /v1/ai-jobs/:jobId
func (h *Handler) GetAIJob(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "jobId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid job id")
	}
	job, err := h.opsAISvc.Job(h.enrichContext(c), id)
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.JSON(job)
}

// SuggestComponentSignalsAI — POST /v1/components/:componentId/signals/ai-suggest?env= → 202 {job_id, job}
func (h *Handler) SuggestComponentSignalsAI(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	job, err := h.opsAISvc.SuggestSignals(h.enrichContext(c), id, opsEnvironment(c))
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"job_id": job.ID, "job": job})
}

// ComponentObservability — GET /v1/components/:componentId/observability?env=
func (h *Handler) ComponentObservability(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	out, err := h.opsAISvc.Readiness(h.enrichContext(c), id, opsEnvironment(c))
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.JSON(out)
}

// EnableComponentObservability — POST /v1/components/:componentId/observability/enable
// {environment, goals, target, otlp_endpoint?} → 201 {task_id, task_key}
func (h *Handler) EnableComponentObservability(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	var req opsai.EnableRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	task, err := h.opsAISvc.Enable(h.enrichContext(c), id, req)
	if err != nil {
		return errorDefErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"task_id": task.ID, "task_key": task.Key})
}
