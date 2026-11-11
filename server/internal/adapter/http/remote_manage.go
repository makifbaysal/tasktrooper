package http

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
)

// The desktop's side of remote access: switch the remote listener on and off,
// pair phones, list and revoke them, read the audit trail. These sit on the
// ordinary loopback API behind the ordinary API key; the remote listener never
// forwards to them (they are not in the allowlist).
func (h *Handler) registerRemoteAccessRoutes(app *fiber.App) {
	if h.remoteSvc == nil {
		return
	}
	app.Get("/v1/remote-access", h.RemoteAccessStatus)
	app.Post("/v1/remote-access/enable", h.RemoteAccessEnable)
	app.Post("/v1/remote-access/disable", h.RemoteAccessDisable)
	app.Put("/v1/remote-access/settings", h.RemoteAccessSettings)
	app.Get("/v1/remote-access/live", h.RemoteAccessLive)
	app.Post("/v1/remote-access/pairings", h.RemoteAccessCreatePairing)
	app.Delete("/v1/remote-access/pairings/:id", h.RemoteAccessCancelPairing)
	app.Get("/v1/remote-access/devices", h.RemoteAccessDevices)
	app.Patch("/v1/remote-access/devices/:id", h.RemoteAccessUpdateDevice)
	app.Delete("/v1/remote-access/devices/:id", h.RemoteAccessRevokeDevice)
	app.Get("/v1/remote-access/audit", h.RemoteAccessAudit)
}

func (h *Handler) remoteStatus(c *fiber.Ctx) error {
	ctx := h.enrichContext(c)
	devices, pending, err := h.remoteSvc.Counts(ctx)
	if err != nil {
		return internalError(c, err)
	}
	port := h.remotePort()
	return c.JSON(fiber.Map{
		"enabled":          port != 0,
		"port":             port,
		"server_name":      h.remoteSvc.ServerName(ctx),
		"device_count":     devices,
		"pending_pairings": pending,
	})
}

func remoteManageError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, remoteaccess.ErrInvalidArgument):
		return badRequest(c, err.Error())
	case errors.Is(err, remoteaccess.ErrNotFound):
		return notFound(c, "not found")
	}
	return internalError(c, err)
}

func (h *Handler) RemoteAccessStatus(c *fiber.Ctx) error { return h.remoteStatus(c) }

func (h *Handler) RemoteAccessEnable(c *fiber.Ctx) error {
	var req struct {
		PreferredPort int `json:"preferred_port"`
	}
	_ = c.BodyParser(&req)
	if _, err := h.EnableRemote(req.PreferredPort); err != nil {
		return internalError(c, err)
	}
	return h.remoteStatus(c)
}

func (h *Handler) RemoteAccessDisable(c *fiber.Ctx) error {
	h.DisableRemote()
	return h.remoteStatus(c)
}

func (h *Handler) RemoteAccessSettings(c *fiber.Ctx) error {
	var req struct {
		ServerName *string `json:"server_name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.ServerName != nil {
		if err := h.remoteSvc.SetServerName(h.enrichContext(c), *req.ServerName); err != nil {
			return remoteManageError(c, err)
		}
	}
	return h.remoteStatus(c)
}

func (h *Handler) RemoteAccessLive(c *fiber.Ctx) error {
	n, last := h.remoteSvc.Live()
	return c.JSON(fiber.Map{"requests_last_minute": n, "last_request_at": last})
}

// BuildPairingURI is the text of the QR code (contract section 2).
func BuildPairingURI(publicURL, code, serverName string) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("u", strings.TrimRight(publicURL, "/"))
	q.Set("c", code)
	if serverName != "" {
		q.Set("n", serverName)
	}
	return "tasktrooper://pair?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

func (h *Handler) RemoteAccessCreatePairing(c *fiber.Ctx) error {
	var req struct {
		Role          remoteaccess.Role `json:"role"`
		ExpiresInDays int               `json:"expires_in_days"`
		PublicURL     string            `json:"public_url"`
		DeviceLabel   string            `json:"device_label"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Role == "" {
		req.Role = remoteaccess.RoleOperator
	}
	if h.remotePort() == 0 {
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: "remote access is off", Type: "remote_disabled"},
		})
	}
	u, err := url.Parse(req.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return badRequest(c, "public_url must be the tunnel's http(s) address")
	}
	ctx := h.enrichContext(c)
	p, err := h.remoteSvc.CreatePairing(ctx, req.Role, req.ExpiresInDays, req.DeviceLabel)
	if err != nil {
		return remoteManageError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":           p.ID,
		"code":         p.Code,
		"code_display": remoteaccess.DisplayCode(p.Code),
		"uri":          BuildPairingURI(req.PublicURL, p.Code, h.remoteSvc.ServerName(ctx)),
		"expires_at":   p.ExpiresAt,
	})
}

func (h *Handler) RemoteAccessCancelPairing(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid pairing id")
	}
	if err := h.remoteSvc.CancelPairing(h.enrichContext(c), id); err != nil {
		return remoteManageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) RemoteAccessDevices(c *fiber.Ctx) error {
	devices, err := h.remoteSvc.Devices(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"devices": devices})
}

func (h *Handler) RemoteAccessUpdateDevice(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid device id")
	}
	var req struct {
		Name *string            `json:"name"`
		Role *remoteaccess.Role `json:"role"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	d, err := h.remoteSvc.UpdateDevice(h.enrichContext(c), id, req.Name, req.Role)
	if err != nil {
		return remoteManageError(c, err)
	}
	return c.JSON(d)
}

func (h *Handler) RemoteAccessRevokeDevice(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid device id")
	}
	if err := h.remoteSvc.Revoke(h.enrichContext(c), id); err != nil {
		return remoteManageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) RemoteAccessAudit(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	var dev *uuid.UUID
	if raw := c.Query("device_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return badRequest(c, "invalid device_id")
		}
		dev = &id
	}
	entries, err := h.remoteSvc.AuditEntries(h.enrichContext(c), dev, limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"entries": entries})
}
