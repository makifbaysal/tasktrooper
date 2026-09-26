package http

import (
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const defaultErrorsWindow = 24 * time.Hour

// registerCloudRoutes mounts cloud provider accounts, the per-(component,
// environment) bindings, and the runtime picture (deployments, logs, errors)
// read through them. See .ai/api-spec.md for the contract this implements.
func (h *Handler) registerCloudRoutes(app fiber.Router) {
	if h.cloudSvc == nil {
		return
	}
	app.Get("/v1/cloud-accounts", h.ListCloudAccounts)
	app.Post("/v1/cloud-accounts", h.CreateCloudAccount)
	app.Patch("/v1/cloud-accounts/:id", h.UpdateCloudAccount)
	app.Post("/v1/cloud-accounts/:id/verify", h.VerifyCloudAccount)
	app.Delete("/v1/cloud-accounts/:id", h.DeleteCloudAccount)
	app.Get("/v1/cloud-accounts/:id/resources", h.ListCloudAccountResources)

	app.Put("/v1/components/:componentId/environments/:env", h.BindComponentEnvironment)
	app.Patch("/v1/environments/:envId", h.PatchEnvironment)
	app.Delete("/v1/environments/:envId", h.DeleteEnvironment)

	app.Get("/v1/environments/:envId/overview", h.EnvironmentOverview)
	app.Get("/v1/environments/:envId/logs", h.EnvironmentLogs)
	app.Get("/v1/environments/:envId/errors", h.EnvironmentErrors)
	app.Get("/v1/environments/:envId/deployments", h.EnvironmentDeployments)
	app.Post("/v1/environments/:envId/errors/task", h.CreateEnvironmentErrorTask)

	app.Get("/v1/repositories/:id/tasks/:taskId/previews", h.ListTaskPreviews)
}

// cErr writes the flat `{"error": "message"}` shape the cloud/runtime
// contract specifies (api-contract-phase2.md), the same as the project
// model's pmErr — this is a fresh endpoint family, not the nested
// errorResponse the rest of this package uses.
func cErr(c *fiber.Ctx, status int, err error) error {
	return c.Status(status).JSON(fiber.Map{"error": err.Error()})
}

func cErrMsg(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

func cErrCode(c *fiber.Ctx, status int, err error, code string) error {
	return c.Status(status).JSON(fiber.Map{"error": err.Error(), "code": code})
}

// cloudErr maps a cloud service/provider error to its contract status. Order
// matters: CreateAccount/UpdateAccount wrap a provider auth refusal as BOTH
// port.ErrCloudAuth and cloud.ErrInvalidInput, and the more specific
// "reconnect" signal must win so the UI can tell a bad credential apart from
// an ordinary validation mistake.
func cloudErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, port.ErrCloudAuth):
		return cErrCode(c, fiber.StatusBadRequest, err, "cloud_auth")
	case errors.Is(err, cloud.ErrInvalidInput):
		return cErr(c, fiber.StatusBadRequest, err)
	case errors.Is(err, cloud.ErrNotConnected):
		return cErrCode(c, fiber.StatusConflict, err, "not_connected")
	case errors.Is(err, port.ErrNotFound):
		return cErr(c, fiber.StatusNotFound, err)
	default:
		return cErr(c, fiber.StatusBadGateway, err)
	}
}

// ListCloudAccounts — GET /v1/cloud-accounts
func (h *Handler) ListCloudAccounts(c *fiber.Ctx) error {
	accounts, err := h.cloudSvc.ListAccounts(h.enrichContext(c))
	if err != nil {
		return cloudErr(c, err)
	}
	return c.JSON(fiber.Map{"accounts": accounts})
}

// CreateCloudAccount — POST /v1/cloud-accounts
// Body: SaveCloudAccountRequest{provider, label?, fields}. Verified against
// the live provider BEFORE anything is stored; a rejected credential is 400
// code cloud_auth. The response never carries the fields back.
func (h *Handler) CreateCloudAccount(c *fiber.Ctx) error {
	var req domain.SaveCloudAccountRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	acct, err := h.cloudSvc.CreateAccount(h.enrichContext(c), req)
	if err != nil {
		return cloudErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(acct)
}

type patchCloudAccountRequest struct {
	Label  *string           `json:"label"`
	Fields map[string]string `json:"fields"`
}

// UpdateCloudAccount — PATCH /v1/cloud-accounts/:id
// fields is re-verified against the provider only when the caller supplies
// it; a bare label rename never touches the stored credential.
func (h *Handler) UpdateCloudAccount(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid cloud account id")
	}
	var req patchCloudAccountRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	acct, err := h.cloudSvc.UpdateAccount(h.enrichContext(c), id, req.Label, req.Fields)
	if err != nil {
		return cloudErr(c, err)
	}
	return c.JSON(acct)
}

// VerifyCloudAccount — POST /v1/cloud-accounts/:id/verify
// Always 200: a refused credential comes back as status "error" with
// status_detail rather than an HTTP error, so the console can poll it like
// any other account read.
func (h *Handler) VerifyCloudAccount(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid cloud account id")
	}
	acct, err := h.cloudSvc.VerifyAccount(h.enrichContext(c), id)
	if err != nil {
		return cloudErr(c, err)
	}
	return c.JSON(acct)
}

// DeleteCloudAccount — DELETE /v1/cloud-accounts/:id
// Environments bound to this account keep their rows; they only lose the
// account reference (see api-contract-phase2.md).
func (h *Handler) DeleteCloudAccount(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid cloud account id")
	}
	if err := h.cloudSvc.DeleteAccount(h.enrichContext(c), id); err != nil {
		return cloudErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListCloudAccountResources — GET /v1/cloud-accounts/:id/resources?refresh=1
// Cached 60s; refresh forces a live re-fetch from the provider.
func (h *Handler) ListCloudAccountResources(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid cloud account id")
	}
	refresh := c.Query("refresh") != ""
	resources, err := h.cloudSvc.ListResources(h.enrichContext(c), id, refresh)
	if err != nil {
		return cloudErr(c, err)
	}
	if resources == nil {
		resources = []domain.CloudResource{}
	}
	return c.JSON(fiber.Map{"resources": resources})
}

// BindComponentEnvironment — PUT /v1/components/:componentId/environments/:env
// Body: SaveEnvironmentRequest{account_id?, resource?, url?, health_url?}.
// :env is production|staging|preview|development. With account_id, resource
// is required (picked from the account's resources); without, url is
// required for a custom environment.
func (h *Handler) BindComponentEnvironment(c *fiber.Ctx) error {
	componentID, err := parseUUIDParam(c, "componentId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid component id")
	}
	env := domain.DeployEnvironment(c.Params("env"))
	var req domain.SaveEnvironmentRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	saved, err := h.cloudSvc.BindEnvironment(h.enrichContext(c), componentID, env, req)
	if err != nil {
		return cloudErr(c, err)
	}
	return c.JSON(saved)
}

type patchEnvironmentRequest struct {
	Status    *string                  `json:"status"`
	AccountID *uuid.UUID               `json:"account_id"`
	Resource  *domain.CloudResourceRef `json:"resource"`
}

// PatchEnvironment — PATCH /v1/environments/:envId
// Body: {status?: "confirmed"|"dismissed"|"suggested", account_id?, resource?}.
// Choosing one of an environment's candidates is sending its account_id +
// resource.ref with status confirmed.
func (h *Handler) PatchEnvironment(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "envId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid environment id")
	}
	var req patchEnvironmentRequest
	if err := c.BodyParser(&req); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	patch := cloud.EnvironmentPatch{AccountID: req.AccountID, Resource: req.Resource}
	if req.Status != nil {
		status := domain.LinkStatus(*req.Status)
		patch.Status = &status
	}
	saved, err := h.cloudSvc.PatchEnvironment(h.enrichContext(c), id, patch)
	if err != nil {
		return cloudErr(c, err)
	}
	return c.JSON(saved)
}

// DeleteEnvironment — DELETE /v1/environments/:envId
// A user-made binding is deleted outright; a scan-sourced row is only
// dismissed, so a later scan never resurrects what was just rejected.
func (h *Handler) DeleteEnvironment(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "envId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid environment id")
	}
	if err := h.cloudSvc.DeleteEnvironment(h.enrichContext(c), id); err != nil {
		return cloudErr(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// EnvironmentOverview — GET /v1/environments/:envId/overview
// Never fails on "not connected" or a provider auth refusal — both are
// reported through Unavailable, same as any other unreachable-but-not-broken
// state.
func (h *Handler) EnvironmentOverview(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "envId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid environment id")
	}
	overview, err := h.cloudSvc.Overview(h.enrichContext(c), id)
	if err != nil {
		return cloudErr(c, err)
	}
	return c.JSON(overview)
}

func validLogSeverity(s domain.LogSeverity) bool {
	switch s {
	case domain.LogDebug, domain.LogInfo, domain.LogWarning, domain.LogError, domain.LogCritical:
		return true
	}
	return false
}

// parseRuntimeLogQuery reads since/until (RFC3339), min_severity, q, limit
// and cursor off the query string. Defaults (last hour, 200 entries) are
// applied by cloud.Service, not here, so a zero value always means "the
// caller did not ask".
func parseRuntimeLogQuery(c *fiber.Ctx) (domain.RuntimeLogQuery, error) {
	var q domain.RuntimeLogQuery
	if raw := c.Query("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return domain.RuntimeLogQuery{}, errors.New("invalid since: must be RFC3339")
		}
		q.Since = t
	}
	if raw := c.Query("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return domain.RuntimeLogQuery{}, errors.New("invalid until: must be RFC3339")
		}
		q.Until = t
	}
	if raw := c.Query("min_severity"); raw != "" {
		sev := domain.LogSeverity(raw)
		if !validLogSeverity(sev) {
			return domain.RuntimeLogQuery{}, errors.New("invalid min_severity")
		}
		q.MinSeverity = sev
	}
	q.Text = c.Query("q")
	q.Cursor = c.Query("cursor")
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return domain.RuntimeLogQuery{}, errors.New("invalid limit")
		}
		q.Limit = n
	}
	return q, nil
}

// EnvironmentLogs — GET /v1/environments/:envId/logs?since=&until=&min_severity=&q=&limit=&cursor=
func (h *Handler) EnvironmentLogs(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "envId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid environment id")
	}
	q, err := parseRuntimeLogQuery(c)
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, err.Error())
	}
	page, err := h.cloudSvc.Logs(h.enrichContext(c), id, q)
	if err != nil {
		return cloudErr(c, err)
	}
	if page.Entries == nil {
		page.Entries = []domain.RuntimeLogEntry{}
	}
	return c.JSON(page)
}

// EnvironmentErrors — GET /v1/environments/:envId/errors?since=
// Default window is the last 24 hours.
func (h *Handler) EnvironmentErrors(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "envId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid environment id")
	}
	since := time.Now().UTC().Add(-defaultErrorsWindow)
	if raw := c.Query("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return cErrMsg(c, fiber.StatusBadRequest, "invalid since: must be RFC3339")
		}
		since = t
	}
	groups, err := h.cloudSvc.Errors(h.enrichContext(c), id, since)
	if err != nil {
		return cloudErr(c, err)
	}
	if groups == nil {
		groups = []domain.RuntimeErrorGroup{}
	}
	return c.JSON(fiber.Map{"errors": groups})
}

// EnvironmentDeployments — GET /v1/environments/:envId/deployments?limit=
func (h *Handler) EnvironmentDeployments(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "envId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid environment id")
	}
	limit := c.QueryInt("limit", 0)
	deployments, err := h.cloudSvc.Deployments(h.enrichContext(c), id, limit)
	if err != nil {
		return cloudErr(c, err)
	}
	if deployments == nil {
		deployments = []domain.CloudDeployment{}
	}
	return c.JSON(fiber.Map{"deployments": deployments})
}

// ListTaskPreviews — GET /v1/repositories/:id/tasks/:taskId/previews
// One entry per active component whose preview environment is per-branch.
func (h *Handler) ListTaskPreviews(c *fiber.Ctx) error {
	repoID, err := parseUUIDParam(c, "id")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid repository id")
	}
	taskID, err := parseUUIDParam(c, "taskId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid task id")
	}
	previews, err := h.cloudSvc.TaskPreviews(h.enrichContext(c), repoID, taskID)
	if err != nil {
		if errors.Is(err, domain.ErrBoardTaskNotFound) {
			return cErr(c, fiber.StatusNotFound, err)
		}
		return cloudErr(c, err)
	}
	return c.JSON(fiber.Map{"previews": previews.Previews})
}

// CreateEnvironmentErrorTask — POST /v1/environments/:envId/errors/task
// Body: RuntimeErrorGroup, as returned by EnvironmentErrors. Opens a bug task
// in the environment's repository with the error, a sample and recent log
// lines; the component is set from the environment.
func (h *Handler) CreateEnvironmentErrorTask(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "envId")
	if err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid environment id")
	}
	var group domain.RuntimeErrorGroup
	if err := c.BodyParser(&group); err != nil {
		return cErrMsg(c, fiber.StatusBadRequest, "invalid request body")
	}
	task, err := h.cloudSvc.CreateFixTask(h.enrichContext(c), id, group)
	if err != nil {
		return cloudErr(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(task)
}
