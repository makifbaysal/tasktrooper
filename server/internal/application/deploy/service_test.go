package deploy_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeTargetStore is an in-memory port.DeployTargetStore for SaveTarget hook
// tests: only Save is exercised (and observed) here.
type fakeTargetStore struct {
	saved   *domain.DeployTarget
	saveErr error
}

func (f *fakeTargetStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeTargetStore) ListAll(ctx context.Context) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeTargetStore) Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error) {
	return domain.DeployTarget{}, errors.New("not found")
}
func (f *fakeTargetStore) Save(ctx context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	if f.saveErr != nil {
		return domain.DeployTarget{}, f.saveErr
	}
	saved := t
	f.saved = &saved
	return t, nil
}
func (f *fakeTargetStore) Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) error {
	return nil
}

// fakeRepoResolver is a fixed-response deploy.RepositoryResolver, used only
// for the app-name-falls-back-to-repo-name path.
type fakeRepoResolver struct {
	repo domain.Repository
}

func (f *fakeRepoResolver) Get(ctx context.Context, id uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

// onboardCall is one recorded invocation of the SetStoreOnboarder hook.
type onboardCall struct {
	repositoryID uuid.UUID
	provider     string
	identifier   string
	appName      string
}

// onboardRecorder stands in for storeops.Service.Onboard: it records every
// call the SaveTarget hook makes and can be made to fail on demand.
type onboardRecorder struct {
	calls []onboardCall
	err   error
}

func (r *onboardRecorder) fn(ctx context.Context, repositoryID uuid.UUID, provider, identifier, appName string) error {
	r.calls = append(r.calls, onboardCall{repositoryID, provider, identifier, appName})
	return r.err
}

// fakeTaskCreator records the last CreateTask call, for asserting what a
// system-opened setup task was assigned to.
type fakeTaskCreator struct {
	lastReq domain.CreateBoardTaskRequest
}

func (f *fakeTaskCreator) CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.lastReq = req
	return domain.BoardTask{}, nil
}

// fakeRoleResolver is a fixed-response port.RoleResolver: it answers
// AgentForPurpose(system_task_assignee) with one agent id and everything else
// with "nobody".
type fakeRoleResolver struct {
	agentID uuid.UUID
}

func (f fakeRoleResolver) AgentForRole(context.Context, uuid.UUID, string) (*uuid.UUID, error) {
	return nil, nil
}

func (f fakeRoleResolver) AgentForPurpose(_ context.Context, purpose domain.RolePurposeKey, _ string) (*uuid.UUID, error) {
	if purpose != domain.PurposeSystemTaskAssignee {
		return nil, nil
	}
	id := f.agentID
	return &id, nil
}

func (f fakeRoleResolver) AgentArea(context.Context, uuid.UUID) string { return "" }

func (f fakeRoleResolver) AgentAreas(context.Context, uuid.UUID) []string { return nil }

func (f fakeRoleResolver) AssigneeForNewTask(_ context.Context, _ domain.TaskType, _ string, requested *uuid.UUID) (*uuid.UUID, error) {
	return requested, nil
}

// The setup task's assignee comes from the developer role's
// system_task_assignee purpose now, not a hardcoded agent name lookup.
func TestCreateLocalSetupTaskAssignsThroughRoleResolver(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend}
	resolver := fakeRoleResolver{agentID: uuid.New()}
	svc := deploy.NewService(&fakeTargetStore{}, &fakeRepoResolver{repo: repo})
	svc.SetRoleResolver(resolver)
	tasks := &fakeTaskCreator{}
	svc.SetTaskCreator(tasks)

	if _, err := svc.CreateLocalSetupTask(context.Background(), repo.ID, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tasks.lastReq.AssigneeAgentID == nil || *tasks.lastReq.AssigneeAgentID != resolver.agentID {
		t.Fatalf("assignee = %v, want %s", tasks.lastReq.AssigneeAgentID, resolver.agentID)
	}
}

// Without a role resolver wired, the task is simply left unassigned — never
// a hardcoded fallback name.
func TestCreateLocalSetupTaskLeavesUnassignedWithoutRoleResolver(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend}
	svc := deploy.NewService(&fakeTargetStore{}, &fakeRepoResolver{repo: repo})
	tasks := &fakeTaskCreator{}
	svc.SetTaskCreator(tasks)

	if _, err := svc.CreateLocalSetupTask(context.Background(), repo.ID, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tasks.lastReq.AssigneeAgentID != nil {
		t.Fatalf("expected no assignee, got %v", tasks.lastReq.AssigneeAgentID)
	}
}

func TestSaveTargetOnboardsStoreProviderWithPackageName(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{})
	rec := &onboardRecorder{}
	svc.SetStoreOnboarder(rec.fn)

	repoID := uuid.New()
	if _, err := svc.SaveTarget(context.Background(), repoID, domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderGooglePlay,
		Vars:     map[string]string{"package_name": "com.x.y"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if targets.saved == nil {
		t.Fatalf("target was not saved")
	}
	if len(rec.calls) != 1 {
		t.Fatalf("want 1 onboard call, got %d", len(rec.calls))
	}
	call := rec.calls[0]
	if call.provider != domain.DeployProviderGooglePlay || call.identifier != "com.x.y" {
		t.Fatalf("unexpected onboard call: %+v", call)
	}
	if call.repositoryID != repoID {
		t.Fatalf("wrong repository id passed to onboarder")
	}
}

func TestSaveTargetStoreProviderWithoutIdentifierErrorsAndDoesNotSave(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{})
	rec := &onboardRecorder{}
	svc.SetStoreOnboarder(rec.fn)

	_, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderAppStore,
		Vars:     map[string]string{},
	})
	if err == nil {
		t.Fatalf("expected error for missing identifier")
	}
	if err.Error() != "store targets need bundle_id/package_name" {
		t.Fatalf("unexpected error message: %v", err)
	}
	if targets.saved != nil {
		t.Fatalf("target must not be saved when identifier is missing")
	}
	if len(rec.calls) != 0 {
		t.Fatalf("onboarder must not be called when save did not happen")
	}
}

func TestSaveTargetNonStoreProviderNeverCallsOnboarder(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{})
	rec := &onboardRecorder{}
	svc.SetStoreOnboarder(rec.fn)

	if _, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderFly,
		Vars:     map[string]string{},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("onboarder must not be called for non-store provider")
	}
}

func TestSaveTargetPrefersBundleIDOverPackageName(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{})
	rec := &onboardRecorder{}
	svc.SetStoreOnboarder(rec.fn)

	if _, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderAppStore,
		Vars:     map[string]string{"bundle_id": "com.a.b", "package_name": "com.c.d"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.calls[0].identifier != "com.a.b" {
		t.Fatalf("want bundle_id preferred over package_name, got %q", rec.calls[0].identifier)
	}
}

func TestSaveTargetAppNameFallsBackToRepositoryName(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{repo: domain.Repository{Name: "Fallback Name"}})
	rec := &onboardRecorder{}
	svc.SetStoreOnboarder(rec.fn)

	if _, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderAppStore,
		Vars:     map[string]string{"bundle_id": "com.a.b"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.calls[0].appName != "Fallback Name" {
		t.Fatalf("want app name to fall back to repository name, got %q", rec.calls[0].appName)
	}
}

func TestSaveTargetNilOnboarderDoesNotPanic(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{}) // SetStoreOnboarder never called

	if _, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderGooglePlay,
		Vars:     map[string]string{"package_name": "com.x.y"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets.saved == nil {
		t.Fatalf("target must still be saved when no onboarder is wired")
	}
}

// A transient onboarding failure (the store API being down) must still leave
// the target saved — the target is the durable record and onboarding retries
// off it. This tolerance is exactly why the identifier refusal cannot ride on
// this hook, and it must survive the guard being added in front.
func TestSaveTargetOnboarderErrorDoesNotFailSave(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{})
	rec := &onboardRecorder{err: errors.New("store api down")}
	svc.SetStoreOnboarder(rec.fn)
	svc.SetStoreIdentifierGuard((&guardRecorder{}).fn)

	target, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderGooglePlay,
		Vars:     map[string]string{"package_name": "com.x.y"},
	})
	if err != nil {
		t.Fatalf("an onboarder failure must not fail SaveTarget: %v", err)
	}
	if target.Provider != domain.DeployProviderGooglePlay {
		t.Fatalf("expected the saved target back even when onboarding fails")
	}
	if len(rec.calls) != 1 {
		t.Fatalf("onboarder should still have been called exactly once")
	}
	if targets.saved == nil {
		t.Fatal("a transient onboarding failure must leave the target persisted")
	}
}

// guardCall is one recorded invocation of the SetStoreIdentifierGuard hook.
type guardCall struct {
	repositoryID uuid.UUID
	provider     string
	identifier   string
}

// guardRecorder stands in for storeops.Service.EnsureIdentifierAllowed.
type guardRecorder struct {
	calls []guardCall
	err   error
}

func (g *guardRecorder) fn(_ context.Context, repositoryID uuid.UUID, provider, identifier string) error {
	g.calls = append(g.calls, guardCall{repositoryID, provider, identifier})
	return g.err
}

// TestSaveTargetRefusesToRePointALiveStoreTarget walks the REAL path an
// operator takes — SaveTarget, not storeops.Onboard directly. Onboarding runs
// after the save and its errors are deliberately swallowed into a log line, so
// a refusal raised only there would return the caller a 200 while the deploy
// target persisted the new bundle ID and the store row kept `live` under the
// old one. The guard runs before the write, so nothing is saved at all.
func TestSaveTargetRefusesToRePointALiveStoreTarget(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{})
	onboarder := &onboardRecorder{}
	svc.SetStoreOnboarder(onboarder.fn)
	guard := &guardRecorder{err: errors.New("already live as com.example.old: delete the deploy target first")}
	svc.SetStoreIdentifierGuard(guard.fn)

	repoID := uuid.New()
	_, err := svc.SaveTarget(context.Background(), repoID, domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvProd,
		Provider: domain.DeployProviderAppStore,
		Vars:     map[string]string{"bundle_id": "com.example.new"},
	})
	if err == nil {
		t.Fatal("expected SaveTarget to refuse the re-point, got nil")
	}
	if !strings.Contains(err.Error(), "com.example.old") {
		t.Errorf("error = %v, want a message an operator can act on", err)
	}
	if targets.saved != nil {
		t.Fatalf("the target must not be persisted when the guard refuses: %+v", targets.saved)
	}
	if len(onboarder.calls) != 0 {
		t.Errorf("onboarding must not run for a refused save, got %d calls", len(onboarder.calls))
	}
	if len(guard.calls) != 1 {
		t.Fatalf("guard called %d times, want 1", len(guard.calls))
	}
	if guard.calls[0].repositoryID != repoID ||
		guard.calls[0].provider != domain.DeployProviderAppStore ||
		guard.calls[0].identifier != "com.example.new" {
		t.Errorf("guard saw %+v, want the incoming repository/provider/identifier", guard.calls[0])
	}
}

// The guard is store-only and permissive by default: a passing guard saves as
// before, a non-store provider never consults it, and no guard wired (the
// pre-wiring default) must not panic.
func TestSaveTargetGuardIsStoreOnlyAndPermissive(t *testing.T) {
	targets := &fakeTargetStore{}
	svc := deploy.NewService(targets, &fakeRepoResolver{})
	guard := &guardRecorder{}
	svc.SetStoreIdentifierGuard(guard.fn)

	if _, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderGooglePlay,
		Vars:     map[string]string{"package_name": "com.example.ok"},
	}); err != nil {
		t.Fatalf("a passing guard must not block the save: %v", err)
	}
	if targets.saved == nil {
		t.Fatal("target was not saved despite a passing guard")
	}
	if len(guard.calls) != 1 {
		t.Fatalf("guard called %d times for a store target, want 1", len(guard.calls))
	}

	targets.saved = nil
	if _, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvProd,
		Provider: domain.DeployProviderGCPCloudRun,
	}); err != nil {
		t.Fatalf("non-store save failed: %v", err)
	}
	if len(guard.calls) != 1 {
		t.Errorf("guard must not run for a non-store provider, got %d calls", len(guard.calls))
	}

	bare := deploy.NewService(&fakeTargetStore{}, &fakeRepoResolver{}) // no guard wired
	if _, err := bare.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
		Env:      domain.DeployEnvStage,
		Provider: domain.DeployProviderAppStore,
		Vars:     map[string]string{"bundle_id": "com.example.ok"},
	}); err != nil {
		t.Fatalf("a nil guard must leave saves ungated: %v", err)
	}
}

// ─── F6 (write half): a health_url must not point inside the cluster ──────────

// TestSaveTargetRefusesInternalHealthURLs stops the scheduled scanner from being
// configured in the first place. prodops.Monitor GETs this URL every minute for
// as long as the target exists, so an accepted http://127.0.0.1:8080/metrics is
// a standing read of this pod's own unauthenticated endpoints.
func TestSaveTargetRefusesInternalHealthURLs(t *testing.T) {
	for _, healthURL := range []string{
		"http://127.0.0.1:8080/metrics",
		"http://[::1]:8080/metrics",
		"http://169.254.169.254/computeMetadata/v1/",
		"http://10.4.0.9/healthz",
		"http://192.168.1.1/healthz",
		"http://100.64.0.1/healthz",
		"http://[fd00::1]/healthz",
		"file:///etc/passwd",
	} {
		targets := &fakeTargetStore{}
		svc := deploy.NewService(targets, &fakeRepoResolver{})

		_, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
			Env:       domain.DeployEnvProd,
			Provider:  domain.DeployProviderGCPCloudRun,
			HealthURL: healthURL,
		})
		if err == nil {
			t.Fatalf("%s was accepted as a health_url", healthURL)
		}
		if !strings.Contains(err.Error(), "health_url") {
			t.Fatalf("%s: unhelpful error %v", healthURL, err)
		}
		if targets.saved != nil {
			t.Fatalf("%s was stored despite the error", healthURL)
		}
	}
}

// A public health URL — the actual product use — still saves, including one
// whose host does not resolve yet because the environment is not up. Names are
// judged at dial time, not here; see deploy.SaveTarget's comment.
func TestSaveTargetAcceptsPublicHealthURLs(t *testing.T) {
	for _, healthURL := range []string{
		"https://api.example.com/healthz",
		"https://not-deployed-yet.example.com/healthz",
		"http://93.184.216.34/healthz",
		"",
	} {
		targets := &fakeTargetStore{}
		svc := deploy.NewService(targets, &fakeRepoResolver{})

		if _, err := svc.SaveTarget(context.Background(), uuid.New(), domain.SaveDeployTargetRequest{
			Env:       domain.DeployEnvProd,
			Provider:  domain.DeployProviderGCPCloudRun,
			HealthURL: healthURL,
		}); err != nil {
			t.Fatalf("%q must still be savable: %v", healthURL, err)
		}
		if targets.saved == nil {
			t.Fatalf("%q was not saved", healthURL)
		}
	}
}

// The deploy settings screen prefills a store target's bundle_id /
// package_name from what the working copy said at import — the config view is
// where it reads them, because the host serving this request may hold no copy
// of the repository at all.
func TestConfigServesTheDetectedAppIdentity(t *testing.T) {
	identity := domain.AppIdentity{BundleID: "com.acme.app", PackageName: "com.acme.app"}
	svc := deploy.NewService(&fakeTargetStore{}, &fakeRepoResolver{repo: domain.Repository{
		Kind:                domain.RepoKindMobile,
		DetectedAppIdentity: identity,
	}})

	view, err := svc.Config(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if view.DetectedAppIdentity != identity {
		t.Fatalf("detected app identity = %+v, want %+v", view.DetectedAppIdentity, identity)
	}
}

// Scoped like Kind: a monorepo's mobile sub-project has its own identifiers,
// and the repository — which is not itself an app — has none to lend it.
func TestConfigScopesTheDetectedAppIdentityToTheSubProject(t *testing.T) {
	sub := domain.AppIdentity{BundleID: "com.acme.sub", PackageName: "com.acme.sub"}
	repo := domain.Repository{
		Kind: domain.RepoKindMonorepo,
		SubProjects: []domain.RepoSubProject{
			{Path: "apps/api", Kind: domain.RepoKindBackend},
			{Path: "apps/mobile", Kind: domain.RepoKindMobile, DetectedAppIdentity: sub},
		},
	}
	svc := deploy.NewService(&fakeTargetStore{}, &fakeRepoResolver{repo: repo})

	for _, tc := range []struct {
		scope string
		want  domain.AppIdentity
	}{
		{scope: "apps/mobile", want: sub},
		{scope: "apps/api", want: domain.AppIdentity{}},
		{scope: "apps/gone", want: domain.AppIdentity{}},
		{scope: "", want: domain.AppIdentity{}},
	} {
		view, err := svc.Config(context.Background(), uuid.New(), tc.scope)
		if err != nil {
			t.Fatalf("Config(%q): %v", tc.scope, err)
		}
		if view.DetectedAppIdentity != tc.want {
			t.Fatalf("Config(%q) identity = %+v, want %+v", tc.scope, view.DetectedAppIdentity, tc.want)
		}
	}
}

// A form that prefills has to tell "unknown" from "not sent", so both keys are
// serialised even when neither could be read.
func TestConfigAlwaysSerialisesTheAppIdentity(t *testing.T) {
	svc := deploy.NewService(&fakeTargetStore{}, &fakeRepoResolver{repo: domain.Repository{Kind: domain.RepoKindBackend}})

	view, err := svc.Config(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"detected_app_identity":{"bundle_id":"","package_name":""}`) {
		t.Fatalf("config payload does not carry an empty detected_app_identity: %s", body)
	}
}
