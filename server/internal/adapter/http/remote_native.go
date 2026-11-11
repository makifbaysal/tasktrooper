package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) remoteServerInfo(ctx context.Context) fiber.Map {
	return fiber.Map{
		"name":    h.remoteSvc.ServerName(ctx),
		"version": h.remoteSvc.Version(),
		"edition": remoteEdition,
	}
}

func (h *Handler) remoteInfo(c *fiber.Ctx) error {
	info := h.remoteServerInfo(c.UserContext())
	info["api_version"] = remoteAPIVersion
	info["capabilities"] = remoteCapabilities
	return c.JSON(info)
}

type remotePairRequest struct {
	Code       string `json:"code"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

func (h *Handler) remotePair(c *fiber.Ctx) error {
	var req remotePairRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return remoteError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	token, dev, err := h.remoteSvc.Exchange(c.UserContext(), req.Code, req.DeviceName, req.Platform)
	var rl *remoteaccess.RateLimitError
	switch {
	case errors.As(err, &rl):
		return remoteRateLimited(c, rl.RetryAfter)
	case errors.Is(err, remoteaccess.ErrInvalidArgument):
		return remoteError(c, fiber.StatusBadRequest, "invalid_request", "code and device_name are required")
	case errors.Is(err, remoteaccess.ErrInvalidPairing):
		return remoteError(c, fiber.StatusUnauthorized, "invalid_pairing_code", "the pairing code is invalid, expired or already used")
	case err != nil:
		log.Error().Err(err).Msg("remote pairing")
		return remoteError(c, fiber.StatusInternalServerError, "internal_error", "internal error")
	}
	return c.JSON(fiber.Map{
		"token":      token,
		"device_id":  dev.ID,
		"role":       dev.Role,
		"expires_at": dev.ExpiresAt,
		"server":     h.remoteServerInfo(c.UserContext()),
	})
}

func (h *Handler) remoteNative(c *fiber.Ctx, rule *remoteRule, dev remoteDevice) error {
	switch rule.Method + " " + rule.Pattern {
	case "GET /remote/v1/me":
		d, err := h.remoteSvc.Device(c.UserContext(), dev.ID)
		if err != nil {
			return remoteError(c, fiber.StatusInternalServerError, "internal_error", "internal error")
		}
		return c.JSON(fiber.Map{
			"device_id": d.ID, "name": d.Name, "role": d.Role,
			"created_at": d.CreatedAt, "expires_at": d.ExpiresAt,
			"server": h.remoteServerInfo(c.UserContext()),
		})
	case "DELETE /remote/v1/me":
		if err := h.remoteSvc.Revoke(c.UserContext(), dev.ID); err != nil && !errors.Is(err, remoteaccess.ErrNotFound) {
			return remoteError(c, fiber.StatusInternalServerError, "internal_error", "internal error")
		}
		return c.SendStatus(fiber.StatusNoContent)
	case "GET /remote/v1/changes":
		return h.remoteChanges(c)
	case "GET /remote/v1/pending":
		return h.remotePending(c)
	case "GET /remote/v1/sessions/:id/turn":
		return h.remoteTurn(c)
	case "POST /remote/v1/sessions/:id/messages":
		return h.remoteAsyncMessage(c, dev)
	}
	return remoteError(c, fiber.StatusNotFound, "route_not_allowed", "unknown remote route")
}

// ---- changes feed ----

const (
	remoteChangesMaxEvents = 200
	remoteChangesMaxWait   = 25
	remoteChangesPoll      = 1500 * time.Millisecond
)

func (h *Handler) remoteChanges(c *fiber.Ctx) error {
	ctx := c.UserContext()
	var since time.Time
	hasSince := false
	if raw := c.Query("since"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return remoteError(c, fiber.StatusBadRequest, "invalid_request", "since must be a cursor from a previous answer")
		}
		since, hasSince = t, true
	}
	wait, _ := strconv.Atoi(c.Query("wait", "0"))
	if wait < 0 {
		wait = 0
	}
	if wait > remoteChangesMaxWait {
		wait = remoteChangesMaxWait
	}

	if !hasSince {
		cursor := h.remoteLatest(ctx)
		return c.JSON(fiber.Map{"cursor": cursor.UTC().Format(time.RFC3339Nano), "events": []domain.BoardEvent{}, "sessions": []uuid.UUID{}})
	}

	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	for {
		events, sessions, cursor, truncated, err := h.remoteChangesSince(ctx, since)
		if err != nil {
			return remoteError(c, fiber.StatusInternalServerError, "internal_error", "internal error")
		}
		if len(events) > 0 || len(sessions) > 0 || !time.Now().Add(remoteChangesPoll).Before(deadline) {
			out := fiber.Map{"cursor": cursor.UTC().Format(time.RFC3339Nano), "events": events, "sessions": sessions}
			if truncated {
				out["truncated"] = true
			}
			return c.JSON(out)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(remoteChangesPoll):
		}
	}
}

func (h *Handler) remoteLatest(ctx context.Context) time.Time {
	latest := time.Now()
	found := false
	if h.boardEvents != nil {
		if evs, err := h.boardEvents.ListRecent(ctx, 1); err == nil && len(evs) > 0 {
			latest, found = evs[0].CreatedAt, true
		}
	}
	if h.sessionSvc != nil {
		if ss, err := h.sessionSvc.List(ctx, 1, 0); err == nil && len(ss) > 0 && (!found || ss[0].UpdatedAt.After(latest)) {
			latest, found = ss[0].UpdatedAt, true
		}
	}
	return latest
}

func (h *Handler) remoteChangesSince(ctx context.Context, since time.Time) ([]domain.BoardEvent, []uuid.UUID, time.Time, bool, error) {
	cursor := since
	events := []domain.BoardEvent{}
	sessions := []uuid.UUID{}
	truncated := false
	if h.boardEvents != nil {
		recent, err := h.boardEvents.ListRecent(ctx, remoteChangesMaxEvents+1)
		if err != nil {
			return nil, nil, since, false, err
		}
		for i := len(recent) - 1; i >= 0; i-- {
			if recent[i].CreatedAt.After(since) {
				events = append(events, recent[i])
			}
		}
		if len(recent) > remoteChangesMaxEvents && len(events) > remoteChangesMaxEvents {
			events = events[len(events)-remoteChangesMaxEvents:]
			truncated = true
		}
		for _, e := range events {
			if e.CreatedAt.After(cursor) {
				cursor = e.CreatedAt
			}
		}
	}
	if h.sessionSvc != nil {
		recent, err := h.sessionSvc.List(ctx, 50, 0)
		if err != nil {
			return nil, nil, since, false, err
		}
		for _, s := range recent {
			if s.UpdatedAt.After(since) {
				sessions = append(sessions, s.ID)
				if s.UpdatedAt.After(cursor) {
					cursor = s.UpdatedAt
				}
			}
		}
	}
	return events, sessions, cursor, truncated, nil
}

// ---- pending: chats waiting for the person ----

type remotePendingQuestion struct {
	SessionID     uuid.UUID                    `json:"session_id"`
	Title         string                       `json:"title,omitempty"`
	AgentID       *uuid.UUID                   `json:"agent_id,omitempty"`
	MessageID     uuid.UUID                    `json:"message_id"`
	AskedAt       time.Time                    `json:"asked_at"`
	Clarification *domain.ClarificationRequest `json:"clarification"`
}

// remotePending lists chats whose latest message is an agent question the
// person has not answered yet (ask_user). Board-task questions are on the
// tasks themselves (blocked_resource analysis_questions).
func (h *Handler) remotePending(c *fiber.Ctx) error {
	out := []remotePendingQuestion{}
	if h.sessionSvc == nil {
		return c.JSON(fiber.Map{"questions": out})
	}
	ctx := c.UserContext()
	recent, err := h.sessionSvc.List(ctx, 15, 0)
	if err != nil {
		return remoteError(c, fiber.StatusInternalServerError, "internal_error", "internal error")
	}
	cutoff := time.Now().Add(-14 * 24 * time.Hour)
	for _, s := range recent {
		if s.UpdatedAt.Before(cutoff) {
			continue
		}
		_, msgs, err := h.sessionSvc.Get(ctx, s.ID)
		if err != nil || len(msgs) == 0 {
			continue
		}
		last := msgs[len(msgs)-1]
		if last.Role == domain.RoleAssistant && last.Clarification != nil && len(last.Clarification.Questions) > 0 {
			out = append(out, remotePendingQuestion{
				SessionID: s.ID, Title: s.Title, AgentID: s.AgentID, MessageID: last.ID,
				AskedAt: last.CreatedAt, Clarification: last.Clarification,
			})
		}
	}
	return c.JSON(fiber.Map{"questions": out})
}

// ---- async chat turns ----

type remoteTurn struct {
	State      string     `json:"state"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Error      *string    `json:"error"`
}

type remoteTurnTracker struct {
	mu    sync.Mutex
	turns map[uuid.UUID]*remoteTurn
}

var remoteTurns = &remoteTurnTracker{turns: map[uuid.UUID]*remoteTurn{}}

func (t *remoteTurnTracker) begin(id uuid.UUID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur := t.turns[id]; cur != nil && cur.State == "running" {
		return false
	}
	if len(t.turns) > 500 {
		cut := time.Now().Add(-time.Hour)
		for k, v := range t.turns {
			if v.State != "running" && v.FinishedAt != nil && v.FinishedAt.Before(cut) {
				delete(t.turns, k)
			}
		}
	}
	now := time.Now().UTC()
	t.turns[id] = &remoteTurn{State: "running", StartedAt: &now}
	return true
}

func (t *remoteTurnTracker) finish(id uuid.UUID, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur := t.turns[id]
	if cur == nil {
		return
	}
	now := time.Now().UTC()
	cur.State, cur.FinishedAt = "idle", &now
	if err != nil {
		msg := err.Error()
		cur.Error = &msg
	}
}

func (t *remoteTurnTracker) get(id uuid.UUID) remoteTurn {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur := t.turns[id]; cur != nil {
		return *cur
	}
	return remoteTurn{State: "idle"}
}

func (h *Handler) remoteTurn(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return remoteError(c, fiber.StatusBadRequest, "invalid_request", "invalid session id")
	}
	return c.JSON(remoteTurns.get(id))
}

func (h *Handler) remoteAsyncMessage(c *fiber.Ctx, dev remoteDevice) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return remoteError(c, fiber.StatusBadRequest, "invalid_request", "invalid session id")
	}
	var req domain.SessionMessageRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return remoteError(c, fiber.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid request body: %v", err))
	}
	if req.Content == "" {
		return remoteError(c, fiber.StatusBadRequest, "invalid_request", "content is required")
	}
	req.Role, req.Stream = domain.RoleUser, false
	if err := h.budgetGate(c); err != nil {
		return err
	}
	if !remoteTurns.begin(id) {
		return remoteError(c, fiber.StatusConflict, "turn_in_progress", "this chat already has a turn running")
	}
	policy := h.resolvePolicy(c, req.ToolPolicy)
	ctx := context.WithoutCancel(h.enrichContext(c))
	go func() {
		_, err := h.sessionSvc.SendMessage(ctx, id, req, policy)
		if _, queued := domain.QuotaBlockOf(err); queued || errors.Is(err, domain.ErrRunCancelled) {
			err = nil
		}
		remoteTurns.finish(id, err)
		if err != nil {
			log.Warn().Err(err).Str("session_id", id.String()).Str("device", dev.Name).Msg("remote chat turn failed")
		}
	}()
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"accepted": true, "session_id": id})
}
