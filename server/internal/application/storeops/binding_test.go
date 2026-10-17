package storeops_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestListStoreAppsKeepsTheUnavailableAnswerMatchable(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.play.ListAppsErr = port.ErrAppListingUnavailable

	_, err := svc.ListStoreApps(context.Background(), domain.StoreCredentialGooglePlay)
	if !errors.Is(err, port.ErrAppListingUnavailable) {
		t.Fatalf("err = %v, want a wrapped ErrAppListingUnavailable", err)
	}
	if errors.Is(err, storeops.ErrInvalidCredential) {
		t.Fatal("an unavailable listing was reported as a bad credential")
	}
}

func TestListStoreAppsSortsTheListing(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.asc.ListAppsResult = []port.StoreAppRef{
		{Identifier: "com.example.zed", Name: "Zed"},
		{Identifier: "com.example.alpha", Name: "Alpha"},
	}
	apps, err := svc.ListStoreApps(context.Background(), domain.StoreCredentialASC)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].Identifier != "com.example.alpha" {
		t.Fatalf("apps = %+v, want a deterministic order", apps)
	}
}

func TestLinkStoreAppRefusesAnAppTheStoreDoesNotHave(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.setState(domain.MobileStoreStateOnboarding)
	f.play.AppExistsResult = false

	_, err := svc.LinkStoreApp(context.Background(), f.repoID, domain.MobileStorePlatformAndroid,
		port.StoreAppRef{Identifier: "com.example.ghost"})
	if !errors.Is(err, storeops.ErrAppNotInStore) {
		t.Fatalf("err = %v, want ErrAppNotInStore", err)
	}
}

func TestLinkStoreAppLeavesTheLifecycleAlone(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()
	f.setState(domain.MobileStoreStateOnboarding)
	f.asc.AppByBundleIDFound = true
	f.asc.AppByBundleIDID = "asc-confirmed"

	app, err := svc.LinkStoreApp(ctx, f.repoID, domain.MobileStorePlatformIOS,
		port.StoreAppRef{Identifier: "com.example.ios", StoreAppID: "stale-from-the-picker", Name: "Trooper"})
	if err != nil {
		t.Fatal(err)
	}
	if app.State != domain.MobileStoreStateOnboarding {
		t.Fatalf("state = %q, want onboarding untouched", app.State)
	}

	if app.StoreAppID != "asc-confirmed" || app.AppName != "Trooper" {
		t.Fatalf("app = %+v, want the console's own app id and name", app)
	}
}

func TestLinkStoreAppRefusesRepointingALiveApp(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	f.play.AppExistsResult = true

	_, err := svc.LinkStoreApp(context.Background(), f.repoID, domain.MobileStorePlatformAndroid,
		port.StoreAppRef{Identifier: "com.example.other"})
	if !errors.Is(err, storeops.ErrIdentifierLocked) {
		t.Fatalf("err = %v, want ErrIdentifierLocked", err)
	}
}

func TestLinkStoreAppRejectsAnUnknownPlatform(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	if _, err := svc.LinkStoreApp(context.Background(), f.repoID, "windows",
		port.StoreAppRef{Identifier: "com.example.x"}); !errors.Is(err, storeops.ErrInvalidPlatform) {
		t.Fatalf("err = %v, want ErrInvalidPlatform", err)
	}
}

func TestTracksCachesWhatTheStoreReported(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()
	f.play.TracksResult = domain.StoreTracks{
		Internal: domain.TrackRelease{HasRelease: true, Version: "1.2.3", Status: domain.TrackStatusLive},
	}

	tracks, err := svc.Tracks(ctx, f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	if tracks.Internal.Version != "1.2.3" {
		t.Fatalf("tracks = %+v", tracks)
	}
	app, err := f.apps.Get(ctx, f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	if app.Tracks.Internal.Version != "1.2.3" || app.TracksSyncedAt == nil {
		t.Fatalf("row = %+v, want the channel cache written with a sync stamp", app)
	}
}

func TestPromoteChannelRefusesASkippedChannel(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelInternal, domain.StoreChannelProduction, 1, "trooper", "akif")
	if !errors.Is(err, storeops.ErrInvalidChannel) {
		t.Fatalf("err = %v, want ErrInvalidChannel", err)
	}
	if len(f.asc.PromoteChannelCalls) != 0 {
		t.Fatal("a skipped promotion reached App Store Connect")
	}
}

func TestPromoteChannelRefusesBackwardsAndUnknownChannels(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	for _, pair := range [][2]string{
		{domain.StoreChannelProduction, domain.StoreChannelExternal},
		{domain.StoreChannelExternal, domain.StoreChannelInternal},
		{domain.StoreChannelProduction, domain.StoreChannelProduction},
		{"nightly", domain.StoreChannelProduction},
	} {
		err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS, pair[0], pair[1], 1, "trooper", "akif")
		if !errors.Is(err, storeops.ErrInvalidChannel) {
			t.Fatalf("%v -> %v: err = %v, want ErrInvalidChannel", pair[0], pair[1], err)
		}
	}
}

func TestPromoteChannelToProductionRequiresTheConfirmPhrase(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelExternal, domain.StoreChannelProduction, 1, "wrong", "akif")
	if !errors.Is(err, storeops.ErrConfirmMismatch) {
		t.Fatalf("err = %v, want ErrConfirmMismatch", err)
	}
	if len(f.asc.PromoteChannelCalls) != 0 {
		t.Fatal("promoted to production despite a failed confirmation")
	}
	entries, _ := f.audit.List(ctx, &f.repoID, 0)
	if len(entries) != 1 || entries[0].Outcome != domain.OpsOutcomeError {
		t.Fatalf("audit = %+v, want the refused attempt recorded", entries)
	}
}

func TestPromoteChannelRefusesProductionForAnAppThatIsNotLive(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()
	f.setState(domain.MobileStoreStateTestReady)

	err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelExternal, domain.StoreChannelProduction, 1, "trooper", "akif")
	if !errors.Is(err, storeops.ErrAppNotReady) {
		t.Fatalf("err = %v, want ErrAppNotReady", err)
	}
	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelInternal, domain.StoreChannelExternal, 1, "", "akif"); err != nil {
		t.Fatalf("a test-channel promotion was refused for a test_ready app: %v", err)
	}
}

func TestPromoteChannelTranslatesAndroidTrackNames(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformAndroid,
		domain.StoreChannelInternal, domain.StoreChannelExternal, 1, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if len(f.play.PromoteTrackCalls) != 1 || f.play.PromoteTrackCalls[0].ToTrack != "beta" {
		t.Fatalf("calls = %+v, want open testing when nothing says otherwise", f.play.PromoteTrackCalls)
	}

	app, err := f.apps.Get(ctx, f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	app.Tracks.External = domain.TrackRelease{HasRelease: true, Audience: "Closed testing"}
	if _, err := f.apps.Upsert(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformAndroid,
		domain.StoreChannelExternal, domain.StoreChannelProduction, 1, "trooper", "akif"); err != nil {
		t.Fatal(err)
	}
	last := f.play.PromoteTrackCalls[len(f.play.PromoteTrackCalls)-1]
	if last.FromTrack != "alpha" || last.ToTrack != "production" {
		t.Fatalf("call = %+v, want alpha -> production", last)
	}
}

func TestPromoteChannelStartsAPlayProductionPromoteAtTheGivenShare(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformAndroid,
		domain.StoreChannelExternal, domain.StoreChannelProduction, 0.1, "trooper", "akif"); err != nil {
		t.Fatal(err)
	}
	if last := f.play.PromoteTrackCalls[len(f.play.PromoteTrackCalls)-1]; last.ToTrack != "production" || last.UserFraction != 0.1 {
		t.Fatalf("call = %+v, want production at 0.1", last)
	}

	// Only into production: a testing channel opens to everyone whatever is sent.
	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformAndroid,
		domain.StoreChannelInternal, domain.StoreChannelExternal, 0.1, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if last := f.play.PromoteTrackCalls[len(f.play.PromoteTrackCalls)-1]; last.UserFraction != 1 {
		t.Fatalf("call = %+v, want a testing promote at 1", last)
	}
}

func TestPromoteChannelRefusesAPlayProductionShareOutsideZeroToOne(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	ctx := context.Background()

	for _, bad := range []float64{0, -0.5, 1.2} {
		err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformAndroid,
			domain.StoreChannelExternal, domain.StoreChannelProduction, bad, "trooper", "akif")
		if !errors.Is(err, storeops.ErrInvalidRolloutFraction) {
			t.Fatalf("fraction %v: err = %v, want ErrInvalidRolloutFraction", bad, err)
		}
	}
	if len(f.play.PromoteTrackCalls) != 0 {
		t.Fatalf("an out-of-range share reached Play: %+v", f.play.PromoteTrackCalls)
	}
	// App Store's production step is App Review; the share means nothing there.
	if err := svc.PromoteChannel(ctx, f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelExternal, domain.StoreChannelProduction, 0, "trooper", "akif"); err != nil {
		t.Fatalf("an iOS promote was refused over a share it ignores: %v", err)
	}
}

func TestPromoteChannelUsesTheASCResourceIDForIOS(t *testing.T) {
	svc, f := newReleaseTestService(t, "trooper")
	if err := svc.PromoteChannel(context.Background(), f.repoID, domain.MobileStorePlatformIOS,
		domain.StoreChannelInternal, domain.StoreChannelExternal, 1, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if len(f.asc.PromoteChannelCalls) != 1 || f.asc.PromoteChannelCalls[0] != [3]string{"asc-app-1", "internal", "external"} {
		t.Fatalf("calls = %+v", f.asc.PromoteChannelCalls)
	}
}

func TestPromoteChannelOnAnUnknownRepositoryIsNotFound(t *testing.T) {
	svc, _ := newReleaseTestService(t, "trooper")
	err := svc.PromoteChannel(context.Background(), uuid.New(), domain.MobileStorePlatformIOS,
		domain.StoreChannelInternal, domain.StoreChannelExternal, 1, "", "akif")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
