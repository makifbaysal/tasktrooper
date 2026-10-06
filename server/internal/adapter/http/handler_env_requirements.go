package http

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/envreq"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// EnvRequirementService is *envreq.Service.
type EnvRequirementService interface {
	Status(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvStatus, error)
	Apply(ctx context.Context, repositoryID, componentID uuid.UUID, inputs []envreq.EnvInput) (domain.ComponentEnvStatus, error)
	Redeploy(ctx context.Context, repositoryID, componentID uuid.UUID) (domain.CloudDeployment, error)
}

func (h *Handler) registerEnvRequirementRoutes(app fiber.Router) {
	if h.envRequirements == nil {
		return
	}
	app.Get("/v1/repositories/:id/env-requirements", h.ListEnvRequirements)
	app.Post("/v1/repositories/:id/env-requirements/apply", h.ApplyEnvRequirements)
	app.Post("/v1/repositories/:id/env-requirements/redeploy", h.RedeployForEnvRequirements)
}

func envReqErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrEnvRequirementInvalid):
		return cErr(c, fiber.StatusBadRequest, err)
	case errors.Is(err, port.ErrCloudWriteDenied):
		return cErrCode(c, fiber.StatusForbidden, err, "cloud_write_denied")
	default:
		return cloudErr(c, err)
	}
}

// ListEnvRequirements — GET /v1/repositories/:id/env-requirements
// Each env-managed production target of the repository: what its component
// requires and whether the provider has it. Never a secret value.
func (h *Handler) ListEnvRequirements(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	targets, err := h.envRequirements.Status(h.enrichContext(c), id)
	if err != nil {
		return envReqErr(c, err)
	}
	return c.JSON(fiber.Map{"targets": targets})
}

type applyEnvRequest struct {
	ComponentID uuid.UUID         `json:"component_id"`
	Vars        []envreq.EnvInput `json:"vars"`
}

// ApplyEnvRequirements — POST /v1/repositories/:id/env-requirements/apply
// Body: {component_id, vars: [{name, kind?, value?}]}. A secret value goes
// straight to the provider and is neither stored nor echoed; an empty vars
// list just re-checks (and fills what TaskTrooper can itself).
func (h *Handler) ApplyEnvRequirements(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	var req applyEnvRequest
	if err := c.BodyParser(&req); err != nil || req.ComponentID == uuid.Nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body: component_id is required")
	}
	status, err := h.envRequirements.Apply(h.enrichContext(c), id, req.ComponentID, req.Vars)
	if err != nil {
		return envReqErr(c, err)
	}
	return c.JSON(status)
}

type redeployEnvRequest struct {
	ComponentID uuid.UUID `json:"component_id"`
}

// RedeployForEnvRequirements — POST /v1/repositories/:id/env-requirements/redeploy
// Body: {component_id}. Rebuilds production so variables written since the
// last deploy reach the running site.
func (h *Handler) RedeployForEnvRequirements(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	var req redeployEnvRequest
	if err := c.BodyParser(&req); err != nil || req.ComponentID == uuid.Nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body: component_id is required")
	}
	d, err := h.envRequirements.Redeploy(h.enrichContext(c), id, req.ComponentID)
	if err != nil {
		return envReqErr(c, err)
	}
	return c.JSON(d)
}
