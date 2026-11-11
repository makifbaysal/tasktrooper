package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// OpenTaskChat — POST /v1/repositories/:id/tasks/:taskId/chat
//
// Opens the chat thread about one board task and returns the session to talk in
// plus the agent answering there. Idempotent: the same task always resolves to the
// same thread, so the drawer's "discuss this task" button can be pressed as often
// as the human likes without splitting the conversation across chats.
//
// The response is deliberately just the two ids. The client already renders
// sessions from GET /v1/sessions/:id, and returning a whole session here would
// give it a second, staler shape of the same object to keep in sync.
func (h *Handler) OpenTaskChat(c *fiber.Ctx) error {
	if h.taskChat == nil {
		return taskChatDisabled(c)
	}
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	var req struct {
		AgentID string `json:"agent_id"`
	}
	_ = c.BodyParser(&req)
	wanted := uuid.Nil
	if req.AgentID != "" {
		parsed, parseErr := uuid.Parse(req.AgentID)
		if parseErr != nil {
			return badRequest(c, "invalid agent_id")
		}
		wanted = parsed
	}
	sessionID, agentID, err := h.taskChat.Open(h.enrichContext(c), repositoryID, taskID, wanted)
	if err != nil {
		// A task id the repository does not own comes back as not-found, which is
		// also the answer for an unknown repository: both mean "no such task
		// here", and distinguishing them would tell a caller whether a repository
		// it cannot see exists.
		if errors.Is(err, domain.ErrChatAgentUnavailable) {
			return badRequest(c, err.Error())
		}
		if errors.Is(err, domain.ErrBoardTaskNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(errorResponse{
				Error: errorDetail{Message: err.Error(), Type: "not_found"},
			})
		}
		return internalError(c, err)
	}
	// agent_id is always present in the shape and empty when the board has nobody
	// subscribed to the task's column: the chat still works (agentless), and the
	// client decides whether to show an agent name.
	agent := ""
	if agentID != uuid.Nil {
		agent = agentID.String()
	}
	return c.JSON(fiber.Map{
		"session_id": sessionID.String(),
		"agent_id":   agent,
	})
}

// taskChatDisabled: a build with no board (desktop without Postgres) has no tasks
// to open a chat about.
func taskChatDisabled(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "board task chat not enabled", Type: "service_unavailable"},
	})
}
