package simrun_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/simrun"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeRepos struct{ repo domain.Repository }

func (f fakeRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) { return f.repo, nil }

type fakeTasks struct{ task domain.BoardTask }

func (f fakeTasks) GetTask(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return f.task, nil
}

type fakeSource struct{ cleaned int }

func (f *fakeSource) CheckoutTask(context.Context, domain.Repository, domain.BoardTask) (string, string, func(), error) {
	return "/checkout", "abc1234", func() { f.cleaned++ }, nil
}

type fakeHost struct {
	mu      sync.Mutex
	err     error
	gate    chan struct{}
	request port.SimulatorLaunch
}

func (f *fakeHost) Devices(context.Context) ([]domain.SimulatorDevice, error) {
	return []domain.SimulatorDevice{
		{ID: "UDID-1", Name: "iPhone 16", Platform: domain.MobileStorePlatformIOS, State: "shutdown"},
		{ID: "avd:Pixel", Name: "Pixel", Platform: domain.MobileStorePlatformAndroid, State: "stopped"},
	}, nil
}

func (f *fakeHost) BuildAndLaunch(_ context.Context, req port.SimulatorLaunch) error {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	f.request = req
	f.mu.Unlock()
	req.Status(domain.SimulatorRunBuilding)
	req.Log("** BUILD SUCCEEDED **")
	return f.err
}

func newService(host *fakeHost, src *fakeSource) (*simrun.Service, uuid.UUID, uuid.UUID) {
	repoID, taskID := uuid.New(), uuid.New()
	repo := domain.Repository{
		ID: repoID, Kind: domain.RepoKindMobile, MobilePlatform: domain.MobilePlatformCross,
		DetectedBuildTargets: domain.BuildTargets{XcodeScheme: "App", GradleModule: "app"},
		DetectedAppIdentity:  domain.AppIdentity{BundleID: "com.example.app", PackageName: "com.example.android"},
	}
	svc := simrun.New(simrun.Deps{
		Repos: fakeRepos{repo}, Tasks: fakeTasks{domain.BoardTask{ID: taskID, Key: "T-54"}},
		Source: src, Host: host, CacheDir: "/cache",
	})
	return svc, repoID, taskID
}

func TestStartBuildsTheTaskAndReportsItRunning(t *testing.T) {
	host, src := &fakeHost{}, &fakeSource{}
	svc, repoID, taskID := newService(host, src)

	run, err := svc.Start(context.Background(), repoID, taskID, domain.MobileStorePlatformIOS, "UDID-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.DeviceName != "iPhone 16" {
		t.Fatalf("device name = %q", run.DeviceName)
	}
	svc.Wait()

	got, err := svc.Latest(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.SimulatorRunRunning || got.CommitSHA != "abc1234" || got.LogTail == "" {
		t.Fatalf("run = %+v", got)
	}
	if host.request.Scheme != "App" || host.request.Identifier != "com.example.app" || host.request.ProjectDir != "/checkout" {
		t.Fatalf("launch = %+v", host.request)
	}
	if src.cleaned != 1 {
		t.Fatal("the checkout was not removed after the run")
	}
}

func TestOnlyOneRunAtATime(t *testing.T) {
	host, src := &fakeHost{gate: make(chan struct{})}, &fakeSource{}
	svc, repoID, taskID := newService(host, src)

	if _, err := svc.Start(context.Background(), repoID, taskID, domain.MobileStorePlatformAndroid, "avd:Pixel"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Start(context.Background(), repoID, taskID, domain.MobileStorePlatformIOS, "UDID-1")
	if !errors.Is(err, domain.ErrSimulatorRunBusy) {
		t.Fatalf("err = %v, want busy", err)
	}
	close(host.gate)
	svc.Wait()
	if _, err := svc.Start(context.Background(), repoID, taskID, domain.MobileStorePlatformIOS, "UDID-1"); err != nil {
		t.Fatalf("a finished run still holds the machine: %v", err)
	}
	svc.Wait()
}

func TestAFailedBuildSaysWhy(t *testing.T) {
	host, src := &fakeHost{err: errors.New("the simulator build failed: xcodebuild exited: exit status 65")}, &fakeSource{}
	svc, repoID, taskID := newService(host, src)

	if _, err := svc.Start(context.Background(), repoID, taskID, domain.MobileStorePlatformIOS, "UDID-1"); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	got, _ := svc.Latest(taskID)
	if got.Status != domain.SimulatorRunFailed || got.Failure == "" {
		t.Fatalf("run = %+v", got)
	}
}

func TestStartRefusesAnUnknownDevice(t *testing.T) {
	svc, repoID, taskID := newService(&fakeHost{}, &fakeSource{})
	if _, err := svc.Start(context.Background(), repoID, taskID, domain.MobileStorePlatformIOS, "avd:Pixel"); !errors.Is(err, domain.ErrNoSimulators) {
		t.Fatalf("err = %v, want ErrNoSimulators for an Android device asked to run iOS", err)
	}
}
