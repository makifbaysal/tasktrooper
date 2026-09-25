package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeReleaseService is an in-memory ReleaseService. Get returns
// domain.ErrReleaseNotFound for any id not seeded into releases, matching the
// real service's documented contract (port/release.go).
type fakeReleaseService struct {
	releases map[uuid.UUID]domain.Release

	listErr     error
	getErr      error
	deployErr   error
	finishErr   error
	rollbackErr error

	lastDeployActor    domain.ReleaseActor
	lastFinishActor    domain.ReleaseActor
	lastFinishNote     string
	lastRollbackActor  domain.ReleaseActor
	lastRollbackReason domain.RollbackReason
	lastRollbackNote   string
}

var _ ReleaseService = (*fakeReleaseService)(nil)

func newFakeReleaseService(releases ...domain.Release) *fakeReleaseService {
	m := make(map[uuid.UUID]domain.Release, len(releases))
	for _, r := range releases {
		m[r.ID] = r
	}
	return &fakeReleaseService{releases: m}
}

func (f *fakeReleaseService) List(_ context.Context, filter domain.ReleaseListFilter) ([]domain.Release, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []domain.Release
	for _, r := range f.releases {
		if filter.RepositoryID != nil && r.RepositoryID != *filter.RepositoryID {
			continue
		}
		if filter.ComponentID != nil && (r.ComponentID == nil || *r.ComponentID != *filter.ComponentID) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeReleaseService) Get(_ context.Context, id uuid.UUID) (domain.Release, error) {
	if f.getErr != nil {
		return domain.Release{}, f.getErr
	}
	r, ok := f.releases[id]
	if !ok {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	return r, nil
}

func (f *fakeReleaseService) Deploy(_ context.Context, id uuid.UUID, actor domain.ReleaseActor) (domain.Release, error) {
	f.lastDeployActor = actor
	if f.deployErr != nil {
		return domain.Release{}, f.deployErr
	}
	r := f.releases[id]
	r.Status = domain.ReleaseDeploying
	f.releases[id] = r
	return r, nil
}

func (f *fakeReleaseService) Finish(_ context.Context, id uuid.UUID, actor domain.ReleaseActor, note string) (domain.Release, error) {
	f.lastFinishActor, f.lastFinishNote = actor, note
	if f.finishErr != nil {
		return domain.Release{}, f.finishErr
	}
	r := f.releases[id]
	r.Status = domain.ReleaseReleased
	r.Verdict = note
	f.releases[id] = r
	return r, nil
}

func (f *fakeReleaseService) Rollback(_ context.Context, id uuid.UUID, actor domain.ReleaseActor, reason domain.RollbackReason, note string) (domain.Release, error) {
	f.lastRollbackActor, f.lastRollbackReason, f.lastRollbackNote = actor, reason, note
	if f.rollbackErr != nil {
		return domain.Release{}, f.rollbackErr
	}
	r := f.releases[id]
	r.Status = domain.ReleaseRollingBack
	f.releases[id] = r
	return r, nil
}

// newReleaseTestApp builds a fiber app with the release routes backed by a
// fakeReleaseService and a real repository.Service (constructed with every
// optional dependency nil — Get/withGitWarning tolerate that, see
// repository/service.go) over an in-memory repository store, so the confirm
// check exercises the same code path production does. The seeded repository
// is named "tasktrooper", the confirm phrase every test below types.
func newReleaseTestApp(t *testing.T, releases ...domain.Release) (*fiber.App, uuid.UUID, *fakeReleaseService) {
	t.Helper()
	repoID := uuid.New()
	repos := newFakeDeployOpsRepositoryStore([]domain.Repository{{ID: repoID, Name: "tasktrooper"}})
	repoSvc := repository.NewService(repos, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	svc := newFakeReleaseService(releases...)
	for i, r := range releases {
		if r.RepositoryID == uuid.Nil {
			r.RepositoryID = repoID
			svc.releases[r.ID] = r
			releases[i] = r
		}
	}

	h := &Handler{releaseSvc: svc, repositorySvc: repoSvc}
	app := fiber.New()
	h.registerReleaseRoutes(app)
	return app, repoID, svc
}

func TestRegisterReleaseRoutesNoopWhenServiceNil(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerReleaseRoutes(app)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/releases/"+uuid.New().String(), nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode, "no route mounted means fiber's own 404, not a handler response")
}

func TestListReleasesRejectsNonUUIDRepository(t *testing.T) {
	app, _, _ := newReleaseTestApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/not-a-uuid/releases", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestListReleasesRejectsInvalidComponentID(t *testing.T) {
	app, repoID, _ := newReleaseTestApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/releases?component_id=not-a-uuid", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestListReleasesReturnsReleasesForTheRepository(t *testing.T) {
	componentID := uuid.New()
	r1 := domain.Release{ID: uuid.New(), Version: "abc1234", ComponentID: &componentID}
	r2 := domain.Release{ID: uuid.New(), Version: "def5678"}
	app, repoID, _ := newReleaseTestApp(t, r1, r2)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/releases", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var body struct {
		Releases []domain.Release `json:"releases"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Len(t, body.Releases, 2)

	resp2, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/releases?component_id="+componentID.String(), nil))
	require.NoError(t, err)
	var filtered struct {
		Releases []domain.Release `json:"releases"`
	}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&filtered))
	require.Len(t, filtered.Releases, 1)
	assert.Equal(t, r1.ID, filtered.Releases[0].ID)
}

func TestListReleasesReturnsEmptyArrayNotNull(t *testing.T) {
	app, repoID, _ := newReleaseTestApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/releases", nil))
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"releases":[]}`, string(raw))
}

func TestGetReleaseNotFoundIs404(t *testing.T) {
	app, _, _ := newReleaseTestApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/releases/"+uuid.New().String(), nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

func TestGetReleaseRejectsNonUUID(t *testing.T) {
	app, _, _ := newReleaseTestApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/releases/not-a-uuid", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestGetReleaseReturnsIt(t *testing.T) {
	r := domain.Release{ID: uuid.New(), Version: "abc1234", Status: domain.ReleaseVerifying}
	app, _, _ := newReleaseTestApp(t, r)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/releases/"+r.ID.String(), nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	var got domain.Release
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, r.Version, got.Version)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)
}

// The three write actions (deploy/finish/rollback) share the confirm
// guardrail and error mapping, so they are table-driven over the endpoint
// path rather than three near-identical copies of each case.
var releaseWriteEndpoints = []string{"deploy", "finish", "rollback"}

func TestReleaseWriteActionsRequireConfirm(t *testing.T) {
	for _, action := range releaseWriteEndpoints {
		t.Run(action, func(t *testing.T) {
			r := domain.Release{ID: uuid.New(), Status: domain.ReleasePending, Mode: domain.DeliveryDispatch}
			app, _, _ := newReleaseTestApp(t, r)
			req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/"+action, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestReleaseWriteActionsRejectWrongConfirm(t *testing.T) {
	for _, action := range releaseWriteEndpoints {
		t.Run(action, func(t *testing.T) {
			r := domain.Release{ID: uuid.New(), Status: domain.ReleasePending, Mode: domain.DeliveryDispatch}
			app, _, _ := newReleaseTestApp(t, r)
			req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/"+action, strings.NewReader(`{"confirm":"not-the-name"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestReleaseWriteActionsOnAnUnknownReleaseAre404BeforeConfirmIsChecked(t *testing.T) {
	for _, action := range releaseWriteEndpoints {
		t.Run(action, func(t *testing.T) {
			app, _, _ := newReleaseTestApp(t)
			req := httptest.NewRequest("POST", "/v1/releases/"+uuid.New().String()+"/"+action, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusNotFound, resp.StatusCode, "a release that does not exist must 404 even with no confirm supplied")
		})
	}
}

func TestDeployReleaseSucceedsWithHumanActor(t *testing.T) {
	r := domain.Release{ID: uuid.New(), Status: domain.ReleasePending, Mode: domain.DeliveryDispatch}
	app, _, svc := newReleaseTestApp(t, r)

	req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/deploy", strings.NewReader(`{"confirm":"tasktrooper"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, domain.ReleaseActorHuman, svc.lastDeployActor)

	var got domain.Release
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, domain.ReleaseDeploying, got.Status)
}

func TestDeployReleaseWrongStatusIs409(t *testing.T) {
	r := domain.Release{ID: uuid.New(), Status: domain.ReleaseDeploying, Mode: domain.DeliveryDispatch}
	app, _, svc := newReleaseTestApp(t, r)
	svc.deployErr = domain.ErrReleaseWrongStatus

	req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/deploy", strings.NewReader(`{"confirm":"tasktrooper"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
}

func TestDeployReleaseNoDeployStepIs409(t *testing.T) {
	r := domain.Release{ID: uuid.New(), Status: domain.ReleasePending, Mode: domain.DeliveryOnMerge}
	app, _, svc := newReleaseTestApp(t, r)
	svc.deployErr = domain.ErrReleaseNoDeploy

	req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/deploy", strings.NewReader(`{"confirm":"tasktrooper"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
}

func TestFinishReleasePassesNoteAndHumanActor(t *testing.T) {
	r := domain.Release{ID: uuid.New(), Status: domain.ReleaseAwaitingVerdict}
	app, _, svc := newReleaseTestApp(t, r)

	req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/finish", strings.NewReader(`{"confirm":"tasktrooper","note":"logs clean, no new errors"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, domain.ReleaseActorHuman, svc.lastFinishActor)
	assert.Equal(t, "logs clean, no new errors", svc.lastFinishNote)

	var got domain.Release
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, domain.ReleaseReleased, got.Status)
}

func TestFinishReleaseDeliveryUnconfirmedIs409(t *testing.T) {
	r := domain.Release{ID: uuid.New(), Status: domain.ReleaseAwaitingVerdict}
	app, _, svc := newReleaseTestApp(t, r)
	svc.finishErr = domain.ErrDeliveryUnconfirmed

	req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/finish", strings.NewReader(`{"confirm":"tasktrooper"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
}

func TestRollbackReleaseAlwaysUsesManualReasonAndHumanActor(t *testing.T) {
	r := domain.Release{ID: uuid.New(), Status: domain.ReleaseFailed}
	app, _, svc := newReleaseTestApp(t, r)

	req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/rollback", strings.NewReader(`{"confirm":"tasktrooper","note":"ship it back"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, domain.ReleaseActorHuman, svc.lastRollbackActor)
	assert.Equal(t, domain.RollbackManual, svc.lastRollbackReason)
	assert.Equal(t, "ship it back", svc.lastRollbackNote)
}

func TestRollbackReleaseWrongStatusIs409(t *testing.T) {
	r := domain.Release{ID: uuid.New(), Status: domain.ReleaseDeploying}
	app, _, svc := newReleaseTestApp(t, r)
	svc.rollbackErr = domain.ErrReleaseWrongStatus

	req := httptest.NewRequest("POST", "/v1/releases/"+r.ID.String()+"/rollback", strings.NewReader(`{"confirm":"tasktrooper"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)
}
