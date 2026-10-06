package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/envreq"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeEnvRequirements struct {
	gotComponent uuid.UUID
	gotInputs    []envreq.EnvInput
	applyErr     error
}

func (f *fakeEnvRequirements) Status(context.Context, uuid.UUID) ([]domain.ComponentEnvStatus, error) {
	return []domain.ComponentEnvStatus{{ComponentName: "web", Vars: []domain.EnvVarView{{Name: "GITHUB_TOKEN", Action: domain.EnvActionHuman}}}}, nil
}

func (f *fakeEnvRequirements) Apply(_ context.Context, _, componentID uuid.UUID, inputs []envreq.EnvInput) (domain.ComponentEnvStatus, error) {
	f.gotComponent, f.gotInputs = componentID, inputs
	if f.applyErr != nil {
		return domain.ComponentEnvStatus{}, f.applyErr
	}
	return domain.ComponentEnvStatus{ComponentID: componentID}, nil
}

func (f *fakeEnvRequirements) Redeploy(context.Context, uuid.UUID, uuid.UUID) (domain.CloudDeployment, error) {
	return domain.CloudDeployment{ID: "dpl_new"}, nil
}

func envRequirementsApp(svc *fakeEnvRequirements) *fiber.App {
	h := &Handler{envRequirements: svc}
	app := fiber.New()
	h.registerEnvRequirementRoutes(app)
	return app
}

func TestListEnvRequirementsAnswersTargets(t *testing.T) {
	app := envRequirementsApp(&fakeEnvRequirements{})

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+uuid.NewString()+"/env-requirements", nil))

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	raw, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(raw), `"targets"`)
	assert.Contains(t, string(raw), `"GITHUB_TOKEN"`)
}

func TestApplyEnvRequirementsPassesTheValuesThroughAndNeverEchoesThem(t *testing.T) {
	svc := &fakeEnvRequirements{}
	app := envRequirementsApp(svc)
	comp := uuid.New()
	body := fmt.Sprintf(`{"component_id":%q,"vars":[{"name":"GITHUB_TOKEN","value":"ghp_abc"},{"name":"CONTENT_BACKEND","kind":"optional"}]}`, comp)
	req := httptest.NewRequest("POST", "/v1/repositories/"+uuid.NewString()+"/env-requirements/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, comp, svc.gotComponent)
	require.Len(t, svc.gotInputs, 2)
	assert.Equal(t, "ghp_abc", svc.gotInputs[0].Value)
	assert.Equal(t, domain.EnvKindOptional, svc.gotInputs[1].Kind)
	raw, _ := io.ReadAll(resp.Body)
	assert.NotContains(t, string(raw), "ghp_abc")
}

func TestApplyEnvRequirementsNeedsAComponent(t *testing.T) {
	app := envRequirementsApp(&fakeEnvRequirements{})
	req := httptest.NewRequest("POST", "/v1/repositories/"+uuid.NewString()+"/env-requirements/apply", strings.NewReader(`{"vars":[]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)

	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestApplyEnvRequirementsMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("%w: short password", domain.ErrEnvRequirementInvalid), fiber.StatusBadRequest, ""},
		{fmt.Errorf("%w: token is read-only", port.ErrCloudWriteDenied), fiber.StatusForbidden, "cloud_write_denied"},
		{fmt.Errorf("%w: revoked", port.ErrCloudAuth), fiber.StatusBadRequest, "cloud_auth"},
	} {
		app := envRequirementsApp(&fakeEnvRequirements{applyErr: tc.err})
		req := httptest.NewRequest("POST", "/v1/repositories/"+uuid.NewString()+"/env-requirements/apply",
			strings.NewReader(fmt.Sprintf(`{"component_id":%q}`, uuid.New())))
		req.Header.Set("Content-Type", "application/json")

		resp, err := app.Test(req)

		require.NoError(t, err)
		assert.Equal(t, tc.status, resp.StatusCode, tc.err.Error())
		var body map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&body)
		assert.Equal(t, tc.code, body["code"])
	}
}
