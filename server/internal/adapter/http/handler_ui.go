package http

import (
	"context"
	"crypto/subtle"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerUIRoutes(app *fiber.App) {
	app.Get("/v1/sessions", h.ListSessions)
	app.Get("/v1/sessions/:id/activity", h.SessionActivity)
	app.Get("/v1/runs/:id/steps", h.RunSteps)
	app.Get("/v1/activity/active", h.ActiveRuns)
	app.Get("/v1/jobs", h.ListJobs)
	app.Get("/admin/api-keys", h.ListAPIKeys)
	app.Post("/admin/api-keys", h.CreateAPIKey)
	app.Delete("/admin/api-keys/:name", h.DeleteAPIKey)

	uiRoot := h.uiRoot
	if uiRoot == "" {
		uiRoot = "../web/dist"
	}

	if h.uiFS != nil {
		h.registerEmbeddedUI(app, h.uiFS)
		return
	}

	if _, err := os.Stat(uiRoot); err == nil {
		app.Static("/", uiRoot, fiber.Static{
			Index:  "index.html",
			Browse: false,
		})
		app.Get("/*", func(c *fiber.Ctx) error {
			if isServerPath(c.Path()) {
				return c.Next()
			}
			return c.SendFile(filepath.Join(uiRoot, "index.html"))
		})
	}
}

// isServerPath reports whether a request belongs to this server rather than to
// the single-page app, so the SPA's index.html fallback leaves it alone.
//
// One function rather than the three copies of the same condition it replaced:
// they had to agree, and a path added to two of them was a route that worked
// until the deployment switched between an embedded and an on-disk UI.
func isServerPath(path string) bool {
	return strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/admin") ||
		path == "/health" || path == "/metrics" || strings.HasPrefix(path, "/docs") ||
		// The Claude Code tool endpoint. Answering a JSON-RPC call with an HTML
		// page makes the CLI report a parse error rather than a missing route.
		path == mcpserver.Path
}

func (h *Handler) registerEmbeddedUI(app *fiber.App, assets fs.FS) {
	app.Use("/", filesystem.New(filesystem.Config{
		Root:       http.FS(assets),
		Browse:     false,
		Index:      "index.html",
		PathPrefix: "/",
		Next:       func(c *fiber.Ctx) bool { return isServerPath(c.Path()) },
	}))
	app.Get("/*", func(c *fiber.Ctx) error {
		if isServerPath(c.Path()) {
			return c.Next()
		}
		return filesystem.SendFile(c, http.FS(assets), "index.html")
	})
}

func (h *Handler) isPublicPath(path string) bool {
	if path == "/health" || path == "/metrics" || path == "/docs" || hasPrefix(path, "/docs/") {
		return true
	}
	// GitHub cannot send a bearer token; the webhook authenticates every
	// request itself via the per-repo HMAC signature.
	//
	// The route matches this same constant, so the two cannot disagree about
	// which path they are talking about.
	if path == githubWebhookPath {
		return true
	}
	// The Claude Code tool endpoint authenticates itself too, with the per-run
	// bearer token it was handed (adapter/mcpserver). Its caller is a CLI
	// session — a child process on this host, or one on a member's Mac reaching
	// in through the control plane — and it deliberately holds none of this
	// server's credentials: no API key, no gateway signature, no
	// Firebase token. Demanding one here would lock out the only client the
	// route exists for. Named explicitly rather than relying on the prefix rule
	// below, because it is a decision, not a side effect of the path we
	// happened to pick.
	if path == mcpserver.Path {
		return true
	}
	if !strings.HasPrefix(path, "/v1") && !strings.HasPrefix(path, "/admin") {
		return true
	}
	return false
}

// secretEqual compares a presented token against a configured key without
// leaking, through timing, how many leading bytes matched — the same rule the
// gateway signature (internalauth) and the GitHub webhook HMAC already follow.
func secretEqual(presented, configured string) bool {
	return subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) == 1
}

func (h *Handler) authenticateToken(c *fiber.Ctx, token string) bool {
	for _, entry := range h.apiKeys {
		if entry.Key != "" && secretEqual(token, entry.Key) {
			c.Locals("client_name", entry.Name)
			c.Locals("client_policy", entry.ToolPolicy)
			return true
		}
	}
	if h.legacyAPIKey != "" && secretEqual(token, h.legacyAPIKey) {
		return true
	}
	if h.apiKeySvc != nil {
		rec, err := h.apiKeySvc.Authenticate(c.Context(), token)
		if err == nil && rec != nil {
			c.Locals("client_name", rec.Name)
			c.Locals("client_policy", rec.ToolPolicy)
			return true
		}
	}
	return false
}

func (h *Handler) ListSessions(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	agentIDStr := c.Query("agent_id")
	if agentIDStr != "" {
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return badRequest(c, "invalid agent_id")
		}
		sessions, err := h.sessionSvc.ListByAgent(h.enrichContext(c), agentID, limit, offset)
		if err != nil {
			return internalError(c, err)
		}
		return c.JSON(fiber.Map{"sessions": sessions, "count": len(sessions)})
	}
	projectIDStr := c.Query("project_id")
	if projectIDStr != "" {
		projectID, err := uuid.Parse(projectIDStr)
		if err != nil {
			return badRequest(c, "invalid project_id")
		}
		sessions, err := h.sessionSvc.ListByProject(h.enrichContext(c), projectID, limit, offset)
		if err != nil {
			return internalError(c, err)
		}
		return c.JSON(fiber.Map{"sessions": sessions, "count": len(sessions)})
	}
	sessions, err := h.sessionSvc.List(h.enrichContext(c), limit, offset)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"sessions": sessions, "count": len(sessions)})
}

func (h *Handler) SessionActivity(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid session id")
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	runs, err := h.sessionSvc.ListRuns(h.enrichContext(c), id, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"runs": runs, "count": len(runs)})
}

// activityRunID maps whatever a client calls a "run id" onto the session_runs.id
// that session_steps and orchestration_plans are actually keyed by.
//
// Two tables answer to the word "run" and both are keyed by a UUID, so a
// wrong-but-parseable id cannot fail loudly — it selects nothing and the panel
// renders empty. The board's run list is built from task_agent_runs, so that is
// the id it holds and (before it learned to send session_run_id) the id it sent;
// chat and the session pages hold session_runs rows and send those.
//
// The task-agent-run lookup goes FIRST because it is the only one of the two
// that can be answered definitively: the row either exists or it does not, and
// when it does, its session_run_id is the authoritative link to the steps. Only
// an id that is not a board run falls through to the session-run reading, which
// costs nothing extra — that is the very query the caller was about to make.
//
// ok=false means the id names a board run that has no session run yet: a pending
// run that never started. That is an empty list, not an error.
func (h *Handler) activityRunID(ctx context.Context, id uuid.UUID) (runID uuid.UUID, taskRun bool, ok bool) {
	if h.taskRuns == nil {
		return id, false, true
	}
	run, err := h.taskRuns.GetByID(ctx, id)
	if err != nil {
		// Either the id is no board run (domain.ErrTaskAgentRunNotFound) or the
		// lookup itself failed. Both leave the id as good a session-run id as it
		// ever was, which is what this endpoint has always assumed it to be.
		return id, false, true
	}
	if run.SessionRunID == nil {
		return uuid.Nil, true, false
	}
	return *run.SessionRunID, true, true
}

func (h *Handler) RunSteps(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid run id")
	}
	ctx := h.enrichContext(c)
	runID, taskRun, ok := h.activityRunID(ctx, id)
	if !ok {
		// A queued board run that never started has nothing behind it yet.
		return c.JSON(fiber.Map{"steps": []domain.SessionStep{}, "count": 0})
	}
	steps, err := h.sessionSvc.ListRunSteps(ctx, runID)
	if err != nil {
		return internalError(c, err)
	}
	if len(steps) == 0 && !taskRun {
		// An id that is neither a board run nor a run with any steps is the
		// silent-empty case this endpoint spent its whole life in: both lookups
		// came up empty and the client still got a 200. Not a 404 — an empty
		// list is a legitimate answer for a run that has yet to record a step —
		// but it is worth naming the id and both lookups in the log.
		log.Warn().
			Str("run_id", id.String()).
			Msg("run steps: id matched no task_agent_runs row and no session_steps rows")
	}
	if steps == nil {
		steps = []domain.SessionStep{}
	}
	return c.JSON(fiber.Map{"steps": steps, "count": len(steps)})
}

func (h *Handler) ActiveRuns(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	runs, err := h.sessionSvc.ListActiveRuns(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if runs == nil {
		runs = []domain.SessionRun{}
	}
	return c.JSON(fiber.Map{"runs": runs, "count": len(runs)})
}

func (h *Handler) ListJobs(c *fiber.Ctx) error {
	if h.jobSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "jobs not enabled", Type: "service_unavailable"},
		})
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	jobs, err := h.jobSvc.List(h.enrichContext(c), c.Query("status"), limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"jobs": jobs, "count": len(jobs)})
}

func (h *Handler) ListAPIKeys(c *fiber.Ctx) error {
	if h.apiKeySvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "api key management not enabled", Type: "service_unavailable"},
		})
	}
	keys, err := h.apiKeySvc.List(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"api_keys": keys, "count": len(keys)})
}

func (h *Handler) CreateAPIKey(c *fiber.Ctx) error {
	if h.apiKeySvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "api key management not enabled", Type: "service_unavailable"},
		})
	}
	var req domain.CreateAPIKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	resp, err := h.apiKeySvc.Create(h.enrichContext(c), req)
	if err != nil {
		return internalError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

func (h *Handler) DeleteAPIKey(c *fiber.Ctx) error {
	if h.apiKeySvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "api key management not enabled", Type: "service_unavailable"},
		})
	}
	name := c.Params("name")
	if name == "" {
		return badRequest(c, "name is required")
	}
	if err := h.apiKeySvc.Delete(h.enrichContext(c), name); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}
