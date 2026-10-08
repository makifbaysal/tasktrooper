package http

import (
	"bytes"
	"errors"
	"html/template"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// MCPOAuthCallbackPath is the loopback redirect URI an MCP server's
// authorization server sends the browser back to. It is named in three
// places — the route, isPublicPath and the redirect URI the runtime builds —
// all through this constant, so they cannot drift apart.
const MCPOAuthCallbackPath = "/oauth/mcp/callback"

func (h *Handler) StartMCPOAuth(c *fiber.Ctx) error {
	if h.mcpSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "mcp management requires postgres", Type: "service_unavailable"},
		})
	}
	var req domain.MCPOAuthStartRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return badRequest(c, "invalid request body")
		}
	}
	// Cloned: fiber reuses the request buffer behind Params, and the id
	// outlives this request inside the pending sign-in.
	resp, err := h.mcpSvc.StartOAuth(h.enrichContext(c), strings.Clone(c.Params("id")), req)
	if err != nil {
		return mcpHandlerError(c, err)
	}
	return c.JSON(resp)
}

func (h *Handler) DisconnectMCPOAuth(c *fiber.Ctx) error {
	if h.mcpSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "mcp management requires postgres", Type: "service_unavailable"},
		})
	}
	if err := h.mcpSvc.DisconnectOAuth(h.enrichContext(c), c.Params("id")); err != nil {
		return mcpHandlerError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// MCPOAuthCallback is the one route outside /v1 that changes state without
// the API token: the browser that comes back from the authorization server
// holds none. What authenticates it is the state — 256 random bits, bound to
// one server and its PKCE verifier, single-use and gone after ten minutes —
// and the code is worthless without that verifier, which never left this
// process. Neither the code nor the state is logged.
func (h *Handler) MCPOAuthCallback(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set("Referrer-Policy", "no-referrer")
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderXFrameOptions, "DENY")
	c.Set(fiber.HeaderContentSecurityPolicy, "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")

	if h.mcpSvc == nil {
		return renderOAuthCallback(c, fiber.StatusServiceUnavailable, oauthCallbackPage{Failed: true})
	}
	serverID, err := h.mcpSvc.CompleteOAuth(h.enrichContext(c), domain.MCPOAuthCallback{
		State:  c.Query("state"),
		Code:   c.Query("code"),
		Issuer: c.Query("iss"),
		Error:  c.Query("error"),
	})
	if err != nil {
		log.Warn().Err(err).Str("server", serverID).Msg("mcp oauth sign-in did not complete")
		page := oauthCallbackPage{Failed: true, ServerID: serverID, Reason: oauthFailureReason(err)}
		return renderOAuthCallback(c, fiber.StatusBadRequest, page)
	}
	log.Info().Str("server", serverID).Msg("mcp oauth sign-in completed")
	return renderOAuthCallback(c, fiber.StatusOK, oauthCallbackPage{ServerID: serverID})
}

type oauthCallbackPage struct {
	Failed   bool
	ServerID string
	Reason   string
}

func oauthFailureReason(err error) string {
	switch {
	case errors.Is(err, domain.ErrMCPOAuthState):
		return "This sign-in link is unknown, expired or was already used. Start the connection again from TaskTrooper."
	case errors.Is(err, domain.ErrMCPOAuthDenied):
		return "Access was not granted."
	case errors.Is(err, domain.ErrMCPOAuthExchange):
		return "The authorization server refused to issue a token."
	default:
		return "Something went wrong while saving the sign-in."
	}
}

// The page is self-contained on purpose: the CSP above forbids every external
// resource and every script, so it renders the same with no network at all.
var oauthCallbackTemplate = template.Must(template.New("oauth").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .Failed}}Not connected{{else}}Connected{{end}} — TaskTrooper</title>
<style>
:root { color-scheme: light dark; }
body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: #f6f7f9; color: #1c1f24; }
main { max-width: 28rem; padding: 2rem; text-align: center; }
h1 { font-size: 1.25rem; margin: 0 0 .5rem; }
p { margin: .25rem 0; line-height: 1.5; color: #5b616b; }
code { font-size: .95em; }
@media (prefers-color-scheme: dark) {
  body { background: #15171a; color: #e8eaed; }
  p { color: #a4a9b1; }
}
</style>
</head>
<body>
<main>
{{if .Failed}}
<h1>Could not connect{{if .ServerID}} <code>{{.ServerID}}</code>{{end}}</h1>
<p>{{.Reason}}</p>
{{else}}
<h1>Connected — you can close this tab</h1>
<p>TaskTrooper is now signed in to <code>{{.ServerID}}</code>.</p>
{{end}}
</main>
</body>
</html>
`))

func renderOAuthCallback(c *fiber.Ctx, status int, page oauthCallbackPage) error {
	if page.Failed && page.Reason == "" {
		page.Reason = "OAuth sign-in is not available on this server."
	}
	var buf bytes.Buffer
	if err := oauthCallbackTemplate.Execute(&buf, page); err != nil {
		return internalError(c, err)
	}
	c.Type("html", "utf-8")
	return c.Status(status).Send(buf.Bytes())
}
