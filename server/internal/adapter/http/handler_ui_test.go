package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// activeRunsActivityStore's ListActiveRuns returns whatever `runs` holds; the
// nil zero value is the natural result of a Go query that matched nothing —
// exactly what the desktop client actually receives in production when no
// run is in flight.
type activeRunsActivityStore struct {
	port.ActivityStore
	runs []domain.SessionRun
}

func (s *activeRunsActivityStore) ListActiveRuns(context.Context) ([]domain.SessionRun, error) {
	return s.runs, nil
}

// The desktop main process (NotificationWatcher#syncSuspensionGuard) decodes
// this body straight into a slice; a bare JSON `null` there crashed the poll
// loop every tick and left the prevent-app-suspension blocker held forever.
// The wire contract has to be an empty array, never null.
func TestActiveRunsSerializesEmptyRunsAsArrayNotNull(t *testing.T) {
	h := &Handler{sessionSvc: session.NewService(nil, &activeRunsActivityStore{}, nil, nil, nil, 0, nil, nil)}
	app := fiber.New()
	app.Get("/v1/activity/active", h.ActiveRuns)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/activity/active", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var raw map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
	require.JSONEq(t, "[]", string(raw["runs"]))
	require.JSONEq(t, "0", string(raw["count"]))
}

// The desktop shell's chat-turn notification builds a title and an
// /agents/:agentId/chat/:sessionId route straight from this endpoint, so the
// owning session's agent_id and title have to ride along on the run.
func TestActiveRunsIncludesAgentIDAndTitleFromTheOwningSession(t *testing.T) {
	sessionID := uuid.New()
	agentID := uuid.New()
	run := domain.SessionRun{
		ID:        uuid.New(),
		SessionID: &sessionID,
		AgentID:   &agentID,
		Title:     "Refactor the auth flow",
		RequestID: "req-1",
		Status:    domain.SessionRunStatusRunning,
		StartedAt: time.Now(),
	}
	h := &Handler{sessionSvc: session.NewService(nil, &activeRunsActivityStore{runs: []domain.SessionRun{run}}, nil, nil, nil, 0, nil, nil)}
	app := fiber.New()
	app.Get("/v1/activity/active", h.ActiveRuns)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/activity/active", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var body struct {
		Runs []domain.SessionRun `json:"runs"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body.Runs, 1)
	require.Equal(t, agentID, *body.Runs[0].AgentID)
	require.Equal(t, "Refactor the auth flow", body.Runs[0].Title)
}
