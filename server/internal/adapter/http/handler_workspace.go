package http

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (h *Handler) registerWorkspaceRoutes(app fiber.Router) {
	if h.workspaceSvc == nil {
		return
	}
	app.Get("/v1/board/config", h.GetBoardConfig)
	app.Get("/v1/board/settings", h.GetBoardSettings)
	app.Put("/v1/board/settings", h.UpdateBoardSettings)
	app.Get("/v1/board/columns", h.ListBoardColumns)
	app.Put("/v1/board/columns", h.UpdateBoardColumns)
	app.Get("/v1/board/members", h.GetBoardMembers)
	app.Put("/v1/board/members", h.SetBoardMembers)
	app.Get("/v1/board/subscriptions", h.GetBoardSubscriptions)
	app.Put("/v1/board/subscriptions", h.SetBoardSubscriptions)
	app.Get("/v1/board/transitions", h.GetBoardTransitions)
	app.Put("/v1/board/transitions", h.SetBoardTransitions)
	app.Get("/v1/activity", h.ListActivity)
	app.Get("/v1/tasks", h.ListAllBoardTasks)
	app.Get("/v1/tasks/released", h.ListReleasedArchive)
	app.Get("/v1/agents/:agentId/subscriptions", h.GetAgentSubscriptions)
	app.Put("/v1/agents/:agentId/subscriptions", h.SetAgentSubscriptions)
	app.Get("/v1/agents/:agentId/column-instructions", h.GetAgentColumnInstructions)
	app.Put("/v1/agents/:agentId/column-instructions", h.SetAgentColumnInstructions)
}

// agentSubscriptionJSON is one column an agent subscribes to, with its
// optional per-column task-type filter (null/omitted = every type).
type agentSubscriptionJSON struct {
	ColumnSlug string   `json:"column_slug"`
	TaskTypes  []string `json:"task_types"`
}

// GetAgentSubscriptions serves both the legacy `column_slugs` shape and the
// new `subscriptions` shape (with each column's task-type filter) in one
// response, so a UI build that only reads the old field keeps working.
func (h *Handler) GetAgentSubscriptions(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	detailed, err := h.workspaceSvc.ListAgentSubscriptionsDetailed(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	slugs := make([]string, 0, len(detailed))
	subs := make([]agentSubscriptionJSON, 0, len(detailed))
	for _, d := range detailed {
		slugs = append(slugs, d.ColumnSlug)
		subs = append(subs, agentSubscriptionJSON{ColumnSlug: d.ColumnSlug, TaskTypes: d.TaskTypes})
	}
	return c.JSON(fiber.Map{"column_slugs": slugs, "subscriptions": subs})
}

// SetAgentSubscriptions accepts either shape: `subscriptions` (per-column
// task_types filter) when present, else the legacy `column_slugs` (each
// column saved with no filter — the same "every type" behaviour it always
// had).
func (h *Handler) SetAgentSubscriptions(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req struct {
		ColumnSlugs   []string                `json:"column_slugs"`
		Subscriptions []agentSubscriptionJSON `json:"subscriptions"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	var subs []domain.AgentColumnSubscription
	if len(req.Subscriptions) > 0 {
		for _, s := range req.Subscriptions {
			subs = append(subs, domain.AgentColumnSubscription{ColumnSlug: s.ColumnSlug, TaskTypes: s.TaskTypes})
		}
	} else {
		for _, slug := range req.ColumnSlugs {
			subs = append(subs, domain.AgentColumnSubscription{ColumnSlug: slug})
		}
	}
	if err := h.workspaceSvc.SetAgentSubscriptionsDetailed(h.enrichContext(c), agentID, subs); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GetAgentColumnInstructions serves an agent's per-column prompts ("what to do
// when a task arrives in this column").
func (h *Handler) GetAgentColumnInstructions(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	instructions, err := h.workspaceSvc.ListAgentColumnInstructions(h.enrichContext(c), agentID)
	if err != nil {
		return internalError(c, err)
	}
	out := make([]struct {
		ColumnSlug  string `json:"column_slug"`
		Instruction string `json:"instruction"`
	}, 0, len(instructions))
	for _, ins := range instructions {
		out = append(out, struct {
			ColumnSlug  string `json:"column_slug"`
			Instruction string `json:"instruction"`
		}{ColumnSlug: ins.ColumnSlug, Instruction: ins.Instruction})
	}
	if out == nil {
		out = []struct {
			ColumnSlug  string `json:"column_slug"`
			Instruction string `json:"instruction"`
		}{}
	}
	return c.JSON(fiber.Map{"instructions": out})
}

// SetAgentColumnInstructions writes an agent's per-column prompts from the
// body. Columns not present are left as they are; an empty instruction clears
// the row, so the UI's "send every column, empty means none" is the delete.
func (h *Handler) SetAgentColumnInstructions(c *fiber.Ctx) error {
	agentID, err := uuid.Parse(c.Params("agentId"))
	if err != nil {
		return badRequest(c, "invalid agent id")
	}
	var req struct {
		Instructions []struct {
			ColumnSlug  string `json:"column_slug"`
			Instruction string `json:"instruction"`
		} `json:"instructions"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	instructions := make([]domain.AgentColumnInstruction, 0, len(req.Instructions))
	for _, ins := range req.Instructions {
		if ins.ColumnSlug == "" {
			continue
		}
		instructions = append(instructions, domain.AgentColumnInstruction{ColumnSlug: ins.ColumnSlug, Instruction: ins.Instruction})
	}
	if err := h.workspaceSvc.SetAgentColumnInstructions(h.enrichContext(c), agentID, instructions); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListAllBoardTasks serves the board. Released tasks older than
// domain.ReleasedBoardWindow are left out — they are in the released archive
// below, which is where the board links to for anything older.
//
// The board is polled several times a second across open views, so it answers
// If-None-Match. The ETag is a hash of the body; a 304 is served without reading
// the board only while the store's board version (read before the list was
// built) and the list's released-window expiry both say the last body is still
// the current one, and for at most boardListMaxAge.
func (h *Handler) ListAllBoardTasks(c *fiber.Ctx) error {
	if h.repositorySvc == nil {
		return internalError(c, fmt.Errorf("repositories unavailable"))
	}
	ifNoneMatch := c.Get(fiber.HeaderIfNoneMatch)
	version, versioned := h.repositorySvc.BoardVersion()
	c.Set(fiber.HeaderCacheControl, "no-cache")
	if versioned {
		if etag, ok := h.boardList.current(version); ok && etagMatches(ifNoneMatch, etag) {
			return notModified(c, etag)
		}
	}
	tasks, err := h.repositorySvc.ListBoardTasks(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	body, err := c.App().Config().JSONEncoder(fiber.Map{"tasks": tasks, "count": len(tasks)})
	if err != nil {
		return internalError(c, err)
	}
	etag := bodyETag(body)
	if versioned {
		expires, _ := repository.BoardListExpiry(tasks)
		h.boardList.remember(version, etag, expires)
	}
	if etagMatches(ifNoneMatch, etag) {
		return notModified(c, etag)
	}
	c.Set(fiber.HeaderETag, etag)
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Send(body)
}

// ListReleasedArchive is every released task, newest first, with an optional
// `q` search over key, title and description.
func (h *Handler) ListReleasedArchive(c *fiber.Ctx) error {
	if h.repositorySvc == nil {
		return internalError(c, fmt.Errorf("repositories unavailable"))
	}
	tasks, err := h.repositorySvc.ListReleasedArchive(h.enrichContext(c), c.Query("q"), c.QueryInt("limit", 100))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"tasks": tasks, "count": len(tasks)})
}

func (h *Handler) GetBoardConfig(c *fiber.Ctx) error {
	config, err := h.workspaceSvc.GetConfig(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(config)
}

func (h *Handler) GetBoardSettings(c *fiber.Ctx) error {
	settings, err := h.workspaceSvc.GetSettings(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(settings)
}

func (h *Handler) UpdateBoardSettings(c *fiber.Ctx) error {
	var req domain.UpdateBoardSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	settings, err := h.workspaceSvc.UpdateSettings(h.enrichContext(c), req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(settings)
}

func (h *Handler) ListBoardColumns(c *fiber.Ctx) error {
	columns, err := h.workspaceSvc.ListColumns(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if columns == nil {
		columns = []domain.BoardColumn{}
	}
	return c.JSON(fiber.Map{"columns": columns, "count": len(columns)})
}

func (h *Handler) UpdateBoardColumns(c *fiber.Ctx) error {
	var req domain.UpdateBoardColumnsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.UpdateColumns(h.enrichContext(c), req); err != nil {
		if errors.Is(err, domain.ErrColumnHasWorkflowStages) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
		}
		return badRequest(c, err.Error())
	}
	columns, err := h.workspaceSvc.ListColumns(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"columns": columns, "count": len(columns)})
}

func (h *Handler) GetBoardMembers(c *fiber.Ctx) error {
	members, err := h.workspaceSvc.ListMembers(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if members == nil {
		members = []domain.BoardMember{}
	}
	return c.JSON(fiber.Map{"members": members, "count": len(members)})
}

func (h *Handler) SetBoardMembers(c *fiber.Ctx) error {
	var req domain.SetBoardMembersRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.SetMembers(h.enrichContext(c), req); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetBoardSubscriptions(c *fiber.Ctx) error {
	subs, err := h.workspaceSvc.ListSubscriptions(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if subs == nil {
		subs = []domain.BoardSubscription{}
	}
	return c.JSON(fiber.Map{"subscriptions": subs, "count": len(subs)})
}

func (h *Handler) SetBoardSubscriptions(c *fiber.Ctx) error {
	var req domain.SetBoardSubscriptionsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.SetSubscriptions(h.enrichContext(c), req); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) GetBoardTransitions(c *fiber.Ctx) error {
	transitions, err := h.workspaceSvc.ListTransitions(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	if transitions == nil {
		transitions = []domain.BoardTransition{}
	}
	return c.JSON(fiber.Map{"transitions": transitions})
}

func (h *Handler) SetBoardTransitions(c *fiber.Ctx) error {
	var req domain.SetBoardTransitionsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.workspaceSvc.SetTransitions(h.enrichContext(c), req); err != nil {
		return badRequest(c, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) ListActivity(c *fiber.Ctx) error {
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if n, parseErr := strconv.Atoi(raw); parseErr == nil && n > 0 {
			limit = n
		}
	}
	items, err := h.workspaceSvc.ListActivity(h.enrichContext(c), h.boardEvents, h.taskRuns, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"items": items, "count": len(items)})
}

func (h *Handler) ListTaskComments(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	comments, err := h.repositorySvc.ListComments(h.enrichContext(c), repositoryID, taskID)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"comments": comments, "count": len(comments)})
}

func (h *Handler) CreateTaskComment(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req domain.CreateTaskCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Content == "" {
		return badRequest(c, "content is required")
	}
	comment, err := h.repositorySvc.AddComment(h.enrichContext(c), repositoryID, taskID, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(comment)
}

// ListTaskEvents serves a task's history: which column it moved between, who
// moved it, when it was assigned and commented on.
func (h *Handler) ListTaskEvents(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		if n, parseErr := strconv.Atoi(raw); parseErr == nil && n > 0 {
			limit = n
		}
	}
	events, err := h.repositorySvc.ListTaskEvents(h.enrichContext(c), repositoryID, taskID, h.boardEvents, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"events": events, "count": len(events)})
}

func (h *Handler) ListTaskAgentRuns(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if n, parseErr := strconv.Atoi(raw); parseErr == nil && n > 0 {
			limit = n
		}
	}
	var store port.TaskAgentRunStore
	if h.taskRuns != nil {
		store = h.taskRuns
	}
	runs, err := h.repositorySvc.ListTaskRuns(h.enrichContext(c), repositoryID, taskID, store, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"runs": runs, "count": len(runs)})
}
