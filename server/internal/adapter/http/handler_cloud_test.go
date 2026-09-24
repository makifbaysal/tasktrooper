package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	cloudapp "github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeCloudAccounts is the smallest in-memory port.CloudAccountStore this
// handler's tests need — the real behavior (verify-before-store, encryption)
// belongs to application/cloud's and the postgres store's own tests.
type fakeCloudAccounts struct {
	accounts map[uuid.UUID]domain.CloudAccount
	fields   map[uuid.UUID]map[string]string
}

func newFakeCloudAccounts() *fakeCloudAccounts {
	return &fakeCloudAccounts{accounts: map[uuid.UUID]domain.CloudAccount{}, fields: map[uuid.UUID]map[string]string{}}
}

func (f *fakeCloudAccounts) ListCloudAccounts(context.Context) ([]domain.CloudAccount, error) {
	out := make([]domain.CloudAccount, 0, len(f.accounts))
	for _, a := range f.accounts {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeCloudAccounts) GetCloudAccount(_ context.Context, id uuid.UUID) (domain.CloudAccount, error) {
	a, ok := f.accounts[id]
	if !ok {
		return domain.CloudAccount{}, port.ErrNotFound
	}
	return a, nil
}

func (f *fakeCloudAccounts) CreateCloudAccount(_ context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error) {
	if acct.ID == uuid.Nil {
		acct.ID = uuid.New()
	}
	f.accounts[acct.ID] = acct
	f.fields[acct.ID] = fields
	return acct, nil
}

func (f *fakeCloudAccounts) UpdateCloudAccount(_ context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error) {
	if _, ok := f.accounts[acct.ID]; !ok {
		return domain.CloudAccount{}, port.ErrNotFound
	}
	f.accounts[acct.ID] = acct
	if fields != nil {
		f.fields[acct.ID] = fields
	}
	return acct, nil
}

func (f *fakeCloudAccounts) DeleteCloudAccount(_ context.Context, id uuid.UUID) error {
	if _, ok := f.accounts[id]; !ok {
		return port.ErrNotFound
	}
	delete(f.accounts, id)
	return nil
}

func (f *fakeCloudAccounts) CloudCredential(_ context.Context, id uuid.UUID) (domain.CloudCredential, error) {
	a, ok := f.accounts[id]
	if !ok {
		return domain.CloudCredential{}, port.ErrNotFound
	}
	return domain.CloudCredential{AccountID: id, Provider: a.Provider, Meta: a.Meta, Fields: f.fields[id]}, nil
}

// fakeEnvironments is the smallest in-memory port.EnvironmentStore.
type fakeEnvironments struct {
	envs map[uuid.UUID]domain.ComponentEnvironment
}

func newFakeEnvironments() *fakeEnvironments {
	return &fakeEnvironments{envs: map[uuid.UUID]domain.ComponentEnvironment{}}
}

func (f *fakeEnvironments) ListEnvironments(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error) {
	var out []domain.ComponentEnvironment
	for _, e := range f.envs {
		if e.RepositoryID == repositoryID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeEnvironments) ListAllEnvironments(context.Context) ([]domain.ComponentEnvironment, error) {
	out := make([]domain.ComponentEnvironment, 0, len(f.envs))
	for _, e := range f.envs {
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeEnvironments) GetEnvironment(_ context.Context, id uuid.UUID) (domain.ComponentEnvironment, error) {
	e, ok := f.envs[id]
	if !ok {
		return domain.ComponentEnvironment{}, port.ErrNotFound
	}
	return e, nil
}

func (f *fakeEnvironments) SaveEnvironment(_ context.Context, e domain.ComponentEnvironment) (domain.ComponentEnvironment, error) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.envs[e.ID] = e
	return e, nil
}

func (f *fakeEnvironments) DeleteEnvironment(_ context.Context, id uuid.UUID) error {
	if _, ok := f.envs[id]; !ok {
		return port.ErrNotFound
	}
	delete(f.envs, id)
	return nil
}

// fakeComponents is the narrow port.ComponentStore the bind route needs.
type fakeComponents struct {
	components map[uuid.UUID]domain.Component
}

func (f *fakeComponents) ListComponents(context.Context, uuid.UUID) ([]domain.Component, error) {
	return nil, nil
}
func (f *fakeComponents) ListComponentsForRepositories(context.Context, []uuid.UUID) ([]domain.Component, error) {
	return nil, nil
}
func (f *fakeComponents) ListAllComponents(context.Context) ([]domain.Component, error) {
	return nil, nil
}
func (f *fakeComponents) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	c, ok := f.components[id]
	if !ok {
		return domain.Component{}, port.ErrNotFound
	}
	return c, nil
}
func (f *fakeComponents) SaveComponent(_ context.Context, c domain.Component) (domain.Component, error) {
	f.components[c.ID] = c
	return c, nil
}

// fakeCloudProvider drives account verification and every runtime read with
// canned, per-test responses.
type fakeCloudProvider struct {
	kind        domain.CloudProviderKind
	verifyErr   error
	logsPage    domain.RuntimeLogPage
	logsErr     error
	errorsErr   error
	deployErr   error
	resourceErr error
}

func (p *fakeCloudProvider) Kind() domain.CloudProviderKind { return p.kind }

func (p *fakeCloudProvider) Verify(context.Context, domain.CloudCredential) (map[string]string, error) {
	if p.verifyErr != nil {
		return nil, p.verifyErr
	}
	return map[string]string{"username": "akif"}, nil
}

func (p *fakeCloudProvider) ListResources(context.Context, domain.CloudCredential) ([]domain.CloudResource, error) {
	return nil, nil
}

func (p *fakeCloudProvider) Resource(context.Context, domain.CloudCredential, domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	if p.resourceErr != nil {
		return domain.CloudResourceDetail{}, p.resourceErr
	}
	return domain.CloudResourceDetail{Status: domain.CloudStatusHealthy}, nil
}

func (p *fakeCloudProvider) Deployments(context.Context, domain.CloudCredential, domain.CloudResourceRef, int) ([]domain.CloudDeployment, error) {
	if p.deployErr != nil {
		return nil, p.deployErr
	}
	return nil, nil
}

func (p *fakeCloudProvider) Logs(context.Context, domain.CloudCredential, domain.CloudResourceRef, domain.RuntimeLogQuery) (domain.RuntimeLogPage, error) {
	if p.logsErr != nil {
		return domain.RuntimeLogPage{}, p.logsErr
	}
	return p.logsPage, nil
}

func (p *fakeCloudProvider) Errors(context.Context, domain.CloudCredential, domain.CloudResourceRef, time.Time) ([]domain.RuntimeErrorGroup, error) {
	if p.errorsErr != nil {
		return nil, p.errorsErr
	}
	return nil, nil
}

type cloudTestFixture struct {
	app      *fiber.App
	accounts *fakeCloudAccounts
	envs     *fakeEnvironments
	comps    *fakeComponents
	provider *fakeCloudProvider
}

func newCloudTestApp(t *testing.T) cloudTestFixture {
	t.Helper()
	accounts := newFakeCloudAccounts()
	envs := newFakeEnvironments()
	comps := &fakeComponents{components: map[uuid.UUID]domain.Component{}}
	provider := &fakeCloudProvider{kind: domain.CloudVercel}

	svc := cloudapp.NewService(cloudapp.Deps{
		Accounts:     accounts,
		Environments: envs,
		Providers:    []port.CloudProvider{provider},
		Components:   comps,
		// CreateAccount/UpdateAccount/VerifyAccount rematch in the background
		// on a successful verify; an empty-but-non-nil Repos keeps that
		// goroutine from dereferencing a nil interface mid-test.
		Repos: fakeCloudRepos{},
	})

	h := &Handler{cloudSvc: svc}
	app := fiber.New()
	h.registerCloudRoutes(app)
	return cloudTestFixture{app: app, accounts: accounts, envs: envs, comps: comps, provider: provider}
}

type fakeCloudRepos struct{}

func (fakeCloudRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return domain.Repository{}, port.ErrNotFound
}

func (fakeCloudRepos) List(context.Context) ([]domain.Repository, error) { return nil, nil }

func TestListCloudAccountsIsEmptyNotNull(t *testing.T) {
	fx := newCloudTestApp(t)
	resp, err := fx.app.Test(httptest.NewRequest("GET", "/v1/cloud-accounts", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var body struct {
		Accounts []domain.CloudAccount `json:"accounts"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotNil(t, body.Accounts)
	require.Empty(t, body.Accounts)
}

func TestCreateCloudAccountRejectsAnUnknownProvider(t *testing.T) {
	fx := newCloudTestApp(t)
	rec := postJSON(t, fx.app, "/v1/cloud-accounts", `{"provider":"heroku","fields":{}}`)
	require.Equal(t, fiber.StatusBadRequest, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Contains(t, body, "error")
	require.NotContains(t, body, "code")
}

// A provider refusing the credential must surface as 400 with code
// cloud_auth, not a generic 400 — the UI's "reconnect" affordance keys off it.
func TestCreateCloudAccountRefusedCredentialReturnsCloudAuthCode(t *testing.T) {
	fx := newCloudTestApp(t)
	fx.provider.verifyErr = port.ErrCloudAuth
	rec := postJSON(t, fx.app, "/v1/cloud-accounts", `{"provider":"vercel","fields":{"token":"bad"}}`)
	require.Equal(t, fiber.StatusBadRequest, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "cloud_auth", body["code"])
}

func TestCreateCloudAccountNeverEchoesFields(t *testing.T) {
	fx := newCloudTestApp(t)
	rec := postJSON(t, fx.app, "/v1/cloud-accounts", `{"provider":"vercel","fields":{"token":"super-secret"}}`)
	require.Equal(t, fiber.StatusCreated, rec.Code)
	require.NotContains(t, rec.Body.String(), "super-secret")
}

func TestDeleteUnknownCloudAccountReturns404(t *testing.T) {
	fx := newCloudTestApp(t)
	req := httptest.NewRequest("DELETE", "/v1/cloud-accounts/"+uuid.New().String(), nil)
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

func TestBindComponentEnvironmentRequiresAnActiveComponent(t *testing.T) {
	fx := newCloudTestApp(t)
	rec := postPut(t, fx.app, "/v1/components/"+uuid.New().String()+"/environments/production", `{"url":"https://app.example.com"}`)
	require.Equal(t, fiber.StatusNotFound, rec.Code)
}

func TestBindComponentEnvironmentCustomURL(t *testing.T) {
	fx := newCloudTestApp(t)
	compID := uuid.New()
	repoID := uuid.New()
	fx.comps.components[compID] = domain.Component{ID: compID, RepositoryID: repoID, Status: domain.ComponentStatusActive}

	rec := postPut(t, fx.app, "/v1/components/"+compID.String()+"/environments/production", `{"url":"https://app.example.com"}`)
	require.Equal(t, fiber.StatusOK, rec.Code)

	var env domain.ComponentEnvironment
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "https://app.example.com", env.URL)
	require.Equal(t, domain.EnvironmentProduction, env.Environment)
}

func TestBindComponentEnvironmentRejectsAnUnknownEnvironment(t *testing.T) {
	fx := newCloudTestApp(t)
	compID := uuid.New()
	fx.comps.components[compID] = domain.Component{ID: compID, RepositoryID: uuid.New(), Status: domain.ComponentStatusActive}

	rec := postPut(t, fx.app, "/v1/components/"+compID.String()+"/environments/moonbase", `{"url":"https://app.example.com"}`)
	require.Equal(t, fiber.StatusBadRequest, rec.Code)
}

func TestEnvironmentLogsRejectsAMalformedSince(t *testing.T) {
	fx := newCloudTestApp(t)
	envID := uuid.New()
	fx.envs.envs[envID] = domain.ComponentEnvironment{ID: envID}

	req := httptest.NewRequest("GET", "/v1/environments/"+envID.String()+"/logs?since=not-a-time", nil)
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

// An environment with no bound account/resource is 409 not_connected, not a
// 500 — the UI tells the human to bind it rather than reporting a crash.
func TestEnvironmentLogsOnAnUnboundEnvironmentReturnsNotConnected(t *testing.T) {
	fx := newCloudTestApp(t)
	envID := uuid.New()
	fx.envs.envs[envID] = domain.ComponentEnvironment{ID: envID}

	req := httptest.NewRequest("GET", "/v1/environments/"+envID.String()+"/logs", nil)
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusConflict, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "not_connected", body["code"])
}

func TestEnvironmentLogsOnABoundEnvironmentReturnsEntries(t *testing.T) {
	fx := newCloudTestApp(t)
	acctID := uuid.New()
	fx.accounts.accounts[acctID] = domain.CloudAccount{ID: acctID, Provider: domain.CloudVercel, Status: domain.CloudAccountOK}
	fx.accounts.fields[acctID] = map[string]string{"token": "tok"}

	envID := uuid.New()
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}
	fx.envs.envs[envID] = domain.ComponentEnvironment{
		ID: envID, Provider: domain.CloudVercel, AccountID: &acctID, Resource: &ref, Status: domain.LinkConfirmed,
	}
	fx.provider.logsPage = domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{
		{Timestamp: time.Now(), Severity: domain.LogError, Message: "boom"},
	}}

	req := httptest.NewRequest("GET", "/v1/environments/"+envID.String()+"/logs?min_severity=error&limit=50", nil)
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var page domain.RuntimeLogPage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
	require.Len(t, page.Entries, 1)
	require.Equal(t, "boom", page.Entries[0].Message)
}

func TestEnvironmentLogsRejectsAnUnknownSeverity(t *testing.T) {
	fx := newCloudTestApp(t)
	envID := uuid.New()
	fx.envs.envs[envID] = domain.ComponentEnvironment{ID: envID}

	req := httptest.NewRequest("GET", "/v1/environments/"+envID.String()+"/logs?min_severity=catastrophic", nil)
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

// A provider that goes unreachable (not an auth refusal, not "unbound") is a
// 502: the server is fine, the third party is not.
func TestEnvironmentLogsProviderOutageReturns502(t *testing.T) {
	fx := newCloudTestApp(t)
	acctID := uuid.New()
	fx.accounts.accounts[acctID] = domain.CloudAccount{ID: acctID, Provider: domain.CloudVercel, Status: domain.CloudAccountOK}
	fx.accounts.fields[acctID] = map[string]string{"token": "tok"}

	envID := uuid.New()
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}
	fx.envs.envs[envID] = domain.ComponentEnvironment{
		ID: envID, Provider: domain.CloudVercel, AccountID: &acctID, Resource: &ref, Status: domain.LinkConfirmed,
	}
	fx.provider.logsErr = errTestProviderOutage

	req := httptest.NewRequest("GET", "/v1/environments/"+envID.String()+"/logs", nil)
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadGateway, resp.StatusCode)
}

func TestEnvironmentOverviewOnAnUnboundEnvironmentReports200WithUnavailable(t *testing.T) {
	fx := newCloudTestApp(t)
	envID := uuid.New()
	fx.envs.envs[envID] = domain.ComponentEnvironment{ID: envID}

	req := httptest.NewRequest("GET", "/v1/environments/"+envID.String()+"/overview", nil)
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var overview domain.EnvironmentRuntime
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&overview))
	require.NotEmpty(t, overview.Unavailable)
}

func TestDeleteEnvironmentUnknownIDReturns404(t *testing.T) {
	fx := newCloudTestApp(t)
	req := httptest.NewRequest("DELETE", "/v1/environments/"+uuid.New().String(), nil)
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

func TestPatchEnvironmentConfirmsACandidate(t *testing.T) {
	fx := newCloudTestApp(t)
	acctID := uuid.New()
	fx.accounts.accounts[acctID] = domain.CloudAccount{ID: acctID, Provider: domain.CloudVercel, Status: domain.CloudAccountOK}

	envID := uuid.New()
	fx.envs.envs[envID] = domain.ComponentEnvironment{ID: envID, Status: domain.LinkSuggested}

	body := `{"status":"confirmed","account_id":"` + acctID.String() + `","resource":{"kind":"vercel_project","id":"prj_1","name":"web"}}`
	req := httptest.NewRequest("PATCH", "/v1/environments/"+envID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := fx.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var env domain.ComponentEnvironment
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	require.Equal(t, domain.LinkConfirmed, env.Status)
	require.NotNil(t, env.Resource)
	require.Equal(t, "prj_1", env.Resource.ID)
}

func postPut(t *testing.T, app *fiber.App, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	_, err = rec.Body.ReadFrom(resp.Body)
	require.NoError(t, err)
	return rec
}

var errTestProviderOutage = &testProviderOutageError{}

type testProviderOutageError struct{}

func (e *testProviderOutageError) Error() string { return "vercel: upstream unavailable" }
