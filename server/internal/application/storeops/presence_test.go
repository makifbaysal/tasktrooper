package storeops_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// The reported bug: an app already on sale, linked from the picker, stayed
// "unregistered" — no store action would run and the UI called it unpublished.
func TestLinkStoreAppAdoptsALiveIOSApp(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.setState(domain.MobileStoreStateUnregistered)
	f.asc.AppByBundleIDFound = true
	f.asc.AppByBundleIDID = "asc-app-1"
	f.asc.LiveVersionResult = port.AppStoreVersionInfo{Version: "3.2.0", State: "READY_FOR_SALE"}
	f.asc.LiveVersionFound = true

	app, err := svc.LinkStoreApp(context.Background(), f.repoID, domain.MobileStorePlatformIOS,
		port.StoreAppRef{Identifier: "com.example.ios"})
	if err != nil {
		t.Fatal(err)
	}
	if app.State != domain.MobileStoreStateLive {
		t.Fatalf("state = %q, want live", app.State)
	}
	if app.LastReleasedVersion != "3.2.0" {
		t.Fatalf("last released = %q, want 3.2.0", app.LastReleasedVersion)
	}
	if app.FirstPublishedAt != nil {
		t.Fatal("first_published_at is set, but nothing here saw the app's first publish")
	}
}

func TestLinkStoreAppMakesANotYetLiveIOSAppTestReady(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.setState(domain.MobileStoreStateUnregistered)
	f.asc.AppByBundleIDFound = true
	f.asc.AppByBundleIDID = "asc-app-1"

	app, err := svc.LinkStoreApp(context.Background(), f.repoID, domain.MobileStorePlatformIOS,
		port.StoreAppRef{Identifier: "com.example.ios"})
	if err != nil {
		t.Fatal(err)
	}
	if app.State != domain.MobileStoreStateTestReady {
		t.Fatalf("state = %q, want test_ready — an App Store Connect record takes TestFlight builds", app.State)
	}
}

func TestLinkStoreAppKeepsAPlayAppWithoutAFirstUploadOnboarding(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.setState(domain.MobileStoreStateUnregistered)
	f.play.AppExistsResult = true

	app, err := svc.LinkStoreApp(context.Background(), f.repoID, domain.MobileStorePlatformAndroid,
		port.StoreAppRef{Identifier: "com.example.android"})
	if err != nil {
		t.Fatal(err)
	}
	if app.State != domain.MobileStoreStateOnboarding {
		t.Fatalf("state = %q, want onboarding — Play takes the first upload from the console only", app.State)
	}
	if len(app.Checklist) != 1 || app.Checklist[0].Key != "play_first_upload" {
		t.Fatalf("checklist = %+v, want only the first-upload step", app.Checklist)
	}
}

func TestLinkStoreAppStillLinksWhenTheStoreReadFails(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.setState(domain.MobileStoreStateUnregistered)
	f.play.AppExistsResult = true
	f.play.LiveVersionErr = context.DeadlineExceeded

	app, err := svc.LinkStoreApp(context.Background(), f.repoID, domain.MobileStorePlatformAndroid,
		port.StoreAppRef{Identifier: "com.example.android"})
	if err != nil {
		t.Fatalf("link refused over a failed state read: %v", err)
	}
	if app.State != domain.MobileStoreStateUnregistered {
		t.Fatalf("state = %q, want unregistered until the monitor reads the console", app.State)
	}
}

func (s *MonitorSuite) TestSweepAdoptsALinkedButUnregisteredLiveApp() {
	s.storeASCCredential()
	s.asc.LiveVersionResult = port.AppStoreVersionInfo{Version: "2.0.0", State: "READY_FOR_SALE"}
	s.asc.LiveVersionFound = true
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateUnregistered,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, got.State)
	s.Equal("2.0.0", got.LastReleasedVersion)
}

func (s *MonitorSuite) TestSweepLeavesAnUnlinkedRowAlone() {
	s.storeASCCredential()
	s.asc.LiveVersionFound = true
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		State:        domain.MobileStoreStateUnregistered,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	s.Empty(s.asc.LiveVersionCalls, "a row with no app chosen has nothing to read")
}

func (s *MonitorSuite) TestSweepMovesAnOnboardingAppThatWentLiveByHand() {
	s.storePlayCredential()
	s.play.AppExistsResult = true
	s.play.LiveVersionResult = "1.0.0"
	s.play.LiveVersionFound = true
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateOnboarding,
		Checklist:    []domain.ChecklistItem{{Key: "play_first_upload", Title: "upload the first AAB"}},
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, got.State)
	s.True(got.ChecklistDone(), "a live app has done every onboarding step")
	s.NotNil(got.FirstPublishedAt, "the monitor watched this one go live")
}
