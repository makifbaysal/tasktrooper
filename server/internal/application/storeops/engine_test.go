package storeops_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type engineFixture struct {
	repoID uuid.UUID
	taskID uuid.UUID
	repos  *fakeRepositoryResolver
	apps   *fakeMobileStoreAppStore
	audit  *fakeOpsAuditStore
	parker *fakeReleaseParker
	tasks  *fakeTaskCreator

	actionsErr   error
	localHost    storeops.LocalRunnerHost
	localErr     error
	actionsCalls int
	localCalls   int

	probedWorkflow string

	started []startedRelease

	startErr error
}

type startedRelease struct {
	Engine    string
	Artifacts []pipeline.Artifact
}

const testWorkflowFile = "mobile-release.yml"

func newEngineTestService(t *testing.T) (*storeops.Service, *engineFixture) {
	t.Helper()

	repoID, taskID := uuid.New(), uuid.New()
	repos := newFakeRepositoryResolver()

	repos.set(domain.Repository{
		ID: repoID, Name: "trooper", Kind: domain.RepoKindMobile,
		DetectedBuildTargets: domain.BuildTargets{XcodeScheme: "Trooper", GradleModule: "app"},
	})

	apps := newFakeMobileStoreAppStore()
	for platform, identifier := range map[string]string{
		domain.MobileStorePlatformIOS:     "com.example.ios",
		domain.MobileStorePlatformAndroid: "com.example.android",
	} {
		if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
			RepositoryID:     repoID,
			Platform:         platform,
			Identifier:       identifier,
			StoreAppID:       "asc-app-1",
			AppName:          "Trooper",
			State:            domain.MobileStoreStateLive,
			OnboardingTaskID: &taskID,
		}); err != nil {
			t.Fatal(err)
		}
	}

	f := &engineFixture{
		repoID:    repoID,
		taskID:    taskID,
		repos:     repos,
		apps:      apps,
		audit:     newFakeOpsAuditStore(),
		parker:    &fakeReleaseParker{},
		tasks:     newFakeTaskCreator(),
		localHost: storeops.LocalRunnerHost{Paired: true, MacOS: true},
	}

	svc := storeops.NewService(storeops.Deps{
		Credentials: newFakeCredentialStore(),
		Apps:        apps,
		Repos:       repos,
		Tasks:       f.tasks,
	})
	svc.SetAuditor(f.audit)
	svc.SetReleaseParker(f.parker)
	svc.SetEngineProbes(
		func(_ context.Context, _ domain.Repository, _, workflowFile string) error {
			f.actionsCalls++
			f.probedWorkflow = workflowFile
			return f.actionsErr
		},
		func(context.Context) (storeops.LocalRunnerHost, error) {
			f.localCalls++
			return f.localHost, f.localErr
		},
	)
	svc.SetReleaseStarter(func(_ context.Context, _ domain.Repository, _ domain.MobileStoreApp, engine string, artifacts []pipeline.Artifact, _ string) error {
		f.started = append(f.started, startedRelease{Engine: engine, Artifacts: artifacts})
		return f.startErr
	})
	return svc, f
}

func (f *engineFixture) repo() domain.Repository {
	repo, err := f.repos.Get(context.Background(), f.repoID)
	if err != nil {
		panic(err)
	}
	return repo
}

func (f *engineFixture) withEngine(engine string) domain.Repository {
	repo := f.repo()
	repo.ReleaseEngine = engine
	return repo
}

func TestResolveEngineAutoPrefersActionsThenLocalThenBlocks(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()

	engine, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineActions {
		t.Fatalf("engine = %q, err = %v; want github_actions", engine, err)
	}
	if f.localCalls != 0 {
		t.Fatal("the local runner was probed even though Actions could run")
	}

	f.actionsErr = errors.New("github api: 402 Payment Required")
	engine, err = svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineLocal {
		t.Fatalf("engine = %q, err = %v; want local", engine, err)
	}

	f.localHost = storeops.LocalRunnerHost{}
	if _, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine", err)
	}
}

func TestResolveEngineTellsABillingRefusalFromAnOutage(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()
	f.localHost = storeops.LocalRunnerHost{Paired: true, MacOS: true}

	for _, refusal := range []error{
		errors.New("github api: 402 Payment Required"),
		errors.New("github api: 403 The spending limit for this account has been reached"),
		errors.New("github api: 403 Actions is disabled for this repository"),
		storeops.ErrActionsNoWorkflow,
		storeops.ErrActionsBillingBlocked,
	} {
		f.actionsErr = refusal
		engine, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
		if err != nil || engine != domain.ReleaseEngineLocal {
			t.Fatalf("%v: engine = %q, err = %v; want a fallback to local", refusal, engine, err)
		}
	}

	f.actionsErr = errors.New("github api: 500 Internal Server Error")
	if _, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile); err == nil ||
		errors.Is(err, domain.ErrNoReleaseEngine) || strings.Contains(err.Error(), "local") {
		t.Fatalf("err = %v, want the transient failure propagated", err)
	}
}

func TestResolveEngineRefusesANonMacHostForIOS(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()

	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{Paired: true, MacOS: false}

	if _, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformIOS, testWorkflowFile); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine for iOS on a non-Mac host", err)
	}

	engine, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineLocal {
		t.Fatalf("engine = %q, err = %v; want local for android", engine, err)
	}
}

func TestResolveEngineNeverSubstitutesAPinnedEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()

	f.actionsErr = storeops.ErrActionsBillingBlocked
	if _, err := svc.ResolveEngine(ctx, f.withEngine(domain.ReleaseEngineActions), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, storeops.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want ErrEngineUnavailable", err)
	}
	if f.localCalls != 0 {
		t.Fatal("a repository pinned to Actions fell through to the local runner")
	}

	f.actionsErr = nil
	f.localHost = storeops.LocalRunnerHost{}
	if _, err := svc.ResolveEngine(ctx, f.withEngine(domain.ReleaseEngineLocal), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, storeops.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want ErrEngineUnavailable", err)
	}
}

func TestResolveEngineRejectsAnUnknownEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	if _, err := svc.ResolveEngine(context.Background(), f.withEngine("jenkins"), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, storeops.ErrInvalidEngine) {
		t.Fatalf("err = %v, want ErrInvalidEngine", err)
	}
}

func TestStartBuildParksTheTaskWhenNoEngineCanRun(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{}

	_, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif")
	if !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine", err)
	}
	if len(f.parker.Calls) != 1 {
		t.Fatalf("parks = %d, want the card parked exactly once", len(f.parker.Calls))
	}
	park := f.parker.Calls[0]
	if park.TaskID != f.taskID || park.Resource != domain.ResourceHumanDecision {
		t.Fatalf("park = %+v, want the onboarding task on human_decision", park)
	}
	if len(f.started) != 0 {
		t.Fatal("a release was started despite having no engine to start it on")
	}
	entries, _ := f.audit.List(context.Background(), &f.repoID, 0)
	if len(entries) != 1 || entries[0].Outcome != domain.OpsOutcomeError {
		t.Fatalf("audit = %+v, want one failed store_build row", entries)
	}
}

func TestStartBuildHonoursTheRequestedEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.actionsErr = errors.New("github api: 402 Payment Required")

	start, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, domain.ReleaseEngineLocal, "akif")
	if err != nil {
		t.Fatal(err)
	}
	if start.Engine != domain.ReleaseEngineLocal || len(f.started) != 1 || f.started[0].Engine != domain.ReleaseEngineLocal {
		t.Fatalf("start = %+v, started = %+v; want the local engine", start, f.started)
	}
	if f.actionsCalls != 0 {
		t.Fatal("Actions was probed for a run pinned to the local runner")
	}

	if len(start.Artifacts) != 2 || !strings.HasSuffix(start.Artifacts[0], "mobile-release.sh") {
		t.Fatalf("artifacts = %v, want the script first", start.Artifacts)
	}
}

func TestStartBuildRejectsAnUnknownEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "jenkins", "akif"); !errors.Is(err, storeops.ErrInvalidEngine) {
		t.Fatalf("err = %v, want ErrInvalidEngine", err)
	}
}

func TestStartBuildRefusesAnUnlinkedApp(t *testing.T) {
	svc, f := newEngineTestService(t)
	app, err := f.apps.Get(context.Background(), f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	app.Identifier = ""
	if _, err := f.apps.Upsert(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); !errors.Is(err, storeops.ErrAppNotReady) {
		t.Fatalf("err = %v, want ErrAppNotReady", err)
	}
}

func TestStartBuildShipsTheDetectedBuildTargets(t *testing.T) {
	svc, f := newEngineTestService(t)
	repo := f.repo()
	repo.DetectedBuildTargets = domain.BuildTargets{XcodeScheme: "Trooper", GradleModule: "androidApp"}
	f.repos.set(repo)

	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 1 {
		t.Fatalf("started = %d, want one release", len(f.started))
	}
	script := f.started[0].Artifacts[0].Body
	if !strings.Contains(script, "MODULE='androidApp'") {
		t.Fatalf("the release script does not build the detected module:\n%s", script)
	}
}

func TestStartBuildTakesTheSubProjectsOwnBuildTargets(t *testing.T) {
	svc, f := newEngineTestService(t)
	repo := f.repo()
	repo.Kind = domain.RepoKindMonorepo
	repo.SubProjects = []domain.RepoSubProject{{
		Path:                 "apps/mobile",
		Kind:                 domain.RepoKindMobile,
		DetectedAppIdentity:  domain.AppIdentity{PackageName: "com.example.android"},
		DetectedBuildTargets: domain.BuildTargets{GradleModule: "shell"},
	}}
	f.repos.set(repo)

	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); err != nil {
		t.Fatal(err)
	}
	script := f.started[0].Artifacts[0].Body
	if !strings.Contains(script, "MODULE='shell'") {
		t.Fatalf("the release script did not use the sub-project's own module:\n%s", script)
	}
}

func TestStartBuildRefusesWhenNoBuildTargetWasDetected(t *testing.T) {
	for _, tc := range []struct {
		platform string
		targets  domain.BuildTargets
		remedy   string
	}{
		{domain.MobileStorePlatformIOS, domain.BuildTargets{GradleModule: "app"}, "share the app's scheme in Xcode"},
		{domain.MobileStorePlatformAndroid, domain.BuildTargets{XcodeScheme: "Trooper"}, "settings.gradle includes the app module"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			svc, f := newEngineTestService(t)
			repo := f.repo()
			repo.DetectedBuildTargets = tc.targets
			f.repos.set(repo)

			_, err := svc.StartBuild(context.Background(), f.repoID, tc.platform, "", "akif")
			if !errors.Is(err, storeops.ErrBuildTargetUnknown) {
				t.Fatalf("err = %v, want ErrBuildTargetUnknown", err)
			}
			if !strings.Contains(err.Error(), tc.remedy) {
				t.Fatalf("err = %v, want it to say %q", err, tc.remedy)
			}
			if len(f.started) != 0 {
				t.Fatal("a release was started for a target nobody could name")
			}
			entries, _ := f.audit.List(context.Background(), &f.repoID, 0)
			if len(entries) != 1 || entries[0].Outcome != domain.OpsOutcomeError {
				t.Fatalf("audit = %+v, want one failed store_build row", entries)
			}
		})
	}
}

func TestStartBuildOnAnUnknownRepositoryIsNotFound(t *testing.T) {
	svc, _ := newEngineTestService(t)
	if _, err := svc.StartBuild(context.Background(), uuid.New(), domain.MobileStorePlatformAndroid, "", "akif"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func (f *engineFixture) unlinkOnboardingTask(t *testing.T, platform string) {
	t.Helper()
	app, err := f.apps.Get(context.Background(), f.repoID, platform)
	if err != nil {
		t.Fatal(err)
	}
	app.OnboardingTaskID = nil
	if _, err := f.apps.Upsert(context.Background(), app); err != nil {
		t.Fatal(err)
	}
}

func TestStartBuildProbesTheExactWorkflowItWillDispatch(t *testing.T) {
	svc, f := newEngineTestService(t)
	repo := f.repo()
	repo.Kind = domain.RepoKindMonorepo
	repo.SubProjects = []domain.RepoSubProject{{
		Path:                 "apps/mobile",
		Kind:                 domain.RepoKindMobile,
		DetectedAppIdentity:  domain.AppIdentity{PackageName: "com.example.android"},
		DetectedBuildTargets: domain.BuildTargets{GradleModule: "app"},
	}}
	f.repos.set(repo)

	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if f.probedWorkflow != "mobile-release-apps-mobile.yml" {
		t.Fatalf("probed %q, want the sub-project's own workflow file", f.probedWorkflow)
	}
}

func TestStartBuildHandsTheStarterTheFilesToDeliver(t *testing.T) {
	svc, f := newEngineTestService(t)
	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); err != nil {
		t.Fatal(err)
	}
	artifacts := f.started[0].Artifacts
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %d, want the script and the workflow", len(artifacts))
	}
	if artifacts[0].Body == "" || artifacts[0].Mode != 0o755 {
		t.Fatalf("script = %+v, want a body and mode 0755", artifacts[0])
	}
	if artifacts[1].Body == "" || artifacts[1].Mode != 0o644 {
		t.Fatalf("workflow = %+v, want a body and mode 0644", artifacts[1])
	}
}

func TestResolveEngineTakesActionsWhenTheOnlyGapIsTheWorkflowItWrites(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()
	f.actionsErr = storeops.ErrActionsNoWorkflow
	f.localHost = storeops.LocalRunnerHost{}

	engine, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineActions {
		t.Fatalf("engine = %q, err = %v; want github_actions", engine, err)
	}

	f.localHost = storeops.LocalRunnerHost{Paired: true, MacOS: true}
	engine, err = svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineLocal {
		t.Fatalf("engine = %q, err = %v; want local", engine, err)
	}

	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{}
	if _, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine", err)
	}
}

func TestStartBuildParksWhenTheStarterCannotDriveTheResolvedEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.startErr = fmt.Errorf("the local release engine is not wired: %w", domain.ErrNoReleaseEngine)

	_, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif")
	if !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine so the HTTP layer answers 409", err)
	}
	if len(f.parker.Calls) != 1 || f.parker.Calls[0].Resource != domain.ResourceHumanDecision {
		t.Fatalf("parks = %+v, want the card parked once on human_decision", f.parker.Calls)
	}
}

func TestStartBuildOpensACardToParkWhenTheAppHasNoTask(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.unlinkOnboardingTask(t, domain.MobileStorePlatformAndroid)
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{}

	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine", err)
	}
	if f.tasks.count() != 1 {
		t.Fatalf("cards opened = %d, want exactly one", f.tasks.count())
	}
	if title := f.tasks.Calls[0].Request.Title; !strings.Contains(title, "Release blocked") {
		t.Fatalf("card title = %q, want it to name the block", title)
	}

	app, err := f.apps.Get(context.Background(), f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	if app.OnboardingTaskID == nil {
		t.Fatal("the opened card was not recorded on the store app row")
	}
	if len(f.parker.Calls) != 1 || f.parker.Calls[0].TaskID != *app.OnboardingTaskID {
		t.Fatalf("parks = %+v, want the card that was just opened parked", f.parker.Calls)
	}
	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatal(err)
	}
	if f.tasks.count() != 1 {
		t.Fatalf("cards opened = %d after a second blocked build, want still one", f.tasks.count())
	}
}

func TestStartBuildSaysSoWhenTheParkItselfFails(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{}
	f.parker.Err = errors.New("board is down")

	_, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif")
	if !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine still matched", err)
	}
	if !strings.Contains(err.Error(), "parking the board card") {
		t.Fatalf("err = %v, want it to say the park failed", err)
	}
}
