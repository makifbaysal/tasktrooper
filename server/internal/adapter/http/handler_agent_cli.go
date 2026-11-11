package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The local agent CLI surface. Deliberately not under /v1/llm/providers: those
// routes all mean "dial this base URL with this key" and refuse host-executed
// providers for that reason. Connecting a CLI is verifying a binary on this
// host and writing a catalog for it — a different act, so a different path.
// See application/agentcli's package comment.
func (h *Handler) registerAgentCLIRoutes(app *fiber.App) {
	app.Get("/v1/agent-cli", h.GetAgentCLIState)
	app.Post("/v1/agent-cli/:flavor/connect", h.ConnectAgentCLI)
	app.Delete("/v1/agent-cli/:flavor", h.DisconnectAgentCLI)
	app.Put("/v1/agent-cli/:flavor/default-model", h.SetAgentCLIDefaultModel)
}

func (h *Handler) SetAgentCLIDefaultModel(c *fiber.Ctx) error {
	if h.agentCLISvc == nil {
		return agentCLIDisabled(c)
	}
	flavor := domain.AgentCLIFlavor(c.Params("flavor"))
	if !domain.ValidAgentCLIFlavor(flavor) {
		return codedBadRequest(c, codeUnknownAgentCLIFlavor, "unknown agent cli flavor")
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
			Error: errorDetail{Message: "invalid request body", Type: "invalid_request"},
		})
	}
	out, err := h.agentCLISvc.SetDefaultModel(h.enrichContext(c), flavor, body.Model)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) GetAgentCLIState(c *fiber.Ctx) error {
	if h.agentCLISvc == nil {
		return agentCLIDisabled(c)
	}
	out, err := h.agentCLISvc.State(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) ConnectAgentCLI(c *fiber.Ctx) error {
	if h.agentCLISvc == nil {
		return agentCLIDisabled(c)
	}
	flavor := domain.AgentCLIFlavor(c.Params("flavor"))
	if !domain.ValidAgentCLIFlavor(flavor) {
		return codedBadRequest(c, codeUnknownAgentCLIFlavor, "unknown agent cli flavor")
	}
	out, err := h.agentCLISvc.Connect(h.enrichContext(c), flavor)
	if err != nil {
		return agentCLIError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) DisconnectAgentCLI(c *fiber.Ctx) error {
	if h.agentCLISvc == nil {
		return agentCLIDisabled(c)
	}
	flavor := domain.AgentCLIFlavor(c.Params("flavor"))
	if !domain.ValidAgentCLIFlavor(flavor) {
		return codedBadRequest(c, codeUnknownAgentCLIFlavor, "unknown agent cli flavor")
	}
	out, err := h.agentCLISvc.Disconnect(h.enrichContext(c), flavor)
	if err != nil {
		return agentCLIError(c, err)
	}
	return c.JSON(out)
}

func agentCLIDisabled(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "local agent CLI management not enabled", Type: "service_unavailable"},
	})
}

// agentCLIError answers a connect/disconnect failure with a STATUS and a TYPE
// the client can branch on.
//
// The type is what makes the difference visible. "Connect failed" is the same
// sentence for a CLI that was never installed and a CLI that is installed and
// signed out, and those have different fixes — one sends the user to an
// installer, the other to a login. A client that can only show one message
// shows the wrong one half the time, and it cannot do better unless the server
// tells it which happened.
//
// None of these is a 500. Nothing here is the server failing: the binary's
// absence, its signed-out state and an unavailable provider are all facts about
// the request's target, and none of them changes on a retry. The unavailable
// provider is the one that is not a 400 either — it goes through
// permanentRefusal so that naming a not-yet-built provider gets the same 409 on
// every route of this server and of the control plane, rather than a status per
// endpoint.
func agentCLIError(c *fiber.Ctx, err error) error {
	if handled, writeErr := permanentRefusal(c, err); handled {
		return writeErr
	}
	switch {
	case errors.Is(err, domain.ErrAgentCLIBinaryMissing):
		return agentCLIStatus(c, err, "agent_cli_binary_missing")
	case errors.Is(err, domain.ErrAgentCLIUnauthenticated):
		return agentCLIStatus(c, err, "agent_cli_unauthenticated")
	default:
		return internalError(c, err)
	}
}

func agentCLIStatus(c *fiber.Ctx, err error, kind string) error {
	return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
		Error: errorDetail{Message: err.Error(), Type: kind},
	})
}
