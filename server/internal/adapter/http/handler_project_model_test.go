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

	"github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakePMStore is a minimal in-memory port.ProjectModelStore: only
// GetComponent/SaveComponent (and the List reads project() makes on the way
// to a no-op legacy projection) do anything; every other method is a stub
// that satisfies the interface without a real backing store, since this
// file's tests exercise UpdateProjectComponent's PATCH handler only.
type fakePMStore struct {
	components map[uuid.UUID]domain.Component
}

var _ port.ProjectModelStore = (*fakePMStore)(nil)

func newFakePMStore(components ...domain.Component) *fakePMStore {
	m := make(map[uuid.UUID]domain.Component, len(components))
	for _, c := range components {
		m[c.ID] = c
	}
	return &fakePMStore{components: m}
}

func (f *fakePMStore) ListComponents(_ context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
	var out []domain.Component
	for _, c := range f.components {
		if c.RepositoryID == repositoryID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakePMStore) ListComponentsForRepositories(context.Context, []uuid.UUID) ([]domain.Component, error) {
	return nil, nil
}

func (f *fakePMStore) ListAllComponents(context.Context) ([]domain.Component, error) { return nil, nil }

func (f *fakePMStore) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	c, ok := f.components[id]
	if !ok {
		return domain.Component{}, port.ErrNotFound
	}
	return c, nil
}

func (f *fakePMStore) SaveComponent(_ context.Context, c domain.Component) (domain.Component, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.components[c.ID] = c
	return c, nil
}

func (f *fakePMStore) ListChecks(context.Context, uuid.UUID) ([]domain.ComponentCheck, error) {
	return nil, nil
}
func (f *fakePMStore) GetCheck(context.Context, uuid.UUID) (domain.ComponentCheck, error) {
	return domain.ComponentCheck{}, port.ErrNotFound
}
func (f *fakePMStore) SaveCheck(_ context.Context, c domain.ComponentCheck) (domain.ComponentCheck, error) {
	return c, nil
}

func (f *fakePMStore) ListLinks(context.Context, uuid.UUID) ([]domain.ComponentLink, error) {
	return nil, nil
}
func (f *fakePMStore) ListIncomingLinks(context.Context, uuid.UUID) ([]domain.ComponentLink, error) {
	return nil, nil
}
func (f *fakePMStore) ListLinksForRepositories(context.Context, []uuid.UUID) ([]domain.ComponentLink, error) {
	return nil, nil
}
func (f *fakePMStore) GetLink(context.Context, uuid.UUID) (domain.ComponentLink, error) {
	return domain.ComponentLink{}, port.ErrNotFound
}
func (f *fakePMStore) SaveLink(_ context.Context, l domain.ComponentLink) (domain.ComponentLink, error) {
	return l, nil
}
func (f *fakePMStore) DeleteLink(context.Context, uuid.UUID) error { return nil }

func (f *fakePMStore) GetResource(context.Context, uuid.UUID) (domain.SystemResource, error) {
	return domain.SystemResource{}, port.ErrNotFound
}
func (f *fakePMStore) ListResources(context.Context, []uuid.UUID) ([]domain.SystemResource, error) {
	return nil, nil
}
func (f *fakePMStore) EnsureResource(_ context.Context, r domain.SystemResource) (domain.SystemResource, error) {
	return r, nil
}
func (f *fakePMStore) MergeResources(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakePMStore) RenameResource(_ context.Context, id uuid.UUID, name string) (domain.SystemResource, error) {
	return domain.SystemResource{ID: id, Name: name}, nil
}

func (f *fakePMStore) CreateScan(_ context.Context, s domain.ProjectScan) (domain.ProjectScan, error) {
	return s, nil
}
func (f *fakePMStore) UpdateScan(context.Context, domain.ProjectScan) error { return nil }
func (f *fakePMStore) GetScan(context.Context, uuid.UUID) (domain.ProjectScan, error) {
	return domain.ProjectScan{}, port.ErrNotFound
}
func (f *fakePMStore) LatestScan(context.Context, uuid.UUID) (domain.ProjectScan, error) {
	return domain.ProjectScan{}, port.ErrNotFound
}
func (f *fakePMStore) FailInterruptedScans(context.Context) (int, error) { return 0, nil }

func (f *fakePMStore) ApplyReconcile(context.Context, port.ModelReconcile) error { return nil }

// fakePMRepos satisfies both projectmodel.RepositoryReader and
// projectmodel.LegacyProjector with one seeded repository; every Update*
// method is a no-op that returns the seeded row unchanged, since the legacy
// projection these tests run through (project()) is exercised for real
// elsewhere (projectmodel package tests) and is incidental here.
type fakePMRepos struct {
	repo domain.Repository
}

func (f *fakePMRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) { return f.repo, nil }
func (f *fakePMRepos) List(context.Context) ([]domain.Repository, error) {
	return []domain.Repository{f.repo}, nil
}

func (f *fakePMRepos) UpdateMeta(context.Context, uuid.UUID, *string, *[]string) (domain.Repository, error) {
	return f.repo, nil
}
func (f *fakePMRepos) UpdateSubProjects(context.Context, uuid.UUID, []domain.RepoSubProject) (domain.Repository, error) {
	return f.repo, nil
}
func (f *fakePMRepos) UpdateMobilePlatform(context.Context, uuid.UUID, string) (domain.Repository, error) {
	return f.repo, nil
}
func (f *fakePMRepos) UpdateDetectedAppIdentity(context.Context, uuid.UUID, domain.AppIdentity) (domain.Repository, error) {
	return f.repo, nil
}
func (f *fakePMRepos) UpdateDetectedBuildTargets(context.Context, uuid.UUID, domain.BuildTargets) (domain.Repository, error) {
	return f.repo, nil
}
func (f *fakePMRepos) UpdateQualityGates(context.Context, uuid.UUID, domain.QualityGate, domain.QualityGate) (domain.Repository, error) {
	return f.repo, nil
}

// fakePMPipelines is an in-memory projectmodel.PipelineJobWriter.
type fakePMPipelines struct{}

func (f *fakePMPipelines) ListByRepository(context.Context, uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	return nil, nil
}
func (f *fakePMPipelines) ReplaceForRepository(_ context.Context, _ uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	return jobs, nil
}

// newProjectModelTestApp builds a fiber app with the project-model routes
// backed by an in-memory store seeded with one component.
func newProjectModelTestApp(t *testing.T, comp domain.Component) (*fiber.App, *fakePMStore) {
	t.Helper()
	store := newFakePMStore(comp)
	repos := &fakePMRepos{repo: domain.Repository{ID: comp.RepositoryID, Name: "tasktrooper"}}
	svc := projectmodel.NewService(projectmodel.Deps{
		Store:     store,
		Repos:     repos,
		Projector: repos,
		Pipelines: &fakePMPipelines{},
	})
	h := &Handler{projectModelSvc: svc}
	app := fiber.New()
	h.registerProjectModelRoutes(app)
	return app, store
}

// Delivery is the only field this test covers; everything else on this
// endpoint (name/role/commands/gates/...) is covered by projectmodel's own
// tests.
func TestUpdateProjectComponentDeliveryRoundTrips(t *testing.T) {
	repoID := uuid.New()
	comp := domain.Component{ID: uuid.New(), RepositoryID: repoID, Path: ".", Status: domain.ComponentStatusActive}
	app, store := newProjectModelTestApp(t, comp)

	body := `{"delivery":{"mode":"on_merge","executor":"github_actions","workflow":"deploy.yml","verify":{"soak_minutes":15},"auto_rollback":true}}`
	req := httptest.NewRequest("PATCH", "/v1/components/"+comp.ID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var got domain.Component
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.NotNil(t, got.Delivery.Override, "the response Component must include the saved delivery override")
	assert.Equal(t, domain.DeliveryOnMerge, got.Delivery.Override.Mode)
	assert.Equal(t, domain.ExecutorGitHubActions, got.Delivery.Override.Executor)
	assert.Equal(t, "deploy.yml", got.Delivery.Override.Workflow)
	assert.Equal(t, 15, got.Delivery.Override.Verify.SoakMinutes)
	assert.True(t, got.Delivery.Override.AutoRollback)

	stored, err := store.GetComponent(context.Background(), comp.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.Delivery.Override)
	assert.Equal(t, domain.DeliveryOnMerge, stored.Delivery.Override.Mode)

	// null clears the override back to the detected profile.
	clearReq := httptest.NewRequest("PATCH", "/v1/components/"+comp.ID.String(), strings.NewReader(`{"delivery":null}`))
	clearReq.Header.Set("Content-Type", "application/json")
	clearResp, err := app.Test(clearReq)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, clearResp.StatusCode)
	var cleared domain.Component
	require.NoError(t, json.NewDecoder(clearResp.Body).Decode(&cleared))
	assert.Nil(t, cleared.Delivery.Override)
}

func TestUpdateProjectComponentDeliveryInvalidProfileIs400(t *testing.T) {
	repoID := uuid.New()
	comp := domain.Component{ID: uuid.New(), RepositoryID: repoID, Path: ".", Status: domain.ComponentStatusActive}
	app, _ := newProjectModelTestApp(t, comp)

	body := `{"delivery":{"mode":"dispatch","executor":"github_actions"}}` // dispatch mode needs a workflow
	req := httptest.NewRequest("PATCH", "/v1/components/"+comp.ID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}
