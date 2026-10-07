package http

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/evolution"
	"github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerEvolutionRoutes(app fiber.Router) {
	if h.kpiSvc != nil {
		app.Get("/v1/kpi-metrics", h.ListKPIMetrics)
		app.Get("/admin/agents/:id/kpis", h.ListAgentKPIs)
		app.Post("/admin/agents/:id/kpis", h.CreateAgentKPI)
		app.Put("/admin/agents/:id/kpis/:kpiId", h.UpdateAgentKPI)
		app.Delete("/admin/agents/:id/kpis/:kpiId", h.DeleteAgentKPI)
	}
	app.Get("/v1/agents/:agentId/performance", h.GetAgentPerformance)
	app.Get("/v1/agents/:agentId/score-events", h.ListAgentScoreEvents)
	if h.evolutionSvc != nil {
		app.Get("/v1/agents/:agentId/evolution-events", h.ListAgentEvolutionEvents)
		app.Get("/v1/agents/:agentId/reflections", h.ListAgentReflections)
		app.Post("/v1/agents/:agentId/reflect", h.ReflectNow)
	}
	if h.memorySvc != nil {
		app.Get("/v1/agents/:agentId/memories", h.ListAgentMemories)
		app.Post("/v1/agents/:agentId/memories", h.CreateAgentMemory)
		app.Put("/v1/agents/:agentId/memories/:memoryId", h.UpdateAgentMemory)
		app.Delete("/v1/agents/:agentId/memories/:memoryId", h.DeleteAgentMemory)
		app.Get("/v1/memories/shared", h.ListSharedMemories)
		app.Post("/v1/memories/shared", h.CreateSharedMemory)
		app.Put("/v1/memories/shared/:memoryId", h.UpdateSharedMemory)
		app.Delete("/v1/memories/shared/:memoryId", h.DeleteSharedMemory)
		if h.evolutionSvc != nil {
			app.Post("/v1/memories/shared/promote/plan", h.PlanSharedMemoryPromotion)
			app.Post("/v1/memories/shared/promote", h.PromoteSharedMemories)
		}
	}
	if h.goldenStore != nil {
		app.Get("/admin/agents/:id/golden-tasks", h.ListGoldenTasks)
		app.Post("/admin/agents/:id/golden-tasks", h.CreateGoldenTask)
		app.Put("/admin/agents/:id/golden-tasks/:goldenId", h.UpdateGoldenTask)
		app.Delete("/admin/agents/:id/golden-tasks/:goldenId", h.DeleteGoldenTask)
		app.Get("/admin/agents/:id/golden-results", h.ListGoldenResults)
	}
}

func (h *Handler) ListGoldenTasks(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	tasks, err := h.goldenStore.ListByAgent(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	if tasks == nil {
		tasks = []domain.GoldenTask{}
	}
	return c.JSON(fiber.Map{"golden_tasks": tasks})
}

func (h *Handler) CreateGoldenTask(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req domain.CreateGoldenTaskRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Name == "" || req.Prompt == "" {
		return badRequest(c, "name and prompt are required")
	}
	created, err := h.goldenStore.CreateTask(h.enrichContext(c), domain.GoldenTask{
		AgentID: agentID, Name: req.Name, Prompt: req.Prompt, Expected: req.Expected, Enabled: req.Enabled,
	})
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(created)
}

func (h *Handler) UpdateGoldenTask(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	goldenID, err := uuid.Parse(c.Params("goldenId"))
	if err != nil {
		return badRequest(c, "invalid golden task id")
	}
	var req domain.CreateGoldenTaskRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	updated, err := h.goldenStore.UpdateTask(h.enrichContext(c), domain.GoldenTask{
		ID: goldenID, AgentID: agentID, Name: req.Name, Prompt: req.Prompt, Expected: req.Expected, Enabled: req.Enabled,
	})
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(updated)
}

func (h *Handler) DeleteGoldenTask(c *fiber.Ctx) error {
	goldenID, err := uuid.Parse(c.Params("goldenId"))
	if err != nil {
		return badRequest(c, "invalid golden task id")
	}
	if err := h.goldenStore.DeleteTask(h.enrichContext(c), goldenID); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListGoldenResults(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	results, err := h.goldenStore.ListResults(h.enrichContext(c), agentID, c.QueryInt("limit", 50))
	if err != nil {
		return internalError(c, err)
	}
	if results == nil {
		results = []domain.GoldenResult{}
	}
	return c.JSON(fiber.Map{"results": results})
}

func agentParam(c *fiber.Ctx) (uuid.UUID, error) {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return uuid.Nil, errors.New("invalid agent id")
	}
	return agentID, nil
}

const kpiRefreshMaxAge = time.Minute

func (h *Handler) GetAgentPerformance(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	ctx := h.enrichContext(c)
	resp := fiber.Map{}
	if h.perfStore != nil {
		score, err := h.perfStore.GetScore(ctx, agentID)
		if err != nil {
			return internalError(c, err)
		}
		events, _ := h.perfStore.RecentEvents(ctx, agentID, 100)
		resp["score"] = score
		resp["events"] = orEmptyScoreEvents(events)
	}
	if h.kpiSvc != nil {
		// Evaluating resolves every metric and upserts its result, so a page that
		// polls reuses the stored results until they are kpiRefreshMaxAge old.
		if err := h.kpiSvc.RefreshAgent(ctx, agentID, time.Now(), kpiRefreshMaxAge); err == nil {
			kpis, _ := h.kpiSvc.ListKPIs(ctx, agentID)
			results, _ := h.kpiSvc.LatestResults(ctx, agentID)
			resp["kpis"] = kpis
			resp["kpi_results"] = results
			resp["kpi_composite"] = kpi.CompositeScore(kpis, results)
		}
	}
	return c.JSON(resp)
}

func (h *Handler) ListAgentScoreEvents(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if h.perfStore == nil {
		return c.JSON(fiber.Map{"events": []domain.AgentScoreEvent{}})
	}
	limit := c.QueryInt("limit", 100)
	events, err := h.perfStore.RecentEvents(h.enrichContext(c), agentID, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"events": orEmptyScoreEvents(events)})
}

func (h *Handler) ListAgentEvolutionEvents(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	events, err := h.evolutionSvc.ListEvents(h.enrichContext(c), agentID, c.QueryInt("limit", 50))
	if err != nil {
		return internalError(c, err)
	}
	if events == nil {
		events = []domain.AgentEvolutionEvent{}
	}
	return c.JSON(fiber.Map{"events": events})
}

func (h *Handler) ListAgentReflections(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	reflections, err := h.evolutionSvc.ListReflections(h.enrichContext(c), agentID, c.QueryInt("limit", 20))
	if err != nil {
		return internalError(c, err)
	}
	if reflections == nil {
		reflections = []domain.AgentReflection{}
	}
	return c.JSON(fiber.Map{"reflections": reflections})
}

func (h *Handler) ReflectNow(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	reflection, err := h.evolutionSvc.ReflectNow(h.enrichContext(c), agentID)
	if err != nil {
		if errors.Is(err, evolution.ErrReflectionInFlight) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "conflict"}})
		}
		return internalError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(reflection)
}

// memoryRepoScope reads the repository dimension of a memory listing:
// ?repository_id= picks a project, ?repo_scope= says whether to return that
// project's memories, the global ones, or (the default) everything.
func memoryRepoScope(c *fiber.Ctx) (*uuid.UUID, domain.MemoryRepoScope, error) {
	var repoID *uuid.UUID
	if raw := strings.TrimSpace(c.Query("repository_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return nil, "", fmt.Errorf("invalid repository id")
		}
		repoID = &parsed
	}
	switch c.Query("repo_scope", "any") {
	case "project":
		if repoID == nil {
			return nil, "", fmt.Errorf("repo_scope=project requires repository_id")
		}
		return repoID, domain.MemoryRepoScopeProject, nil
	case "global":
		return nil, domain.MemoryRepoScopeGlobal, nil
	case "visible":
		return repoID, domain.MemoryRepoScopeVisible, nil
	default:
		return repoID, domain.MemoryRepoScopeAny, nil
	}
}

// memoryRepositoryBody reads the repository a written memory is bound to.
// Absent or null means the memory is global.
func memoryRepositoryBody(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid repository id")
	}
	return &parsed, nil
}

func (h *Handler) ListAgentMemories(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	repoID, repoScope, err := memoryRepoScope(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	owner := domain.MemoryOwnerAll
	if c.Query("scope", "all") == "agent" {
		owner = domain.MemoryOwnerAgent
	}
	memories, err := h.memorySvc.List(h.enrichContext(c), domain.MemoryQuery{
		AgentID: agentID, Owner: owner,
		RepositoryID: repoID, Repo: repoScope,
		Limit: c.QueryInt("limit", 100),
	})
	if err != nil {
		return internalError(c, err)
	}
	if memories == nil {
		memories = []domain.AgentMemory{}
	}
	return c.JSON(fiber.Map{"memories": memories})
}

func (h *Handler) CreateAgentMemory(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req struct {
		Content      string `json:"content"`
		Category     string `json:"category"`
		RepositoryID string `json:"repository_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	repoID, err := memoryRepositoryBody(req.RepositoryID)
	if err != nil {
		return badRequest(c, err.Error())
	}
	mem, err := h.memorySvc.Save(h.enrichContext(c), agentID, repoID, req.Content, req.Category, domain.MemorySourceUser)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(mem)
}

func (h *Handler) UpdateAgentMemory(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	memoryID, err := uuid.Parse(c.Params("memoryId"))
	if err != nil {
		return badRequest(c, "invalid memory id")
	}
	var req struct {
		Content  string `json:"content"`
		Category string `json:"category"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	mem, err := h.memorySvc.Update(h.enrichContext(c), agentID, memoryID, req.Content, req.Category)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(mem)
}

func (h *Handler) DeleteAgentMemory(c *fiber.Ctx) error {
	agentID, err := agentParam(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	memoryID, err := uuid.Parse(c.Params("memoryId"))
	if err != nil {
		return badRequest(c, "invalid memory id")
	}
	if err := h.memorySvc.Delete(h.enrichContext(c), agentID, memoryID); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListSharedMemories(c *fiber.Ctx) error {
	repoID, repoScope, err := memoryRepoScope(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	memories, err := h.memorySvc.List(h.enrichContext(c), domain.MemoryQuery{
		Owner: domain.MemoryOwnerTeam, RepositoryID: repoID, Repo: repoScope,
		Limit: c.QueryInt("limit", 200),
	})
	if err != nil {
		return internalError(c, err)
	}
	if memories == nil {
		memories = []domain.AgentMemory{}
	}
	return c.JSON(fiber.Map{"memories": memories})
}

func (h *Handler) CreateSharedMemory(c *fiber.Ctx) error {
	var req struct {
		Content      string `json:"content"`
		Category     string `json:"category"`
		RepositoryID string `json:"repository_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	repoID, err := memoryRepositoryBody(req.RepositoryID)
	if err != nil {
		return badRequest(c, err.Error())
	}
	mem, err := h.memorySvc.SaveShared(h.enrichContext(c), repoID, req.Content, req.Category, domain.MemorySourceUser)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(mem)
}

func (h *Handler) UpdateSharedMemory(c *fiber.Ctx) error {
	memoryID, err := uuid.Parse(c.Params("memoryId"))
	if err != nil {
		return badRequest(c, "invalid memory id")
	}
	var req struct {
		Content  string `json:"content"`
		Category string `json:"category"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	mem, err := h.memorySvc.UpdateShared(h.enrichContext(c), memoryID, req.Content, req.Category)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(mem)
}

// PlanSharedMemoryPromotion answers what a promotion WOULD do: which team
// memories read as reusable know-how, the skill each would become, and which
// agents would receive it. Nothing is written — the operator approves the plan
// and sends it back to PromoteSharedMemories.
func (h *Handler) PlanSharedMemoryPromotion(c *fiber.Ctx) error {
	plan, err := h.evolutionSvc.PlanSharedMemoryPromotion(h.enrichContext(c))
	if err != nil {
		if errors.Is(err, evolution.ErrPromotionInFlight) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "conflict"}})
		}
		return internalError(c, err)
	}
	return c.JSON(plan)
}

// PromoteSharedMemories writes the promotion. With a body it applies exactly
// the candidates the operator approved; with no body it plans and applies
// everything, which is the old one-shot behaviour.
//
// Synchronous on purpose: the page that triggers it wants the result, and the
// inserts are well inside the request budget.
func (h *Handler) PromoteSharedMemories(c *fiber.Ctx) error {
	var req struct {
		Candidates []domain.MemoryPromotionCandidate `json:"candidates"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	ctx := h.enrichContext(c)
	var (
		result domain.MemoryPromotionResult
		err    error
	)
	if len(req.Candidates) > 0 {
		result, err = h.evolutionSvc.ApplySharedMemoryPromotion(ctx, req.Candidates)
	} else {
		result, err = h.evolutionSvc.PromoteSharedMemories(ctx)
	}
	if err != nil {
		if errors.Is(err, evolution.ErrPromotionInFlight) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": fiber.Map{"message": err.Error(), "type": "conflict"}})
		}
		return internalError(c, err)
	}
	return c.JSON(result)
}

func (h *Handler) DeleteSharedMemory(c *fiber.Ctx) error {
	memoryID, err := uuid.Parse(c.Params("memoryId"))
	if err != nil {
		return badRequest(c, "invalid memory id")
	}
	if err := h.memorySvc.Delete(h.enrichContext(c), uuid.Nil, memoryID); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListKPIMetrics(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"metrics": h.kpiSvc.ListMetrics()})
}

func (h *Handler) ListAgentKPIs(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	kpis, err := h.kpiSvc.ListKPIs(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	if kpis == nil {
		kpis = []domain.AgentKPI{}
	}
	return c.JSON(fiber.Map{"kpis": kpis})
}

func (h *Handler) CreateAgentKPI(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req domain.CreateKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	created, err := h.kpiSvc.CreateKPI(h.enrichContext(c), agentID, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(created)
}

func (h *Handler) UpdateAgentKPI(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	kpiID, err := uuid.Parse(c.Params("kpiId"))
	if err != nil {
		return badRequest(c, "invalid kpi id")
	}
	var req domain.UpdateKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	updated, err := h.kpiSvc.UpdateKPI(h.enrichContext(c), agentID, kpiID, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(updated)
}

func (h *Handler) DeleteAgentKPI(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	kpiID, err := uuid.Parse(c.Params("kpiId"))
	if err != nil {
		return badRequest(c, "invalid kpi id")
	}
	if err := h.kpiSvc.DeleteKPI(h.enrichContext(c), agentID, kpiID); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func orEmptyScoreEvents(events []domain.AgentScoreEvent) []domain.AgentScoreEvent {
	if events == nil {
		return []domain.AgentScoreEvent{}
	}
	return events
}
