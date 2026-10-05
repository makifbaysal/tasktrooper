package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// stepsActivityStore is session_steps: rows keyed by session_runs.id, which is
// the only id they were ever written under.
type stepsActivityStore struct {
	port.ActivityStore
	bySessionRun map[uuid.UUID][]domain.SessionStep
}

func (s *stepsActivityStore) ListStepsByRun(_ context.Context, runID uuid.UUID) ([]domain.SessionStep, error) {
	return s.bySessionRun[runID], nil
}

func (s *stepsActivityStore) ListStepsByRunSince(_ context.Context, runID uuid.UUID, since time.Time) ([]domain.SessionStep, error) {
	var out []domain.SessionStep
	for _, st := range s.bySessionRun[runID] {
		if !st.CreatedAt.Before(since) {
			out = append(out, st)
		}
	}
	return out, nil
}

// stepsTaskRunStore is task_agent_runs: the table the board's run list is built
// from, and therefore the id its clients send.
type stepsTaskRunStore struct {
	port.TaskAgentRunStore
	rows map[uuid.UUID]domain.TaskAgentRun
}

func (s *stepsTaskRunStore) GetByID(_ context.Context, id uuid.UUID) (domain.TaskAgentRun, error) {
	run, ok := s.rows[id]
	if !ok {
		return domain.TaskAgentRun{}, domain.ErrTaskAgentRunNotFound
	}
	return run, nil
}

func newRunStepsApp(h *Handler) *fiber.App {
	app := fiber.New()
	app.Get("/v1/runs/:id/steps", h.RunSteps)
	return app
}

func getRunSteps(t *testing.T, app *fiber.App, runID uuid.UUID) []domain.SessionStep {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/runs/"+runID.String()+"/steps", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	var body struct {
		Steps []domain.SessionStep `json:"steps"`
		Count int                  `json:"count"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, len(body.Steps), body.Count)
	return body.Steps
}

// The panel behind this endpoint was empty for every board run ever: steps are
// written under session_runs.id, the board holds task_agent_runs.id, and both
// are UUIDs — so the wrong id parsed fine and selected nothing. The endpoint has
// to answer for either id.
func TestRunStepsResolvesEitherRunID(t *testing.T) {
	sessionRunID := uuid.New()
	taskRunID := uuid.New()
	steps := []domain.SessionStep{
		{ID: uuid.New(), RunID: sessionRunID, StepType: "llm_request", Payload: json.RawMessage(`{}`)},
		{ID: uuid.New(), RunID: sessionRunID, StepType: "assistant_message", Payload: json.RawMessage(`{}`)},
	}
	pendingRunID := uuid.New()

	activity := &stepsActivityStore{bySessionRun: map[uuid.UUID][]domain.SessionStep{sessionRunID: steps}}
	taskRuns := &stepsTaskRunStore{rows: map[uuid.UUID]domain.TaskAgentRun{
		taskRunID: {ID: taskRunID, SessionRunID: &sessionRunID, Status: domain.TaskAgentRunStatusRunning},
		// A run still queued has no session run behind it yet.
		pendingRunID: {ID: pendingRunID, Status: domain.TaskAgentRunStatusPending},
	}}
	app := newRunStepsApp(&Handler{
		sessionSvc: session.NewService(nil, activity, nil, nil, nil, 0, nil, nil),
		taskRuns:   taskRuns,
	})

	t.Run("a board run id resolves through its session run", func(t *testing.T) {
		got := getRunSteps(t, app, taskRunID)
		require.Len(t, got, 2)
		require.Equal(t, "llm_request", got[0].StepType)
	})

	t.Run("a session run id still answers directly", func(t *testing.T) {
		require.Len(t, getRunSteps(t, app, sessionRunID), 2)
	})

	t.Run("a board run that never started is empty, not an error", func(t *testing.T) {
		require.Empty(t, getRunSteps(t, app, pendingRunID))
	})

	t.Run("an id in neither table is empty, not an error", func(t *testing.T) {
		require.Empty(t, getRunSteps(t, app, uuid.New()))
	})
}

// planCatalogStore is orchestration_plans, whose run_id is a session_runs.id —
// the same key session_steps uses, reached from the panel by the same id.
type planCatalogStore struct {
	port.CatalogStore
	bySessionRun map[uuid.UUID]domain.PlanView
}

func (s *planCatalogStore) GetPlanByRunID(_ context.Context, runID uuid.UUID) (domain.PlanView, error) {
	plan, ok := s.bySessionRun[runID]
	if !ok {
		return domain.PlanView{}, errors.New("plan not found")
	}
	return plan, nil
}

// /v1/runs/:id/plan is the same panel's other half and reads a table keyed the
// same way, so it had the same bug and takes the same resolution.
func TestRunPlanResolvesEitherRunID(t *testing.T) {
	sessionRunID := uuid.New()
	taskRunID := uuid.New()
	pendingRunID := uuid.New()

	plans := &planCatalogStore{bySessionRun: map[uuid.UUID]domain.PlanView{
		sessionRunID: {ID: uuid.New(), RunID: sessionRunID, Status: domain.PlanStatusRunning},
	}}
	taskRuns := &stepsTaskRunStore{rows: map[uuid.UUID]domain.TaskAgentRun{
		taskRunID:    {ID: taskRunID, SessionRunID: &sessionRunID, Status: domain.TaskAgentRunStatusRunning},
		pendingRunID: {ID: pendingRunID, Status: domain.TaskAgentRunStatusPending},
	}}
	h := &Handler{catalogSvc: catalog.NewService(plans, nil, ""), taskRuns: taskRuns}
	app := fiber.New()
	app.Get("/v1/runs/:id/plan", h.GetRunPlan)

	planStatus := func(t *testing.T, id uuid.UUID) (int, string) {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest("GET", "/v1/runs/"+id.String()+"/plan", nil))
		require.NoError(t, err)
		var body domain.PlanView
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body.Status
	}

	code, status := planStatus(t, taskRunID)
	require.Equal(t, fiber.StatusOK, code)
	require.Equal(t, domain.PlanStatusRunning, status)

	code, status = planStatus(t, sessionRunID)
	require.Equal(t, fiber.StatusOK, code)
	require.Equal(t, domain.PlanStatusRunning, status)

	// A run that never started has no plan, which is the 404 this endpoint has
	// always answered with for a run without one.
	code, _ = planStatus(t, pendingRunID)
	require.Equal(t, fiber.StatusNotFound, code)
}

// Without a task-agent-run store wired (no Postgres) the endpoint keeps its
// original single meaning rather than failing.
func TestRunStepsWithoutTaskRunStore(t *testing.T) {
	sessionRunID := uuid.New()
	activity := &stepsActivityStore{bySessionRun: map[uuid.UUID][]domain.SessionStep{
		sessionRunID: {{ID: uuid.New(), RunID: sessionRunID, StepType: "tool_call", Payload: json.RawMessage(`{}`)}},
	}}
	app := newRunStepsApp(&Handler{sessionSvc: session.NewService(nil, activity, nil, nil, nil, 0, nil, nil)})

	require.Len(t, getRunSteps(t, app, sessionRunID), 1)
	require.Empty(t, getRunSteps(t, app, uuid.New()))
}

func TestRunStepsSinceFiltersInclusively(t *testing.T) {
	runID := uuid.New()
	base := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	at := func(sec int) domain.SessionStep {
		return domain.SessionStep{ID: uuid.New(), RunID: runID, StepType: "x", Payload: json.RawMessage(`{}`), CreatedAt: base.Add(time.Duration(sec) * time.Second)}
	}
	activity := &stepsActivityStore{bySessionRun: map[uuid.UUID][]domain.SessionStep{runID: {at(0), at(1), at(2)}}}
	app := newRunStepsApp(&Handler{sessionSvc: session.NewService(nil, activity, nil, nil, nil, 0, nil, nil)})

	get := func(since string) (int, int) {
		resp, err := app.Test(httptest.NewRequest("GET", "/v1/runs/"+runID.String()+"/steps?since="+since, nil))
		require.NoError(t, err)
		var body struct {
			Count int `json:"count"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body.Count
	}

	code, n := get(base.Add(time.Second).Format(time.RFC3339Nano))
	require.Equal(t, fiber.StatusOK, code)
	require.Equal(t, 2, n, "since is inclusive")

	code, _ = get("yesterday")
	require.Equal(t, fiber.StatusBadRequest, code)

	require.Len(t, getRunSteps(t, app, runID), 3)
}
