package http

import (
	"context"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ReleaseService is application/release.Service narrowed to what the HTTP
// layer calls. Declared here rather than imported: this package must not
// depend on application/release (a package that in turn depends on the board
// package this handler layer stays clear of), so the two sides are wired
// together in platform/runtime by a concrete *release.Service that happens to
// satisfy this interface.
type ReleaseService interface {
	List(ctx context.Context, f domain.ReleaseListFilter) ([]domain.Release, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Release, error)
	Deploy(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor) (domain.Release, error)
	Finish(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, note string) (domain.Release, error)
	Rollback(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, reason domain.RollbackReason, note string) (domain.Release, error)
	CutPreview(ctx context.Context, releaseID uuid.UUID) (domain.ReleaseCutPreview, error)
	Cut(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, req domain.ReleaseCutRequest) (domain.Release, error)
}

// errReleaseConfirmMismatch matches the wording of deployops.ErrConfirmMismatch
// (handler_deployops.go's RollbackDeploy) rather than importing it: this
// package already depends on deployops for an unrelated handler family, but
// the two confirm checks are independent business rules that only happen to
// read alike, and this one is enforced here (not in application/release,
// which takes no confirm parameter at all).
var errReleaseConfirmMismatch = errors.New("confirmation phrase does not match the repository name")

// registerReleaseRoutes mounts the release API: what a merge opened, what the
// release engineer or a human watches it through, and the two actions
// (finish, rollback) that record a verdict on production. Same auth chain as
// every other route; every write additionally requires the caller to type
// the repository's name, the same guardrail handler_deployops.go's dispatch
// and rollback routes use.
func (h *Handler) registerReleaseRoutes(app fiber.Router) {
	if h.releaseSvc == nil {
		return
	}
	app.Get("/v1/repositories/:id/releases", h.ListReleases)
	app.Get("/v1/releases/:releaseId", h.GetRelease)
	app.Get("/v1/releases/:releaseId/cut-preview", h.GetReleaseCutPreview)
	app.Post("/v1/releases/:releaseId/cut", h.CutRelease)
	app.Post("/v1/releases/:releaseId/deploy", h.DeployRelease)
	app.Post("/v1/releases/:releaseId/finish", h.FinishRelease)
	app.Post("/v1/releases/:releaseId/rollback", h.RollbackRelease)
}

// releaseErr maps a release action's error onto the status the UI branches
// on: a release that does not exist is 404, a release the action does not
// apply to right now (wrong status, no deploy step, delivery unconfirmed) is
// 409 — the caller can retry once the state changes — a malformed confirm or
// delivery profile is the caller's fault (400), an unknown repository is 404,
// and anything else is ours.
func releaseErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errReleaseConfirmMismatch):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, domain.ErrReleaseNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, domain.ErrReleaseWrongStatus), errors.Is(err, domain.ErrReleaseNoDeploy), errors.Is(err, domain.ErrDeliveryUnconfirmed),
		errors.Is(err, domain.ErrReleaseEmpty), errors.Is(err, domain.ErrReleaseTagExists):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, domain.ErrInvalidDelivery), errors.Is(err, domain.ErrInvalidVersion):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, port.ErrNotFound):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "repository not found"})
	default:
		return internalError(c, err)
	}
}

// checkReleaseConfirm loads the release's repository and requires the caller
// to have typed its name — the human-in-the-loop guardrail every production
// write in this codebase asks for (see handler_deployops.go, prodops).
// application/release.Deploy/Finish/Rollback take no confirm parameter at
// all: the check belongs to this layer, the only one that knows what a human
// typed into a dialog.
func (h *Handler) checkReleaseConfirm(ctx context.Context, repositoryID uuid.UUID, confirm string) error {
	repo, err := h.repositorySvc.Get(ctx, repositoryID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirm) != repo.Name {
		return errReleaseConfirmMismatch
	}
	return nil
}

// ListReleases — GET /v1/repositories/:id/releases?component_id=&task_id=&limit=
func (h *Handler) ListReleases(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "id")
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	filter := domain.ReleaseListFilter{RepositoryID: &id, Limit: c.QueryInt("limit", 0)}
	if raw := c.Query("component_id"); raw != "" {
		componentID, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid component id")
		}
		filter.ComponentID = &componentID
	}
	if raw := c.Query("task_id"); raw != "" {
		taskID, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid task id")
		}
		filter.TaskID = &taskID
	}

	releases, err := h.releaseSvc.List(h.enrichContext(c), filter)
	if err != nil {
		return releaseErr(c, err)
	}
	if releases == nil {
		releases = []domain.Release{}
	}
	return c.JSON(fiber.Map{"releases": releases})
}

// GetRelease — GET /v1/releases/:releaseId
func (h *Handler) GetRelease(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "releaseId")
	if err != nil {
		return badRequest(c, "invalid release id")
	}
	release, err := h.releaseSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return releaseErr(c, err)
	}
	return c.JSON(release)
}

// GetReleaseCutPreview — GET /v1/releases/:releaseId/cut-preview
func (h *Handler) GetReleaseCutPreview(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "releaseId")
	if err != nil {
		return badRequest(c, "invalid release id")
	}
	preview, err := h.releaseSvc.CutPreview(h.enrichContext(c), id)
	if err != nil {
		return releaseErr(c, err)
	}
	return c.JSON(preview)
}

// cutReleaseRequest is the cut endpoint's body: the confirm phrase plus the
// human-chosen version and notes (notes empty regenerates them).
type cutReleaseRequest struct {
	Confirm string `json:"confirm"`
	Version string `json:"version"`
	Notes   string `json:"notes"`
}

// CutRelease — POST /v1/releases/:releaseId/cut
func (h *Handler) CutRelease(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "releaseId")
	if err != nil {
		return badRequest(c, "invalid release id")
	}
	var req cutReleaseRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ctx := h.enrichContext(c)

	release, err := h.releaseSvc.Get(ctx, id)
	if err != nil {
		return releaseErr(c, err)
	}
	if err := h.checkReleaseConfirm(ctx, release.RepositoryID, req.Confirm); err != nil {
		return releaseErr(c, err)
	}

	updated, err := h.releaseSvc.Cut(ctx, id, domain.ReleaseActorHuman, domain.ReleaseCutRequest{
		Version: req.Version,
		Notes:   req.Notes,
	})
	if err != nil {
		return releaseErr(c, err)
	}
	return c.JSON(updated)
}

// releaseActionRequest is the body every release write endpoint shares: the
// confirm phrase (always required), and a note (finish/rollback only, left
// empty and ignored by deploy).
type releaseActionRequest struct {
	Confirm string `json:"confirm"`
	Note    string `json:"note"`
}

// DeployRelease — POST /v1/releases/:releaseId/deploy
func (h *Handler) DeployRelease(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "releaseId")
	if err != nil {
		return badRequest(c, "invalid release id")
	}
	var req releaseActionRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ctx := h.enrichContext(c)

	release, err := h.releaseSvc.Get(ctx, id)
	if err != nil {
		return releaseErr(c, err)
	}
	if err := h.checkReleaseConfirm(ctx, release.RepositoryID, req.Confirm); err != nil {
		return releaseErr(c, err)
	}

	updated, err := h.releaseSvc.Deploy(ctx, id, domain.ReleaseActorHuman)
	if err != nil {
		return releaseErr(c, err)
	}
	return c.JSON(updated)
}

// FinishRelease — POST /v1/releases/:releaseId/finish
func (h *Handler) FinishRelease(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "releaseId")
	if err != nil {
		return badRequest(c, "invalid release id")
	}
	var req releaseActionRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ctx := h.enrichContext(c)

	release, err := h.releaseSvc.Get(ctx, id)
	if err != nil {
		return releaseErr(c, err)
	}
	if err := h.checkReleaseConfirm(ctx, release.RepositoryID, req.Confirm); err != nil {
		return releaseErr(c, err)
	}

	updated, err := h.releaseSvc.Finish(ctx, id, domain.ReleaseActorHuman, req.Note)
	if err != nil {
		return releaseErr(c, err)
	}
	return c.JSON(updated)
}

// RollbackRelease — POST /v1/releases/:releaseId/rollback
// Always domain.RollbackManual: a human's own decision, distinct from the
// deploy_failed/verify_failed/health_incident reasons the release engineer
// cites from its own evidence via the rollback_release tool.
func (h *Handler) RollbackRelease(c *fiber.Ctx) error {
	id, err := parseUUIDParam(c, "releaseId")
	if err != nil {
		return badRequest(c, "invalid release id")
	}
	var req releaseActionRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	ctx := h.enrichContext(c)

	release, err := h.releaseSvc.Get(ctx, id)
	if err != nil {
		return releaseErr(c, err)
	}
	if err := h.checkReleaseConfirm(ctx, release.RepositoryID, req.Confirm); err != nil {
		return releaseErr(c, err)
	}

	updated, err := h.releaseSvc.Rollback(ctx, id, domain.ReleaseActorHuman, domain.RollbackManual, req.Note)
	if err != nil {
		return releaseErr(c, err)
	}
	return c.JSON(updated)
}
