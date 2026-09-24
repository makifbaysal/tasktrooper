package http

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func notFound(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusNotFound).JSON(errorResponse{
		Error: errorDetail{Message: msg, Type: "not_found"},
	})
}

// registerDeployRoutes exposes the deploy catalog and each repository's
// per-environment deploy definition.
func (h *Handler) registerDeployRoutes(app fiber.Router) {
	app.Get("/v1/deploy/templates", h.ListDeployTemplates)
	app.Get("/v1/deploy/templates/:templateId", h.GetDeployTemplate)
	if h.deploySvc == nil {
		return
	}
	app.Get("/v1/repositories/:id/deploy/config", h.GetDeployConfig)
	app.Put("/v1/repositories/:id/deploy/targets", h.SaveDeployTarget)
	app.Delete("/v1/repositories/:id/deploy/targets/:env", h.DeleteDeployTarget)
	app.Get("/v1/repositories/:id/deploy/targets/:env/instructions", h.GetDeployInstructions)
	app.Post("/v1/repositories/:id/deploy/targets/:env/setup-task", h.CreateDeploySetupTask)
	app.Post("/v1/repositories/:id/deploy/local-setup-task", h.CreateLocalSetupTask)
}

// registerProdOpsRoutes exposes incident ingest (the webhook every alerting
// stack posts to) and the incident lifecycle.
func (h *Handler) registerProdOpsRoutes(app fiber.Router) {
	if h.prodOpsSvc == nil {
		return
	}
	app.Post("/v1/repositories/:id/incidents", h.IngestIncident)
	app.Get("/v1/incidents", h.ListIncidents)
	app.Get("/v1/incidents/:incidentId", h.GetIncident)
	app.Post("/v1/incidents/:incidentId/triage", h.TriageIncident)
	app.Post("/v1/incidents/:incidentId/remedy", h.ProposeIncidentRemedy)
	app.Post("/v1/incidents/:incidentId/resolve", h.ResolveIncident)
	app.Post("/v1/incidents/:incidentId/ignore", h.IgnoreIncident)
	app.Put("/v1/repositories/:id/incident-policy", h.SetIncidentPolicy)
}

// registerRepositoryOpsRoutes exposes the per-repository verification settings
// that do not need the prodops service.
func (h *Handler) registerRepositoryOpsRoutes(app fiber.Router) {
	if h.repositorySvc == nil {
		return
	}
	app.Put("/v1/repositories/:id/test-strategy", h.SetTestStrategy)
	app.Get("/v1/repositories/:id/env-inventory", h.GetEnvInventory)
}

// SetTestStrategy — PUT /v1/repositories/:id/test-strategy
// Chooses how the repo's tasks are verified: workspace tests only, a staging
// deploy before QA, or a deploy at every reviewed step.
func (h *Handler) SetTestStrategy(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		TestStrategy string `json:"test_strategy"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	repo, err := h.repositorySvc.SetTestStrategy(h.enrichContext(c), id, req.TestStrategy)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(repo)
}

// GetEnvInventory — GET /v1/repositories/:id/env-inventory
// What the project needs to run (variable names declared by its .env.example
// style files) and where each environment answers.
func (h *Handler) GetEnvInventory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	inv, err := h.repositorySvc.EnvInventory(h.enrichContext(c), id)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(inv)
}

// ListDeployTemplates — GET /v1/deploy/templates?kind=backend
func (h *Handler) ListDeployTemplates(c *fiber.Ctx) error {
	kind := strings.TrimSpace(c.Query("kind"))
	templates := deploy.Templates()
	if kind != "" {
		templates = deploy.TemplatesForKind(kind)
	}
	// The list is a picker: bodies are fetched one at a time when applied.
	for i := range templates {
		templates[i].Body = ""
	}
	return c.JSON(fiber.Map{"templates": templates, "count": len(templates)})
}

// GetDeployTemplate — GET /v1/deploy/templates/:templateId
func (h *Handler) GetDeployTemplate(c *fiber.Ctx) error {
	tpl, ok := deploy.Template(c.Params("templateId"))
	if !ok {
		return notFound(c, "deploy template not found")
	}
	return c.JSON(tpl)
}

// GetDeployConfig — GET /v1/repositories/:id/deploy/config?sub_project_path=
func (h *Handler) GetDeployConfig(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	view, err := h.deploySvc.Config(h.enrichContext(c), id, c.Query("sub_project_path"))
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(view)
}

// SaveDeployTarget — PUT /v1/repositories/:id/deploy/targets
func (h *Handler) SaveDeployTarget(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req domain.SaveDeployTargetRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	target, err := h.deploySvc.SaveTarget(h.enrichContext(c), id, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(target)
}

// DeleteDeployTarget — DELETE /v1/repositories/:id/deploy/targets/:env?sub_project_path=
func (h *Handler) DeleteDeployTarget(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	if err := h.deploySvc.DeleteTarget(h.enrichContext(c), id, c.Query("sub_project_path"), c.Params("env")); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GetDeployInstructions — GET /v1/repositories/:id/deploy/targets/:env/instructions
// Returns the target's template rendered with its variables: the exact recipe
// for this repo's environment.
func (h *Handler) GetDeployInstructions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	body, err := h.deploySvc.Instructions(h.enrichContext(c), id, c.Params("env"))
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(fiber.Map{"env": c.Params("env"), "instructions": body})
}

// CreateDeploySetupTask — POST /v1/repositories/:id/deploy/targets/:env/setup-task
func (h *Handler) CreateDeploySetupTask(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	task, err := h.deploySvc.CreateSetupTask(h.enrichContext(c), id, c.Params("env"))
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(task)
}

// CreateLocalSetupTask — POST /v1/repositories/:id/deploy/local-setup-task?sub_project_path=
func (h *Handler) CreateLocalSetupTask(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	task, err := h.deploySvc.CreateLocalSetupTask(h.enrichContext(c), id, c.Query("sub_project_path"))
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(task)
}

// IngestIncident — POST /v1/repositories/:id/incidents
// The alert webhook. It accepts Alertmanager, Sentry, GCP Cloud Monitoring and
// a plain {title, severity, env, detail} payload; ?env= and ?source= override
// what the payload does not say.
func (h *Handler) IngestIncident(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	in, err := prodops.Normalize(c.Body(), prodops.Defaults{
		Env:    strings.TrimSpace(c.Query("env")),
		Source: strings.TrimSpace(c.Query("source")),
	})
	if err != nil {
		return badRequest(c, err.Error())
	}
	in.RepositoryID = id
	incident, err := h.prodOpsSvc.Ingest(h.enrichContext(c), in)
	if err != nil {
		return internalError(c, err)
	}
	// A resolve for an unknown fingerprint is a no-op, not a failure.
	if incident.ID == uuid.Nil {
		return c.SendStatus(fiber.StatusAccepted)
	}
	return c.Status(fiber.StatusCreated).JSON(incident)
}

// ListIncidents — GET /v1/incidents?repository_id=&env=&status=open,triaging
func (h *Handler) ListIncidents(c *fiber.Ctx) error {
	filter := domain.IncidentFilter{
		Env:   strings.TrimSpace(c.Query("env")),
		Limit: c.QueryInt("limit", 100),
	}
	if raw := strings.TrimSpace(c.Query("repository_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid repository id")
		}
		filter.RepositoryID = &id
	}
	for _, status := range strings.Split(c.Query("status"), ",") {
		status = strings.TrimSpace(status)
		if status == "" {
			continue
		}
		if !domain.ValidIncidentStatus(domain.IncidentStatus(status)) {
			return badRequest(c, "invalid status: "+status)
		}
		filter.Statuses = append(filter.Statuses, domain.IncidentStatus(status))
	}
	incidents, err := h.prodOpsSvc.List(h.enrichContext(c), filter)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"incidents": incidents, "count": len(incidents)})
}

// GetIncident — GET /v1/incidents/:incidentId
func (h *Handler) GetIncident(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return badRequest(c, "invalid incident id")
	}
	incident, err := h.prodOpsSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return notFound(c, err.Error())
	}
	return c.JSON(incident)
}

// TriageIncident — POST /v1/incidents/:incidentId/triage
func (h *Handler) TriageIncident(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return badRequest(c, "invalid incident id")
	}
	incident, err := h.prodOpsSvc.Triage(h.enrichContext(c), id)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(incident)
}

// ProposeIncidentRemedy — POST /v1/incidents/:incidentId/remedy
func (h *Handler) ProposeIncidentRemedy(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return badRequest(c, "invalid incident id")
	}
	var req struct {
		domain.Remedy
		Author string `json:"author,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	// The author is recorded on the incident (remedy_author) and is what stops
	// the next recurrence's machine triage from overwriting this proposal.
	author := strings.TrimSpace(req.Author)
	if author == "" {
		author = domain.RemedyAuthorHuman
	}
	incident, err := h.prodOpsSvc.ProposeRemedy(h.enrichContext(c), id, req.Remedy, author)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(incident)
}

// ResolveIncident — POST /v1/incidents/:incidentId/resolve
func (h *Handler) ResolveIncident(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return badRequest(c, "invalid incident id")
	}
	var req struct {
		Note string `json:"note,omitempty"`
	}
	_ = c.BodyParser(&req)
	incident, err := h.prodOpsSvc.Resolve(h.enrichContext(c), id, req.Note)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(incident)
}

// IgnoreIncident — POST /v1/incidents/:incidentId/ignore
func (h *Handler) IgnoreIncident(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return badRequest(c, "invalid incident id")
	}
	var req struct {
		Note string `json:"note,omitempty"`
	}
	_ = c.BodyParser(&req)
	incident, err := h.prodOpsSvc.Ignore(h.enrichContext(c), id, req.Note)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(incident)
}

// SetIncidentPolicy — PUT /v1/repositories/:id/incident-policy
func (h *Handler) SetIncidentPolicy(c *fiber.Ctx) error {
	if h.repositorySvc == nil {
		return notFound(c, "repositories are not available")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		Policy domain.IncidentPolicy `json:"incident_policy"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	repo, err := h.repositorySvc.SetIncidentPolicy(h.enrichContext(c), id, req.Policy)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(repo)
}
