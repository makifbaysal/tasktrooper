package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// activeRunsActivityStore's ListActiveRuns returns a nil slice, the natural
// zero value of a Go query that matched nothing — exactly what the desktop
// client actually receives in production when no run is in flight.
type activeRunsActivityStore struct {
	port.ActivityStore
}

func (s *activeRunsActivityStore) ListActiveRuns(context.Context) ([]domain.SessionRun, error) {
	return nil, nil
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
