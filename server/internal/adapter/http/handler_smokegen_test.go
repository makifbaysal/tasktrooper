package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/smokegen"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeSmokeGen struct {
	job          smokegen.Job
	generateErr  error
	lastExisting []domain.SmokeCheck
	cancelled    []uuid.UUID
}

func (f *fakeSmokeGen) Generate(_ context.Context, componentID uuid.UUID, existing []domain.SmokeCheck) (smokegen.Job, error) {
	f.lastExisting = existing
	if f.generateErr != nil {
		return smokegen.Job{}, f.generateErr
	}
	f.job.ComponentID = componentID
	return f.job, nil
}

func (f *fakeSmokeGen) Get(jobID uuid.UUID) (smokegen.Job, error) {
	if jobID != f.job.JobID {
		return smokegen.Job{}, smokegen.ErrJobNotFound
	}
	return f.job, nil
}

func (f *fakeSmokeGen) Cancel(jobID uuid.UUID) { f.cancelled = append(f.cancelled, jobID) }

func newSmokeGenApp(svc *fakeSmokeGen) *fiber.App {
	h := &Handler{smokeGenSvc: svc}
	app := fiber.New()
	h.registerSmokeGenRoutes(app)
	return app
}

func TestGenerateSmokeChecksAccepted(t *testing.T) {
	svc := &fakeSmokeGen{job: smokegen.Job{JobID: uuid.New(), Status: smokegen.StatusRunning, Checks: []domain.SmokeCheck{}, Results: []domain.SmokeResult{}}}
	app := newSmokeGenApp(svc)
	componentID := uuid.New()

	req := httptest.NewRequest("POST", "/v1/components/"+componentID.String()+"/smoke-checks/generate",
		strings.NewReader(`{"existing":[{"method":"GET","path":"/health"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusAccepted, resp.StatusCode)

	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "running", got["status"])
	assert.Equal(t, componentID.String(), got["component_id"])
	assert.Equal(t, []any{}, got["checks"], "arrays are never null on the wire")
	assert.Contains(t, got, "error")
	require.Len(t, svc.lastExisting, 1)
	assert.Equal(t, "/health", svc.lastExisting[0].Path)
}

func TestGenerateSmokeChecksErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"unknown component": {port.ErrNotFound, fiber.StatusNotFound},
		"no agent":          {smokegen.ErrNoAgent, fiber.StatusConflict},
	} {
		app := newSmokeGenApp(&fakeSmokeGen{generateErr: tc.err})
		req := httptest.NewRequest("POST", "/v1/components/"+uuid.New().String()+"/smoke-checks/generate", nil)
		resp, err := app.Test(req)
		require.NoError(t, err, name)
		require.Equal(t, tc.status, resp.StatusCode, name)

		var body errorResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body), name)
		assert.NotEmpty(t, body.Error.Message, name)
	}

	app := newSmokeGenApp(&fakeSmokeGen{})
	resp, err := app.Test(httptest.NewRequest("POST", "/v1/components/not-a-uuid/smoke-checks/generate", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestGetSmokeCheckGeneration(t *testing.T) {
	svc := &fakeSmokeGen{job: smokegen.Job{JobID: uuid.New(), Status: smokegen.StatusDone, Checks: []domain.SmokeCheck{{Method: "GET", Path: "/"}}, Results: []domain.SmokeResult{}}}
	app := newSmokeGenApp(svc)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/smoke-check-generations/"+svc.job.JobID.String(), nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	resp, err = app.Test(httptest.NewRequest("GET", "/v1/smoke-check-generations/"+uuid.New().String(), nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

func TestCancelSmokeCheckGenerationIsIdempotent(t *testing.T) {
	svc := &fakeSmokeGen{}
	app := newSmokeGenApp(svc)
	id := uuid.New()

	for range 2 {
		resp, err := app.Test(httptest.NewRequest("DELETE", "/v1/smoke-check-generations/"+id.String(), nil))
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)
	}
	assert.Equal(t, []uuid.UUID{id, id}, svc.cancelled)
}

func TestRegisterSmokeGenRoutesNoopWhenServiceNil(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerSmokeGenRoutes(app)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/smoke-check-generations/"+uuid.New().String(), nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}
