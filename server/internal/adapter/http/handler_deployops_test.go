package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Fakes below are deliberately named with a "DeployOps" infix (rather than
// reusing e.g. handler_storeops_test.go's fakeDeployTargetStore) because that
// existing fake only tracks a single "last saved" target and always returns
// nil from ListAll/ListByRepository — fine for the SaveDeployTarget re-point
// test it serves, useless for building a real matrix here. Same package, so
// the names must not collide.

// fakeDeployOpsRepositoryStore is an in-memory port.RepositoryStore. Only
// Get and List are exercised by deployops.Service; the rest of the
// interface is stubbed to satisfy the type.
type fakeDeployOpsRepositoryStore struct {
	rows map[uuid.UUID]domain.Repository
}

var _ port.RepositoryStore = (*fakeDeployOpsRepositoryStore)(nil)

func newFakeDeployOpsRepositoryStore(rows []domain.Repository) *fakeDeployOpsRepositoryStore {
	m := make(map[uuid.UUID]domain.Repository, len(rows))
	for _, r := range rows {
		m[r.ID] = r
	}
	return &fakeDeployOpsRepositoryStore{rows: m}
}

func (f *fakeDeployOpsRepositoryStore) Create(_ context.Context, name, description, rootPath, remoteURL, kind string) (domain.Repository, error) {
	r := domain.Repository{ID: uuid.New(), Name: name, Description: description, RootPath: rootPath, RemoteURL: remoteURL}
	f.rows[r.ID] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateRemoteURL(_ context.Context, id uuid.UUID, remoteURL string) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository remote url: %w", port.ErrNotFound)
	}
	r.RemoteURL = remoteURL
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateRootPath(_ context.Context, id uuid.UUID, rootPath string) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository root path: %w", port.ErrNotFound)
	}
	r.RootPath = rootPath
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("get repository: %w", port.ErrNotFound)
	}
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) GetByRootPath(_ context.Context, rootPath string) (domain.Repository, error) {
	for _, r := range f.rows {
		if r.RootPath == rootPath {
			return r, nil
		}
	}
	return domain.Repository{}, fmt.Errorf("get repository by root path: %w", port.ErrNotFound)
}

func (f *fakeDeployOpsRepositoryStore) List(_ context.Context) ([]domain.Repository, error) {
	out := make([]domain.Repository, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeDeployOpsRepositoryStore) Update(_ context.Context, id uuid.UUID, name, description string, _, _, _ *string, _ *bool) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository: %w", port.ErrNotFound)
	}
	r.Name, r.Description = name, description
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateMeta(_ context.Context, id uuid.UUID, _ *string, _ *[]string) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository meta: %w", port.ErrNotFound)
	}
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateMobilePlatform(_ context.Context, id uuid.UUID, platform string) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository mobile platform: %w", port.ErrNotFound)
	}
	r.MobilePlatform = platform
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateReleaseEngine(_ context.Context, id uuid.UUID, engine string) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository release engine: %w", port.ErrNotFound)
	}
	r.ReleaseEngine = engine
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateDetectedAppIdentity(_ context.Context, id uuid.UUID, identity domain.AppIdentity) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository app identity: %w", port.ErrNotFound)
	}
	r.DetectedAppIdentity = identity
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateDetectedBuildTargets(_ context.Context, id uuid.UUID, targets domain.BuildTargets) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository build targets: %w", port.ErrNotFound)
	}
	r.DetectedBuildTargets = targets
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateQualityGates(_ context.Context, id uuid.UUID, coverage, mutation domain.QualityGate) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update quality gates: %w", port.ErrNotFound)
	}
	r.RequireOverallCoverage = coverage.Enabled
	r.CoverageThreshold = coverage.Threshold
	r.MutationEnabled = mutation.Enabled
	r.MutationThreshold = mutation.Threshold
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) SetDocsTaskID(_ context.Context, id uuid.UUID, taskID string) error {
	r, ok := f.rows[id]
	if !ok {
		return fmt.Errorf("set repository docs task: %w", port.ErrNotFound)
	}
	r.DocsTaskID = taskID
	f.rows[id] = r
	return nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateSubProjects(_ context.Context, id uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository sub-projects: %w", port.ErrNotFound)
	}
	r.SubProjects = subProjects
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateIncidentPolicy(_ context.Context, id uuid.UUID, _ domain.IncidentPolicy) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update incident policy: %w", port.ErrNotFound)
	}
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateTestStrategy(_ context.Context, id uuid.UUID, _ string) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update test strategy: %w", port.ErrNotFound)
	}
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) UpdateDocs(_ context.Context, id uuid.UUID, docs domain.RepositoryDocs) (domain.Repository, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository docs: %w", port.ErrNotFound)
	}
	r.Docs = docs
	f.rows[id] = r
	return r, nil
}

func (f *fakeDeployOpsRepositoryStore) SetWebhook(_ context.Context, id uuid.UUID, _ string, _ int64) error {
	if _, ok := f.rows[id]; !ok {
		return fmt.Errorf("set repository webhook: %w", port.ErrNotFound)
	}
	return nil
}

func (f *fakeDeployOpsRepositoryStore) WebhookSecret(_ context.Context, id uuid.UUID) (string, error) {
	if _, ok := f.rows[id]; !ok {
		return "", fmt.Errorf("get webhook secret: %w", port.ErrNotFound)
	}
	return "", nil
}

func (f *fakeDeployOpsRepositoryStore) Delete(_ context.Context, id uuid.UUID) error {
	delete(f.rows, id)
	return nil
}

func (f *fakeDeployOpsRepositoryStore) SetProjects(context.Context, uuid.UUID, []uuid.UUID) error {
	return nil
}

func (f *fakeDeployOpsRepositoryStore) ListProjectIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (f *fakeDeployOpsRepositoryStore) ListProjectIDsByRepositories(context.Context, []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return nil, nil
}

// fakeDeployOpsTargetStore is an in-memory port.DeployTargetStore, keyed on
// (repository_id, env).
type fakeDeployOpsTargetStore struct {
	rows []domain.DeployTarget
}

var _ port.DeployTargetStore = (*fakeDeployOpsTargetStore)(nil)

func newFakeDeployOpsTargetStore(rows []domain.DeployTarget) *fakeDeployOpsTargetStore {
	return &fakeDeployOpsTargetStore{rows: append([]domain.DeployTarget(nil), rows...)}
}

func (f *fakeDeployOpsTargetStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	var out []domain.DeployTarget
	for _, t := range f.rows {
		if t.RepositoryID == repositoryID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeDeployOpsTargetStore) ListAll(context.Context) ([]domain.DeployTarget, error) {
	return append([]domain.DeployTarget(nil), f.rows...), nil
}

func (f *fakeDeployOpsTargetStore) Get(_ context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error) {
	for _, t := range f.rows {
		if t.RepositoryID == repositoryID && t.Env == env {
			return t, nil
		}
	}
	return domain.DeployTarget{}, fmt.Errorf("get deploy target: %w", port.ErrNotFound)
}

func (f *fakeDeployOpsTargetStore) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	for i, existing := range f.rows {
		if existing.RepositoryID == t.RepositoryID && existing.Env == t.Env {
			f.rows[i] = t
			return t, nil
		}
	}
	f.rows = append(f.rows, t)
	return t, nil
}

func (f *fakeDeployOpsTargetStore) Delete(_ context.Context, repositoryID uuid.UUID, subProjectPath, env string) error {
	for i, existing := range f.rows {
		if existing.RepositoryID == repositoryID && existing.Env == env {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

// fakeDeployOpsPipelineJobStore is an in-memory port.RepositoryPipelineJobStore.
type fakeDeployOpsPipelineJobStore struct {
	rows []domain.RepositoryPipelineJob
}

var _ port.RepositoryPipelineJobStore = (*fakeDeployOpsPipelineJobStore)(nil)

func newFakeDeployOpsPipelineJobStore(rows []domain.RepositoryPipelineJob) *fakeDeployOpsPipelineJobStore {
	return &fakeDeployOpsPipelineJobStore{rows: append([]domain.RepositoryPipelineJob(nil), rows...)}
}

func (f *fakeDeployOpsPipelineJobStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	var out []domain.RepositoryPipelineJob
	for _, j := range f.rows {
		if j.RepositoryID == repositoryID {
			out = append(out, j)
		}
	}
	return out, nil
}

func (f *fakeDeployOpsPipelineJobStore) ListAll(context.Context) ([]domain.RepositoryPipelineJob, error) {
	return append([]domain.RepositoryPipelineJob(nil), f.rows...), nil
}

func (f *fakeDeployOpsPipelineJobStore) ReplaceForRepository(_ context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	var kept []domain.RepositoryPipelineJob
	for _, j := range f.rows {
		if j.RepositoryID != repositoryID {
			kept = append(kept, j)
		}
	}
	f.rows = append(kept, jobs...)
	return jobs, nil
}

// fakeDeployOpsRunStore is an in-memory port.DeploymentRunStore. Empty for
// every handler test in this file — none of them seed a prior run — so
// Latest/LastSuccessfulBefore always report port.ErrNotFound and Rollback
// always lands on ErrNoRollbackTarget.
type fakeDeployOpsRunStore struct {
	rows []domain.DeploymentRun
}

var _ port.DeploymentRunStore = (*fakeDeployOpsRunStore)(nil)

func (f *fakeDeployOpsRunStore) Upsert(_ context.Context, run domain.DeploymentRun) (domain.DeploymentRun, error) {
	f.rows = append(f.rows, run)
	return run, nil
}

func (f *fakeDeployOpsRunStore) Latest(_ context.Context, repositoryID uuid.UUID, env string) (domain.DeploymentRun, error) {
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID && r.Env == env {
			return r, nil
		}
	}
	return domain.DeploymentRun{}, fmt.Errorf("latest run: %w", port.ErrNotFound)
}

func (f *fakeDeployOpsRunStore) ByRunID(_ context.Context, repositoryID uuid.UUID, runID int64) (domain.DeploymentRun, error) {
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID && r.RunID == runID {
			return r, nil
		}
	}
	return domain.DeploymentRun{}, port.ErrNotFound
}

func (f *fakeDeployOpsRunStore) LatestAll(context.Context) ([]domain.DeploymentRun, error) {
	return append([]domain.DeploymentRun(nil), f.rows...), nil
}

func (f *fakeDeployOpsRunStore) ListByEnv(_ context.Context, repositoryID uuid.UUID, env string, limit int) ([]domain.DeploymentRun, error) {
	var out []domain.DeploymentRun
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID && r.Env == env {
			out = append(out, r)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeDeployOpsRunStore) LastSuccessfulBefore(_ context.Context, repositoryID uuid.UUID, env, excludeSHA string) (domain.DeploymentRun, error) {
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID && r.Env == env && r.Conclusion == domain.RunConclusionSuccess && r.HeadSHA != "" && r.HeadSHA != excludeSHA {
			return r, nil
		}
	}
	return domain.DeploymentRun{}, fmt.Errorf("last successful run: %w", port.ErrNotFound)
}

func (f *fakeDeployOpsRunStore) Stamp(context.Context, uuid.UUID, int64, string, string, string) error {
	return nil
}

// fakeDeployOpsDispatchStore is an in-memory port.DeployDispatchStore.
type fakeDeployOpsDispatchStore struct {
	rows []domain.DeployDispatch
}

var _ port.DeployDispatchStore = (*fakeDeployOpsDispatchStore)(nil)

func (f *fakeDeployOpsDispatchStore) Create(_ context.Context, d domain.DeployDispatch) (domain.DeployDispatch, error) {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	f.rows = append(f.rows, d)
	return d, nil
}

func (f *fakeDeployOpsDispatchStore) ListPending(context.Context) ([]domain.DeployDispatch, error) {
	var out []domain.DeployDispatch
	for _, d := range f.rows {
		if d.State == domain.DispatchStatePending {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeDeployOpsDispatchStore) Resolve(_ context.Context, id uuid.UUID, state string, runID *int64) error {
	for i, d := range f.rows {
		if d.ID == id {
			f.rows[i].State = state
			f.rows[i].MatchedRunID = runID
			return nil
		}
	}
	return fmt.Errorf("resolve dispatch: %w", port.ErrNotFound)
}

// fakeDeployOpsAuditStore is an in-memory port.OpsAuditStore.
type fakeDeployOpsAuditStore struct {
	rows []domain.OpsAuditEntry
}

var _ port.OpsAuditStore = (*fakeDeployOpsAuditStore)(nil)

func (f *fakeDeployOpsAuditStore) Log(_ context.Context, entry domain.OpsAuditEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	f.rows = append([]domain.OpsAuditEntry{entry}, f.rows...)
	return nil
}

func (f *fakeDeployOpsAuditStore) List(_ context.Context, repositoryID *uuid.UUID, limit int) ([]domain.OpsAuditEntry, error) {
	var out []domain.OpsAuditEntry
	for _, e := range f.rows {
		if repositoryID != nil {
			if e.RepositoryID == nil || *e.RepositoryID != *repositoryID {
				continue
			}
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// fakeActions is a settable, call-counting port.ActionsClient. Tests mutate
// dispatchErr/createTagErr directly after construction to script a GitHub
// failure (see TestDeployOpsProviderFailureIs502 below).
type fakeActions struct {
	dispatches   int
	dispatchErr  error
	tags         int
	createTagErr error
}

var _ port.ActionsClient = (*fakeActions)(nil)

func (f *fakeActions) ListWorkflowRuns(context.Context, string, string, string, string) ([]port.ActionsRun, error) {
	return nil, nil
}

func (f *fakeActions) DispatchWorkflow(_ context.Context, _, _, _, _ string) error {
	f.dispatches++
	return f.dispatchErr
}

func (f *fakeActions) CreateTag(_ context.Context, _, _, _, _ string) error {
	f.tags++
	return f.createTagErr
}

// newDeployOpsTestApp builds a fiber app with a deployops service backed by
// the in-memory fakes, and returns the repository id those fakes are seeded
// with plus the fake ActionsClient so tests can assert nothing was
// dispatched. The seeded repository is named "tasktrooper" (the confirm
// phrase every production-class test below types), has a stage target with
// no workflow mapping (the ErrNoWorkflowMapping test) and a prod target with
// one (so a prod dispatch/rollback can actually reach the ActionsClient).
func newDeployOpsTestApp(t *testing.T) (*fiber.App, uuid.UUID, *fakeActions) {
	t.Helper()
	repoID := uuid.New()

	repos := newFakeDeployOpsRepositoryStore([]domain.Repository{
		{ID: repoID, Name: "tasktrooper", Kind: "backend"},
	})
	targets := newFakeDeployOpsTargetStore([]domain.DeployTarget{
		{RepositoryID: repoID, Env: domain.DeployEnvStage, Provider: "gke"},
		{RepositoryID: repoID, Env: domain.DeployEnvProd, Provider: "gke"},
	})
	jobs := newFakeDeployOpsPipelineJobStore([]domain.RepositoryPipelineJob{
		{
			RepositoryID: repoID,
			Category:     domain.DeployEnvCategory(domain.DeployEnvProd),
			TargetKind:   domain.PipelineTargetWorkflow,
			TargetRef:    "prod.yml",
		},
	})
	runs := &fakeDeployOpsRunStore{}
	dispatches := &fakeDeployOpsDispatchStore{}
	audit := &fakeDeployOpsAuditStore{}
	actions := &fakeActions{}

	svc := deployops.New(runs, dispatches, audit, targets, repos, jobs, actions)
	svc.SetRepoResolver(func(_ context.Context, repo domain.Repository) (string, string, error) {
		return "owner", repo.Name, nil
	})

	h := &Handler{deployOpsSvc: svc}
	app := fiber.New()
	h.registerDeployOpsRoutes(app)

	return app, repoID, actions
}

// The confirm guardrail must be enforced by the server. 400, and nothing
// reaches GitHub.
func TestDeployOpsDispatchProdWithoutConfirmIs400(t *testing.T) {
	app, repoID, actions := newDeployOpsTestApp(t)
	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/deploy/prod/dispatch", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	require.Zero(t, actions.dispatches)
}

// A missing workflow mapping is a configuration conflict the UI can act on,
// not a server error — the matrix cell links the user off to go fix it.
func TestDeployOpsDispatchWithoutWorkflowMappingIs409(t *testing.T) {
	app, repoID, _ := newDeployOpsTestApp(t) // seeded with a stage target, no jobs
	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/deploy/stage/dispatch", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusConflict, resp.StatusCode)
}

// Nothing to roll back to is also 409, so the UI can disable the button and
// explain why rather than showing a generic failure.
func TestDeployOpsRollbackWithoutTargetIs409(t *testing.T) {
	app, repoID, _ := newDeployOpsTestApp(t)
	body := strings.NewReader(`{"confirm":"tasktrooper"}`)
	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/deploy/prod/rollback", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusConflict, resp.StatusCode)
}

// A GitHub outage is not our bug and not the caller's — 502 so the UI can say
// "the provider failed" rather than "something broke".
func TestDeployOpsProviderFailureIs502(t *testing.T) {
	app, repoID, actions := newDeployOpsTestApp(t)
	actions.dispatchErr = errors.New("github: 503 Service Unavailable")
	body := strings.NewReader(`{"confirm":"tasktrooper"}`)
	req := httptest.NewRequest("POST", "/v1/repositories/"+repoID.String()+"/deploy/prod/dispatch", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadGateway, resp.StatusCode)
}

func TestDeployOpsMatrixReturnsEnvsAndRepos(t *testing.T) {
	app, _, _ := newDeployOpsTestApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/operations/deployments", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	var view struct {
		Envs  []string `json:"envs"`
		Repos []struct {
			Cells []struct {
				Env string `json:"env"`
			} `json:"cells"`
		} `json:"repos"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&view))
	require.Equal(t, []string{"local", "stage", "preprod", "prod"}, view.Envs)
	require.Len(t, view.Repos, 1)
	require.Len(t, view.Repos[0].Cells, 4)
}

func TestDeployOpsRunsRejectsNonUUIDRepository(t *testing.T) {
	app, _, _ := newDeployOpsTestApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/not-a-uuid/deploy/prod/runs", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

// An unknown environment must never reach the service — stage/preprod/prod is
// the whole set.
func TestDeployOpsRejectsUnknownEnv(t *testing.T) {
	app, repoID, _ := newDeployOpsTestApp(t)
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/deploy/qa/runs", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

// The deploy-watch reads (migration 105). This handler test exercises the
// dispatch/rollback endpoints only, so they are stubs — but the interface is
// one interface, and a fake that does not satisfy it does not compile.

func (f *fakeActions) ListRunsForCommit(context.Context, string, string, string) ([]port.ActionsRun, error) {
	return nil, nil
}

func (f *fakeActions) ListRunJobs(context.Context, string, string, int64) ([]port.ActionsJob, error) {
	return nil, nil
}

func (f *fakeActions) JobLogs(context.Context, string, string, int64) (string, error) {
	return "", nil
}

func (f *fakeActions) CommitDeployStatus(context.Context, string, string, string) (port.CommitDeploySignal, error) {
	return port.CommitDeploySignal{}, nil
}
