package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"

	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
)

const (
	remoteDeviceLocal = "remote_device"
	remoteAPIVersion  = 1
	remoteBodyLimit   = 1 << 20
	remoteEdition     = "local"
)

type remoteDevice struct {
	ID   uuid.UUID
	Name string
	Role remoteaccess.Role
}

// remoteCapabilities is what GET /remote/v1/info advertises; clients gate
// features on it. Each name is a group of the allowlist in remote_allowlist.go.
var remoteCapabilities = []string{
	"read", "comments", "task_move", "task_create", "task_detail",
	"chat", "chat_async", "chat_delete", "questions", "run_control",
	"changes", "changes_longpoll", "pending",
	"agents_detail", "roles_read", "workflows_read", "task_types_read",
	"agent_cli_status", "llm_providers_read",
	"github_status", "github_import", "github_device_flow",
	"projects_write", "attachments_read",
}

// remoteListener is the listener a tunnel points at. It is a second fiber app
// on its own loopback port, never the UI's: the only thing it can do is run
// remoteGate, which authenticates a device, matches the allowlist and hands
// the request to the local API's own router.
type remoteListener struct {
	mu   sync.Mutex
	app  *fiber.App
	ln   net.Listener
	port int
}

// EnableRemote starts the remote listener (idempotent) and returns its port.
func (h *Handler) EnableRemote(preferredPort int) (int, error) {
	if h.remoteSvc == nil {
		return 0, errors.New("remote access is not available")
	}
	if h.mainApp == nil {
		return 0, errors.New("routes are not registered yet")
	}
	if h.remote == nil {
		h.remote = &remoteListener{}
	}
	r := h.remote
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.app != nil {
		return r.port, nil
	}
	var ln net.Listener
	var err error
	if preferredPort > 0 && preferredPort < 65536 {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	}
	if ln == nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return 0, err
		}
	}
	app := h.newRemoteApp(h.mainApp.Handler())
	go func() {
		if err := app.Listener(ln); err != nil {
			log.Error().Err(err).Msg("remote listener stopped")
		}
	}()
	r.app, r.ln, r.port = app, ln, ln.Addr().(*net.TCPAddr).Port
	log.Info().Int("port", r.port).Msg("remote access listener started")
	return r.port, nil
}

// DisableRemote stops the remote listener. Paired devices stay.
func (h *Handler) DisableRemote() {
	if h.remote == nil {
		return
	}
	r := h.remote
	r.mu.Lock()
	app, ln := r.app, r.ln
	r.app, r.ln, r.port = nil, nil, 0
	r.mu.Unlock()
	if app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = app.ShutdownWithContext(ctx)
		// Shutdown only closes a listener the server already picked up; one
		// stopped before its goroutine ran would keep the port.
		_ = ln.Close()
		log.Info().Msg("remote access listener stopped")
	}
}

func (h *Handler) remotePort() int {
	if h.remote == nil {
		return 0
	}
	h.remote.mu.Lock()
	defer h.remote.mu.Unlock()
	return h.remote.port
}

func (h *Handler) newRemoteApp(forward fasthttp.RequestHandler) *fiber.App {
	app := fiber.New(fiber.Config{
		CaseSensitive:         true,
		StrictRouting:         true,
		DisableStartupMessage: true,
		BodyLimit:             remoteBodyLimit,
		ReadBufferSize:        16 << 10,
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          30 * time.Minute,
		IdleTimeout:           90 * time.Second,
		Concurrency:           512,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				code = fe.Code
			}
			if code == fiber.StatusRequestEntityTooLarge {
				return remoteError(c, code, "payload_too_large", "request body is over 1 MiB")
			}
			if code >= 500 {
				log.Error().Err(err).Int("status", code).Msg("remote listener error")
			}
			return remoteError(c, code, "request_error", "request failed")
		},
	})
	app.Use(func(c *fiber.Ctx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("remote listener panic")
				err = remoteError(c, fiber.StatusInternalServerError, "internal_error", "internal error")
			}
		}()
		return h.remoteGate(c, forward)
	})
	return app
}

func remoteError(c *fiber.Ctx, status int, code, msg string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"message": msg, "type": code},
		"code":  code,
	})
}

func (h *Handler) remoteGate(c *fiber.Ctx, forward fasthttp.RequestHandler) error {
	svc := h.remoteSvc
	c.Set("X-Content-Type-Options", "nosniff")
	rid := c.Get("X-Request-ID")
	if rid == "" || len(rid) > 64 || !urlSafeToken(rid) {
		rid = uuid.NewString()
	}
	c.Set("X-Request-ID", rid)
	c.Locals("request_id", rid)
	svc.NoteRequest()

	method, path := reqMethod(c), c.Path()
	if method == fiber.MethodGet && path == remoteInfoPath {
		return h.remoteInfo(c)
	}
	if method == fiber.MethodPost && path == remotePairPath {
		return h.remotePair(c)
	}

	ctx := c.UserContext()
	dev, aerr := svc.Authenticate(ctx, bearerToken(c))
	if aerr != nil {
		return h.remoteAuthFailure(c, aerr)
	}
	rd := remoteDevice{ID: dev.ID, Name: dev.Name, Role: dev.Role}

	rule, ok := matchRemoteRule(method, path)
	if !ok {
		svc.Audit(ctx, remoteaccess.AuditEntry{Kind: remoteaccess.AuditRequest, DeviceID: &rd.ID, DeviceName: rd.Name, Method: method, Route: "(not allowed)", Status: 403})
		return remoteError(c, fiber.StatusForbidden, "route_not_allowed", "this route is not part of the remote API")
	}
	if allowed, retry := svc.Allow(rd.ID); !allowed {
		svc.Audit(ctx, remoteaccess.AuditEntry{Kind: remoteaccess.AuditRateLimited, DeviceID: &rd.ID, DeviceName: rd.Name, Method: method, Route: rule.Pattern, Status: 429})
		return remoteRateLimited(c, retry)
	}
	if rule.Write && !rule.SelfService && rd.Role != remoteaccess.RoleOperator {
		return h.remoteAudited(c, rd, rule, remoteError(c, fiber.StatusForbidden, "read_only_device", "this device is view-only"))
	}
	if rule.Check != nil {
		if refusal := rule.Check(c.Body()); refusal != nil {
			return h.remoteAudited(c, rd, rule, remoteError(c, fiber.StatusForbidden, "field_not_allowed", refusal.Message))
		}
	}

	c.Locals("client_name", "remote:"+rd.Name)
	if rule.Native {
		return h.remoteAudited(c, rd, rule, h.remoteNative(c, rule, rd))
	}

	c.Locals(remoteDeviceLocal, rd)
	c.Request().Header.Del(fiber.HeaderAuthorization)
	forward(c.Context())
	if len(c.Response().Header.Peek(fiber.HeaderCacheControl)) == 0 {
		c.Set(fiber.HeaderCacheControl, "no-store")
	}
	return h.remoteAudited(c, rd, rule, nil)
}

// remoteAudited records every non-GET once its status is known. Reads are not
// listed one by one (a phone polls); they only move the device's last-seen.
func (h *Handler) remoteAudited(c *fiber.Ctx, d remoteDevice, rule *remoteRule, err error) error {
	if m := reqMethod(c); m != fiber.MethodGet {
		status := c.Response().StatusCode()
		var fe *fiber.Error
		if errors.As(err, &fe) {
			status = fe.Code
		}
		id := d.ID
		h.remoteSvc.Audit(context.WithoutCancel(c.UserContext()), remoteaccess.AuditEntry{
			Kind: remoteaccess.AuditRequest, DeviceID: &id, DeviceName: d.Name,
			Method: m, Route: rule.Pattern, Status: status,
		})
	}
	return err
}

// reqMethod copies the method: fiber hands out strings that alias fasthttp's
// reused request buffers, and an audit entry outlives the request.
func reqMethod(c *fiber.Ctx) string { return strings.Clone(c.Method()) }

func bearerToken(c *fiber.Ctx) string {
	auth := c.Get(fiber.HeaderAuthorization)
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}

func (h *Handler) remoteAuthFailure(c *fiber.Ctx, err error) error {
	svc := h.remoteSvc
	ctx := c.UserContext()
	c.Set(fiber.HeaderWWWAuthenticate, "Bearer")
	switch {
	case errors.Is(err, remoteaccess.ErrTokenExpired):
		svc.Audit(ctx, remoteaccess.AuditEntry{Kind: remoteaccess.AuditAuthFailed, Method: reqMethod(c), Route: "token_expired", Status: 401})
		return remoteError(c, fiber.StatusUnauthorized, "token_expired", "this device's access has expired; pair it again")
	case errors.Is(err, remoteaccess.ErrTokenRevoked):
		svc.Audit(ctx, remoteaccess.AuditEntry{Kind: remoteaccess.AuditAuthFailed, Method: reqMethod(c), Route: "token_revoked", Status: 401})
		return remoteError(c, fiber.StatusUnauthorized, "token_revoked", "this device was revoked on the computer")
	case errors.Is(err, remoteaccess.ErrUnauthorized):
		if blocked, retry := svc.AuthFailureBlocked(); blocked {
			return remoteRateLimited(c, retry)
		}
		svc.NoteAuthFailure()
		svc.Audit(ctx, remoteaccess.AuditEntry{Kind: remoteaccess.AuditAuthFailed, Method: reqMethod(c), Route: "unauthorized", Status: 401})
		return remoteError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or missing device token")
	}
	log.Error().Err(err).Msg("remote auth")
	return remoteError(c, fiber.StatusInternalServerError, "internal_error", "internal error")
}

func remoteRateLimited(c *fiber.Ctx, retry time.Duration) error {
	secs := int(retry.Round(time.Second) / time.Second)
	if secs < 1 {
		secs = 1
	}
	c.Set(fiber.HeaderRetryAfter, strconv.Itoa(secs))
	return remoteError(c, fiber.StatusTooManyRequests, "rate_limited", "too many requests")
}
