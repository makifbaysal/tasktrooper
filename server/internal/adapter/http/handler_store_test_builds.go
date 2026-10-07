package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// registerStoreTestBuildRoutes exposes per-task test builds (TestFlight /
// Play internal app sharing) and the groups and tracks they are opened to.
// Called from registerStoreOpsRoutes so the same storeOpsSvc gate applies.
func (h *Handler) registerStoreTestBuildRoutes(app fiber.Router) {
	app.Get("/v1/repositories/:id/store/test-builds", h.ListStoreTestBuilds)
	app.Post("/v1/repositories/:id/store/test-builds", h.StartStoreTestBuilds)
	app.Get("/v1/repositories/:id/store/test-builds/:buildId", h.GetStoreTestBuild)
	app.Post("/v1/repositories/:id/store/test-builds/:buildId/open", h.OpenStoreTestBuild)
	app.Post("/v1/repositories/:id/store/test-builds/:buildId/close", h.CloseStoreTestBuild)
	app.Post("/v1/repositories/:id/store/test-builds/:buildId/export-compliance", h.AnswerStoreTestBuildCompliance)
	app.Get("/v1/repositories/:id/store/apps/:platform/test-groups", h.ListStoreTestGroups)
	app.Post("/v1/repositories/:id/store/apps/:platform/test-groups", h.CreateStoreTestGroup)
	app.Put("/v1/repositories/:id/store/apps/:platform/test-groups/auto", h.SetStoreTestAutoGroups)
	app.Get("/v1/repositories/:id/store/test-groups/:groupId/testers", h.ListStoreTestGroupTesters)
	app.Post("/v1/repositories/:id/store/test-groups/:groupId/testers", h.AddStoreTestGroupTester)
	app.Delete("/v1/repositories/:id/store/test-groups/:groupId/testers/:testerId", h.RemoveStoreTestGroupTester)
}

// storeTestBuildError maps the test-build verdicts onto the store actions'
// split: a build or app in the wrong state for the action is 409, an action
// one platform does not have is 400, an unknown build 404.
func storeTestBuildError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrTestBuildNotFound):
		return notFound(c, err.Error())
	case errors.Is(err, domain.ErrTestBuildUnsupported):
		return badRequest(c, err.Error())
	case errors.Is(err, domain.ErrTestBuildNotReady), errors.Is(err, domain.ErrTestBuildNoArtifact),
		errors.Is(err, domain.ErrTestBuildNoStoreApp), errors.Is(err, domain.ErrTestBuildAppNotTestable):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, storeops.ErrTestBuildsNotConfigured):
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": err.Error()})
	default:
		return storeOpsBuildError(c, err)
	}
}

// ListStoreTestBuilds — GET /v1/repositories/:id/store/test-builds?platform=&task_id=&limit=
func (h *Handler) ListStoreTestBuilds(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var taskID *uuid.UUID
	if raw := c.Query("task_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid task id")
		}
		taskID = &parsed
	}
	builds, err := h.storeOpsSvc.TestBuilds(h.enrichContext(c), id, c.Query("platform"), taskID, c.QueryInt("limit", 50))
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.JSON(builds)
}

type startStoreTestBuildsRequest struct {
	TaskID    *uuid.UUID `json:"task_id"`
	Platforms []string   `json:"platforms"`
}

// StartStoreTestBuilds — POST /v1/repositories/:id/store/test-builds
// Body: {"task_id": "...", "platforms": ["ios","android"]}. No task_id builds
// the default branch; no platforms means every linked app. Returns 202 with
// the queued builds; each runs on its own and is read back by polling.
func (h *Handler) StartStoreTestBuilds(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req startStoreTestBuildsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	builds, err := h.storeOpsSvc.StartTestBuilds(h.enrichContext(c), id, req.TaskID, req.Platforms, domain.TestBuildTriggerManual, consoleActor)
	if err != nil && len(builds) == 0 {
		return storeTestBuildError(c, err)
	}
	resp := fiber.Map{"builds": builds}
	if err != nil {
		resp["error"] = err.Error()
	}
	return c.Status(fiber.StatusAccepted).JSON(resp)
}

// testBuildParams parses both path ids. ok is false once the 400 has been
// written; badRequest itself returns nil, so its result cannot carry that.
func (h *Handler) testBuildParams(c *fiber.Ctx) (id, buildID uuid.UUID, ok bool) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		_ = badRequest(c, "invalid repository id")
		return uuid.Nil, uuid.Nil, false
	}
	buildID, err = uuid.Parse(c.Params("buildId"))
	if err != nil {
		_ = badRequest(c, "invalid build id")
		return uuid.Nil, uuid.Nil, false
	}
	return id, buildID, true
}

// GetStoreTestBuild — GET /v1/repositories/:id/store/test-builds/:buildId
func (h *Handler) GetStoreTestBuild(c *fiber.Ctx) error {
	id, buildID, ok := h.testBuildParams(c)
	if !ok {
		return nil
	}
	build, err := h.storeOpsSvc.TestBuild(h.enrichContext(c), id, buildID)
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.JSON(build)
}

type storeTestGroupsRequest struct {
	Groups []string `json:"groups"`
}

// OpenStoreTestBuild — POST .../test-builds/:buildId/open, Body: {"groups": [...]}
// Groups are TestFlight group ids or Play track names. An external TestFlight
// group submits the build to Beta App Review.
func (h *Handler) OpenStoreTestBuild(c *fiber.Ctx) error {
	id, buildID, ok := h.testBuildParams(c)
	if !ok {
		return nil
	}
	var req storeTestGroupsRequest
	if err := c.BodyParser(&req); err != nil || len(req.Groups) == 0 {
		return badRequest(c, "groups is required")
	}
	build, err := h.storeOpsSvc.OpenTestBuild(h.enrichContext(c), id, buildID, req.Groups, consoleActor)
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.JSON(build)
}

// CloseStoreTestBuild — POST .../test-builds/:buildId/close, Body: {"groups": [...]} (iOS only)
func (h *Handler) CloseStoreTestBuild(c *fiber.Ctx) error {
	id, buildID, ok := h.testBuildParams(c)
	if !ok {
		return nil
	}
	var req storeTestGroupsRequest
	if err := c.BodyParser(&req); err != nil || len(req.Groups) == 0 {
		return badRequest(c, "groups is required")
	}
	build, err := h.storeOpsSvc.CloseTestBuild(h.enrichContext(c), id, buildID, req.Groups, consoleActor)
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.JSON(build)
}

// AnswerStoreTestBuildCompliance — POST .../test-builds/:buildId/export-compliance
// Body: {"uses_non_exempt_encryption": false}. The answer is the developer's
// legal statement, so the body must state it; there is no default.
func (h *Handler) AnswerStoreTestBuildCompliance(c *fiber.Ctx) error {
	id, buildID, ok := h.testBuildParams(c)
	if !ok {
		return nil
	}
	var req struct {
		UsesNonExemptEncryption *bool `json:"uses_non_exempt_encryption"`
	}
	if err := c.BodyParser(&req); err != nil || req.UsesNonExemptEncryption == nil {
		return badRequest(c, "uses_non_exempt_encryption is required")
	}
	build, err := h.storeOpsSvc.AnswerExportCompliance(h.enrichContext(c), id, buildID, *req.UsesNonExemptEncryption, consoleActor)
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.JSON(build)
}

// ListStoreTestGroups — GET /v1/repositories/:id/store/apps/:platform/test-groups
func (h *Handler) ListStoreTestGroups(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	groups, err := h.storeOpsSvc.TestGroups(h.enrichContext(c), id, c.Params("platform"))
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.JSON(groups)
}

// CreateStoreTestGroup — POST .../test-groups, Body: {"name": "...", "internal": false} (iOS only)
func (h *Handler) CreateStoreTestGroup(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		Name     string `json:"name"`
		Internal bool   `json:"internal"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	group, err := h.storeOpsSvc.CreateTestGroup(h.enrichContext(c), id, c.Params("platform"), req.Name, req.Internal)
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(group)
}

// SetStoreTestAutoGroups — PUT .../test-groups/auto, Body: {"groups": [...]}
// The groups a new task build is opened to once ready; [] opens it to none.
func (h *Handler) SetStoreTestAutoGroups(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req storeTestGroupsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Groups == nil {
		req.Groups = []string{}
	}
	if err := h.storeOpsSvc.SetTestAutoGroups(h.enrichContext(c), id, c.Params("platform"), req.Groups); err != nil {
		return storeTestBuildError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListStoreTestGroupTesters — GET /v1/repositories/:id/store/test-groups/:groupId/testers
func (h *Handler) ListStoreTestGroupTesters(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	testers, err := h.storeOpsSvc.TestGroupTesters(h.enrichContext(c), id, c.Params("groupId"))
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.JSON(testers)
}

// AddStoreTestGroupTester — POST .../testers, Body: {"email","first_name","last_name"}
func (h *Handler) AddStoreTestGroupTester(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	tester, err := h.storeOpsSvc.AddTestGroupTester(h.enrichContext(c), id, c.Params("groupId"), req.Email, req.FirstName, req.LastName)
	if err != nil {
		return storeTestBuildError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(tester)
}

// RemoveStoreTestGroupTester — DELETE .../testers/:testerId
func (h *Handler) RemoveStoreTestGroupTester(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	if err := h.storeOpsSvc.RemoveTestGroupTester(h.enrichContext(c), id, c.Params("groupId"), c.Params("testerId")); err != nil {
		return storeTestBuildError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
