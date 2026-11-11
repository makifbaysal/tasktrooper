package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace/runtracetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func traceApp(t *testing.T) (*fiber.App, *runtracetest.MemStore, domain.TaskAgentRun) {
	t.Helper()
	store := runtracetest.NewMemStore()
	run := domain.TaskAgentRun{ID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New()}
	other := domain.TaskAgentRun{ID: uuid.New(), TaskID: uuid.New()}
	require.NoError(t, store.Begin(nil, domain.RunTrace{RunID: run.ID, TaskID: run.TaskID, AgentID: run.AgentID, BaseSHA: "abc", StartedAt: time.Now()}))
	mk := func(seq int, cat, detail string) domain.RunTraceEvent {
		return domain.RunTraceEvent{RunID: run.ID, Seq: seq, Tool: "t", Category: cat, Status: "ok", Detail: json.RawMessage(detail)}
	}
	require.NoError(t, store.Append(nil, []domain.RunTraceEvent{
		mk(1, domain.TraceCategoryRead, `{"path":"a"}`),
		mk(2, domain.TraceCategoryExec, `{"command":"ls"}`),
		mk(3, domain.TraceCategoryExternal, `{"effect":"http_request"}`),
	}))
	h := &Handler{
		runTraces: store,
		taskRuns:  &stepsTaskRunStore{rows: map[uuid.UUID]domain.TaskAgentRun{run.ID: run, other.ID: other}},
	}
	app := fiber.New()
	h.registerRunTraceRoutes(app)
	return app, store, run
}

func traceURL(run domain.TaskAgentRun, suffix string) string {
	return "/v1/repositories/" + uuid.NewString() + "/tasks/" + run.TaskID.String() + "/runs/" + run.ID.String() + suffix
}

func TestRunTraceListsFiltersAndPages(t *testing.T) {
	app, _, run := traceApp(t)
	get := func(url string) map[string]any {
		resp, err := app.Test(httptest.NewRequest("GET", url, nil))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		var m map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
		return m
	}
	all := get(traceURL(run, "/trace"))
	require.Equal(t, true, all["recorded"])
	require.EqualValues(t, 3, all["count"])

	exec := get(traceURL(run, "/trace?category=exec"))
	require.EqualValues(t, 1, exec["count"])

	page := get(traceURL(run, "/trace?limit=2"))
	require.EqualValues(t, 2, page["next_after"])
	rest := get(traceURL(run, "/trace?after=2"))
	require.EqualValues(t, 1, rest["count"])
}

func TestRunTraceRejectsBadInput(t *testing.T) {
	app, _, run := traceApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", traceURL(run, "/trace?category=nope"), nil))
	require.NoError(t, err)
	require.Equal(t, 400, resp.StatusCode)

	foreign := run
	foreign.TaskID = uuid.New()
	resp, err = app.Test(httptest.NewRequest("GET", traceURL(foreign, "/trace"), nil))
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode, "a run named under another task is not found")
}

func TestRunTraceOfAnOlderRunSaysNotRecorded(t *testing.T) {
	app, _, _ := traceApp(t)
	old := domain.TaskAgentRun{ID: uuid.New(), TaskID: uuid.New()}
	h := &Handler{runTraces: runtracetest.NewMemStore(), taskRuns: &stepsTaskRunStore{rows: map[uuid.UUID]domain.TaskAgentRun{old.ID: old}}}
	app = fiber.New()
	h.registerRunTraceRoutes(app)
	for _, suffix := range []string{"/trace", "/trace/summary"} {
		resp, err := app.Test(httptest.NewRequest("GET", traceURL(old, suffix), nil))
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		var m map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
		require.Equal(t, false, m["recorded"])
	}
	resp, err := app.Test(httptest.NewRequest("GET", traceURL(old, "/trace.json"), nil))
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode)
}

func TestRunTraceSummaryAndExport(t *testing.T) {
	app, _, run := traceApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", traceURL(run, "/trace/summary"), nil))
	require.NoError(t, err)
	var body struct {
		Recorded bool                    `json:"recorded"`
		Summary  domain.RunTraceSummary `json:"summary"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.True(t, body.Recorded)
	require.Equal(t, "abc", body.Summary.Run.BaseSHA)
	require.Len(t, body.Summary.NonUndoable, 1)
	require.Equal(t, "http_request", body.Summary.NonUndoable[0].Kind)

	resp, err = app.Test(httptest.NewRequest("GET", traceURL(run, "/trace.json"), nil))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Disposition"), "attachment")
	var export struct {
		Events []domain.RunTraceEvent `json:"events"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&export))
	require.Len(t, export.Events, 3)
}
