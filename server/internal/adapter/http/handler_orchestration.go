package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerOrchestrationRoutes(app *fiber.App) {
	app.Get("/admin/agents", h.ListAgents)
	app.Post("/admin/agents", h.CreateAgent)
	app.Get("/admin/agent-templates", h.ListAgentTemplates)
	app.Post("/admin/agents/:id/template", h.SaveAgentAsTemplate)
	app.Get("/admin/agents/:id", h.GetAgent)
	app.Put("/admin/agents/:id", h.UpdateAgent)
	app.Delete("/admin/agents/:id", h.DeleteAgent)

	app.Get("/admin/agents/:id/skills", h.ListAgentSkills)
	app.Post("/admin/agents/:id/skills", h.CreateAgentSkill)
	app.Get("/admin/agents/:id/skills/:skillId", h.GetAgentSkill)
	app.Put("/admin/agents/:id/skills/:skillId", h.UpdateAgentSkill)
	app.Delete("/admin/agents/:id/skills/:skillId", h.DeleteAgentSkill)
	app.Get("/admin/agents/:id/skills/:skillId/versions", h.ListAgentSkillVersions)
	app.Post("/admin/agents/:id/skills/:skillId/restore", h.RestoreAgentSkillVersion)

	app.Get("/admin/agents/:id/tech-stacks", h.ListAgentTechStacks)
	app.Post("/admin/agents/:id/tech-stacks", h.CreateAgentTechStack)
	app.Put("/admin/agents/:id/tech-stacks/:stackId", h.UpdateAgentTechStack)
	app.Delete("/admin/agents/:id/tech-stacks/:stackId", h.DeleteAgentTechStack)

	app.Get("/admin/agents/:id/rules", h.ListAgentRules)
	app.Post("/admin/agents/:id/rules", h.CreateAgentRule)
	app.Get("/admin/agents/:id/rules/:ruleId", h.GetAgentRule)
	app.Put("/admin/agents/:id/rules/:ruleId", h.UpdateAgentRule)
	app.Delete("/admin/agents/:id/rules/:ruleId", h.DeleteAgentRule)
	app.Get("/admin/agents/:id/rules/:ruleId/versions", h.ListAgentRuleVersions)
	app.Post("/admin/agents/:id/rules/:ruleId/restore", h.RestoreAgentRuleVersion)

	app.Get("/v1/runs/:id/plan", h.GetRunPlan)
}

func (h *Handler) parseAgentID(c *fiber.Ctx) (uuid.UUID, error) {
	return uuid.Parse(c.Params("id"))
}

func (h *Handler) ListAgentSkills(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	skills, err := h.catalogSvc.ListSkillsByAgent(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"skills": skills, "count": len(skills)})
}

func (h *Handler) CreateAgentSkill(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req domain.CreateSkillRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	skill, err := h.catalogSvc.CreateSkillForAgent(h.enrichContext(c), agentID, req)
	if err != nil {
		return catalogError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(skill)
}

func (h *Handler) GetAgentSkill(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	skillID, err := uuid.Parse(c.Params("skillId"))
	if err != nil {
		return badRequest(c, "invalid skill id")
	}
	skill, err := h.catalogSvc.GetSkillForAgent(h.enrichContext(c), agentID, skillID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.JSON(skill)
}

func (h *Handler) UpdateAgentSkill(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	skillID, err := uuid.Parse(c.Params("skillId"))
	if err != nil {
		return badRequest(c, "invalid skill id")
	}
	var req domain.UpdateSkillRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	skill, err := h.catalogSvc.UpdateSkillForAgent(h.enrichContext(c), agentID, skillID, req)
	if err != nil {
		return catalogError(c, err)
	}
	return c.JSON(skill)
}

func (h *Handler) DeleteAgentSkill(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	skillID, err := uuid.Parse(c.Params("skillId"))
	if err != nil {
		return badRequest(c, "invalid skill id")
	}
	if err := h.catalogSvc.DeleteSkillForAgent(h.enrichContext(c), agentID, skillID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListAgentTechStacks(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	stacks, err := h.catalogSvc.ListTechStacksForAgent(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"tech_stacks": stacks, "count": len(stacks)})
}

func (h *Handler) CreateAgentTechStack(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req domain.CreateTechStackRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	stack, err := h.catalogSvc.CreateTechStackForAgent(h.enrichContext(c), agentID, req)
	if err != nil {
		return catalogError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(stack)
}

func (h *Handler) UpdateAgentTechStack(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	stackID, err := uuid.Parse(c.Params("stackId"))
	if err != nil {
		return badRequest(c, "invalid tech stack id")
	}
	var req domain.UpdateTechStackRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	stack, err := h.catalogSvc.UpdateTechStackForAgent(h.enrichContext(c), agentID, stackID, req)
	if err != nil {
		return catalogError(c, err)
	}
	return c.JSON(stack)
}

func (h *Handler) DeleteAgentTechStack(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	stackID, err := uuid.Parse(c.Params("stackId"))
	if err != nil {
		return badRequest(c, "invalid tech stack id")
	}
	if err := h.catalogSvc.DeleteTechStackForAgent(h.enrichContext(c), agentID, stackID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListAgentRules(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	rules, err := h.catalogSvc.ListRulesByAgent(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"orchestrator_rules": rules, "count": len(rules)})
}

func (h *Handler) CreateAgentRule(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req domain.CreateOrchestratorRuleRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	rule, err := h.catalogSvc.CreateRuleForAgent(h.enrichContext(c), agentID, req)
	if err != nil {
		return catalogError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(rule)
}

func (h *Handler) GetAgentRule(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	ruleID, err := uuid.Parse(c.Params("ruleId"))
	if err != nil {
		return badRequest(c, "invalid rule id")
	}
	rule, err := h.catalogSvc.GetRuleForAgent(h.enrichContext(c), agentID, ruleID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.JSON(rule)
}

func (h *Handler) UpdateAgentRule(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	ruleID, err := uuid.Parse(c.Params("ruleId"))
	if err != nil {
		return badRequest(c, "invalid rule id")
	}
	var req domain.UpdateOrchestratorRuleRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	rule, err := h.catalogSvc.UpdateRuleForAgent(h.enrichContext(c), agentID, ruleID, req)
	if err != nil {
		return catalogError(c, err)
	}
	return c.JSON(rule)
}

func (h *Handler) DeleteAgentRule(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agentID, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	ruleID, err := uuid.Parse(c.Params("ruleId"))
	if err != nil {
		return badRequest(c, "invalid rule id")
	}
	if err := h.catalogSvc.DeleteRuleForAgent(h.enrichContext(c), agentID, ruleID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListAgents(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	agents, err := h.catalogSvc.ListAgents(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	// The built-in template upsert is scheduled at boot but may not have
	// started yet; reporting only the catalog's own flag let the first read
	// answer "done, and empty" before boot ever touched the templates table,
	// and the sidebar stopped waiting on work that was never going to happen.
	seeding := h.catalogSvc.SeedingInProgress()
	if !seeding && h.bootSeed != nil && h.bootSeed.Booting() {
		seeding = true
	}
	return c.JSON(fiber.Map{"agents": agents, "count": len(agents), "seeding": seeding})
}

func (h *Handler) CreateAgent(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	var req domain.CreateAgentRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if templateID := c.Query("template_id"); templateID != "" {
		tid, err := uuid.Parse(templateID)
		if err != nil {
			return badRequest(c, "invalid template_id")
		}
		agent, err := h.catalogSvc.CreateAgentFromTemplate(h.enrichContext(c), tid, req)
		if err != nil {
			return catalogError(c, err)
		}
		return c.Status(fiber.StatusCreated).JSON(agent)
	}
	agent, err := h.catalogSvc.CreateAgent(h.enrichContext(c), req)
	if err != nil {
		return catalogError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(agent)
}

func (h *Handler) ListAgentTemplates(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	templates, err := h.catalogSvc.ListAgentTemplates(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"templates": templates, "count": len(templates)})
}

func (h *Handler) SaveAgentAsTemplate(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	id, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	tpl, err := h.catalogSvc.SaveAgentAsTemplate(h.enrichContext(c), id)
	if err != nil {
		return internalError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(tpl)
}

func (h *Handler) GetAgent(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	id, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	agent, err := h.catalogSvc.GetAgent(h.enrichContext(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.JSON(agent)
}

func (h *Handler) UpdateAgent(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	id, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req domain.UpdateAgentRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	agent, err := h.catalogSvc.UpdateAgent(h.enrichContext(c), id, req)
	if err != nil {
		return catalogError(c, err)
	}
	return c.JSON(agent)
}

func (h *Handler) DeleteAgent(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	id, err := h.parseAgentID(c)
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	if err := h.catalogSvc.DeleteAgent(h.enrichContext(c), id); err != nil {
		if errors.Is(err, catalog.ErrInvalidInput) {
			return catalogError(c, err)
		}
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetRunPlan(c *fiber.Ctx) error {
	if h.catalogSvc == nil {
		return catalogDisabled(c)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid run id")
	}
	ctx := h.enrichContext(c)
	// orchestration_plans.run_id is a session_runs.id, exactly like
	// session_steps.run_id — and this endpoint is called with the same id as
	// /v1/runs/:id/steps by the same panel, so it needs the same resolution. See
	// activityRunID. A board run with no session run has no plan either.
	runID, _, ok := h.activityRunID(ctx, id)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: "run has not started", Type: "not_found"},
		})
	}
	plan, err := h.catalogSvc.GetPlanByRunID(ctx, runID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.JSON(plan)
}

func catalogDisabled(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "orchestration catalog not enabled", Type: "service_unavailable"},
	})
}

// catalogError gives every write to the agent catalog the same classification,
// rather than each handler inventing one.
//
// Two of the three answers are not 500s and used to be. A body with no name (or
// no content, or an effort level that is not one of the five) is the caller's to
// fix — `PUT /admin/agents/:id` answered `500 "name is required"`, which reads
// to a client and a monitor as "the server broke, send it again". A provider
// whose engine is not installed on this host is the same kind of correction.
// A provider that was never BUILT is the third answer and belongs to
// permanentRefusal's 409, so it deliberately falls through to internalError.
func catalogError(c *fiber.Ctx, err error) error {
	if errors.Is(err, catalog.ErrInvalidInput) || errors.Is(err, catalog.ErrNoHostRunner) {
		return badRequestErr(c, err)
	}
	return internalError(c, err)
}
