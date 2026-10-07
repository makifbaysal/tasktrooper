package storeops_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type MonitorSuite struct {
	suite.Suite

	prevKey    string
	prevKeySet bool

	creds    *fakeCredentialStore
	apps     *fakeMobileStoreAppStore
	signing  *fakeSigningAssetStore
	asc      *fakeASC
	play     *fakePlay
	push     *fakePushSecret
	tasks    *fakeTaskCreator
	repos    *fakeRepositoryResolver
	comments *fakeCommenter
	ingester *fakeIncidentIngester
	cipher   *secrets.Cipher

	svc     *storeops.Service
	monitor *storeops.Monitor
}

func TestMonitorSuite(t *testing.T) {
	suite.Run(t, new(MonitorSuite))
}

func (s *MonitorSuite) SetupTest() {
	s.prevKey, s.prevKeySet = os.LookupEnv("MCP_SECRETS_KEY")
	s.Require().NoError(os.Setenv("MCP_SECRETS_KEY", "test-storeops-monitor-key"))

	cipher, err := secrets.NewCipherFromEnv()
	s.Require().NoError(err)
	s.cipher = cipher

	s.creds = newFakeCredentialStore()
	s.apps = newFakeMobileStoreAppStore()
	s.signing = newFakeSigningAssetStore()
	s.asc = &fakeASC{}
	s.play = &fakePlay{}
	s.push = &fakePushSecret{}
	s.tasks = newFakeTaskCreator()
	s.repos = newFakeRepositoryResolver()
	s.comments = newFakeCommenter()
	s.ingester = newFakeIncidentIngester()

	s.svc = storeops.NewService(storeops.Deps{
		Credentials: s.creds,
		Apps:        s.apps,
		Signing:     s.signing,
		Cipher:      s.cipher,
		NewASC:      newFakeASCFactory(s.asc, nil),
		NewPlay:     newFakePlayFactory(s.play, nil),
		PushSecret:  s.push.Push,
		Repos:       s.repos,
		Tasks:       s.tasks,
		Comments:    s.comments,
	})
	s.monitor = storeops.NewMonitor(s.svc, s.apps, s.ingester)
}

func (s *MonitorSuite) TearDownTest() {
	if s.prevKeySet {
		os.Setenv("MCP_SECRETS_KEY", s.prevKey)
	} else {
		os.Unsetenv("MCP_SECRETS_KEY")
	}
}

func (s *MonitorSuite) storeASCCredential() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialASC, map[string]string{
		"key_id":    "K1MEE23AB",
		"issuer_id": "69a6de8b-fake-issuer",
		"p8":        "-----BEGIN PRIVATE KEY-----\nfake-monitor-key-material\n-----END PRIVATE KEY-----",
	}))
}

func (s *MonitorSuite) storePlayCredential() {
	s.Require().NoError(s.svc.SaveCredential(context.Background(), domain.StoreCredentialGooglePlay, map[string]string{
		"service_account_json": `{"client_email":"deploy@project.iam.gserviceaccount.com","private_key":"fake"}`,
	}))
}

func (s *MonitorSuite) TestSweepOnboardingVerifiesAndAdvancesToTestReady() {
	s.storeASCCredential()
	s.asc.AppByBundleIDFound = true
	s.asc.AppByBundleIDID = "asc-app-9"
	repoID := uuid.New()
	taskID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:     repoID,
		Platform:         domain.MobileStorePlatformIOS,
		Identifier:       "com.example.app",
		State:            domain.MobileStoreStateOnboarding,
		Checklist:        []domain.ChecklistItem{{Key: "ios_app_record", Title: "register the bundle id"}},
		OnboardingTaskID: &taskID,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateTestReady, got.State)
	s.Require().Len(got.Checklist, 1)
	s.True(got.Checklist[0].Done)
	s.Equal("asc-app-9", got.StoreAppID)
	s.Require().Len(s.comments.Calls, 1, "VerifyOnboarding posts its own verification comment")
	s.Equal(taskID, s.comments.Calls[0].TaskID)
}

func (s *MonitorSuite) TestSweepTestReadyIOSGoesLiveAndComments() {
	s.storeASCCredential()
	s.asc.LiveVersionResult = port.AppStoreVersionInfo{Version: "1.0.0", State: "READY_FOR_SALE"}
	s.asc.LiveVersionFound = true
	repoID := uuid.New()
	taskID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:     repoID,
		Platform:         domain.MobileStorePlatformIOS,
		Identifier:       "com.example.app",
		StoreAppID:       "asc-app-1",
		State:            domain.MobileStoreStateTestReady,
		OnboardingTaskID: &taskID,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, got.State)
	s.Require().NotNil(got.FirstPublishedAt)
	s.Empty(got.ReviewState)
	s.Require().Len(s.comments.Calls, 1)
	s.Equal(taskID, s.comments.Calls[0].TaskID)
}

func (s *MonitorSuite) TestSweepTestReadyAndroidGoesLiveWithoutTaskDoesNotPanic() {
	s.storePlayCredential()
	s.play.LiveVersionResult = "2.0.0"
	s.play.LiveVersionFound = true
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)

	s.NotPanics(func() { s.monitor.Sweep(ctx) })

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, got.State)
	s.Require().NotNil(got.FirstPublishedAt)
	s.Empty(s.comments.Calls, "no onboarding task means no comment")
}

func (s *MonitorSuite) TestSweepTestReadyStaysWhenNotYetLive() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.0.0", State: "PREPARE_FOR_SUBMISSION"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateTestReady, got.State, "not READY_FOR_SALE yet: stays test_ready")
}

func (s *MonitorSuite) TestSweepLiveIOSApprovedSetsLastReleasedVersionAndComments() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.1.0", State: "READY_FOR_SALE"}
	repoID := uuid.New()
	taskID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformIOS,
		Identifier:           "com.example.app",
		StoreAppID:           "asc-app-1",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateInReview,
		LastSubmittedVersion: "1.1.0",
		OnboardingTaskID:     &taskID,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateApproved, got.ReviewState)
	s.Equal("1.1.0", got.LastReleasedVersion)
	s.Require().Len(s.comments.Calls, 1)
	s.Empty(s.ingester.Calls, "an approval is not an incident")
}

func (s *MonitorSuite) TestSweepLiveIOSRejectedIngestsIncidentExactlyOnceAcrossSweeps() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.2.0", State: "REJECTED"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformIOS,
		Identifier:           "com.example.app",
		StoreAppID:           "asc-app-1",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "1.2.0",
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateRejected, got.ReviewState)
	s.Require().Len(s.ingester.Calls, 1)
	incident := s.ingester.Calls[0]
	s.Equal(repoID, incident.RepositoryID)
	s.Equal(domain.IncidentSeverityHigh, incident.Severity)
	s.Equal("Store review rejected: com.example.app 1.2.0", incident.Title)
	s.Equal(domain.IncidentFingerprint("store_review", domain.MobileStorePlatformIOS, "1.2.0"), incident.Fingerprint)

	s.monitor.Sweep(ctx)

	s.Len(s.ingester.Calls, 1, "rejection must be ingested exactly once across repeated sweeps")
	s.Len(s.asc.LatestVersionCalls, 1, "a rejected row is skipped on the next sweep, not re-polled")
}

func (s *MonitorSuite) TestSweepLiveIOSRejectedRetriesAfterFailedIngest() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.3.0", State: "REJECTED"}
	s.ingester.Err = errors.New("incident service down")
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformIOS,
		Identifier:           "com.example.app",
		StoreAppID:           "asc-app-1",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "1.3.0",
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateWaiting, got.ReviewState, "a failed ingest must not mark the row rejected")
	s.Require().Len(s.ingester.Calls, 1, "the ingest was attempted")

	s.monitor.Sweep(ctx)
	s.Require().Len(s.ingester.Calls, 2, "still waiting: the row was polled and ingest retried")

	got, err = s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateWaiting, got.ReviewState, "still not marked rejected: the retry also failed")

	s.ingester.Err = nil
	s.monitor.Sweep(ctx)

	got, err = s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateRejected, got.ReviewState)
	s.Require().Len(s.ingester.Calls, 3, "the retry succeeded")

	s.monitor.Sweep(ctx)
	s.Len(s.ingester.Calls, 3, "rejected row no longer polled: no further ingest attempts")
}

func (s *MonitorSuite) TestSweepLiveAndroidHaltedRolloutRetriesAfterFailedIngest() {
	s.storePlayCredential()
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true, Status: "halted", VersionName: "4.0.0"}
	s.ingester.Err = errors.New("incident service down")
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformAndroid,
		Identifier:           "com.example.android",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "4.0.0",
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)
	s.Require().Len(s.ingester.Calls, 1, "the ingest was attempted")

	s.monitor.Sweep(ctx)
	s.Require().Len(s.ingester.Calls, 2, "still not notified: the retry was attempted again")

	s.ingester.Err = nil
	s.monitor.Sweep(ctx)
	s.Require().Len(s.ingester.Calls, 3, "the retry succeeded")

	s.monitor.Sweep(ctx)
	s.Len(s.ingester.Calls, 3, "now notified: no further ingest attempts")
}

func (s *MonitorSuite) TestSweepOneRowsErrorDoesNotAbortTheRest() {
	s.storePlayCredential()
	s.play.LiveVersionResult = "1.0.0"
	s.play.LiveVersionFound = true
	ctx := context.Background()

	repoIOS := uuid.New()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoIOS,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		State:        domain.MobileStoreStateOnboarding,
		Checklist:    []domain.ChecklistItem{{Key: "ios_app_record", Title: "register the bundle id"}},
	})
	s.Require().NoError(err)

	repoAndroid := uuid.New()
	_, err = s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoAndroid,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateTestReady,
	})
	s.Require().NoError(err)

	s.NotPanics(func() { s.monitor.Sweep(ctx) })

	iosRow, err := s.apps.Get(ctx, repoIOS, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateOnboarding, iosRow.State, "the errored row is left untouched, not crashed on")

	androidRow, err := s.apps.Get(ctx, repoAndroid, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal(domain.MobileStoreStateLive, androidRow.State, "the other row is still swept despite the first row's error")
}

func (s *MonitorSuite) TestSweepLiveAndroidHaltedRolloutIngestsIncidentAndDedupesInMemory() {
	s.storePlayCredential()
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true, Status: "halted", VersionName: "3.0.0"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformAndroid,
		Identifier:           "com.example.android",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "3.0.0",
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)
	s.monitor.Sweep(ctx)

	s.Require().Len(s.ingester.Calls, 1, "the halted rollout is ingested once")
	incident := s.ingester.Calls[0]
	s.Equal(repoID, incident.RepositoryID)
	s.Equal(domain.IncidentSeverityHigh, incident.Severity)
	s.Equal("Store rollout halted: com.example.android 3.0.0", incident.Title)
	s.Equal(domain.IncidentFingerprint("store_rollout", "com.example.android", "3.0.0"), incident.Fingerprint)
	s.Len(s.play.TrackInfoCalls, 2, "the row keeps polling; only the ingest is deduped")
}

func (s *MonitorSuite) TestSweepLiveSkipsWhenReviewStateNotPending() {
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)

	s.Empty(s.asc.LatestVersionCalls, "no pending review: nothing to poll")
	s.Empty(s.ingester.Calls)
}

func (s *MonitorSuite) TestSweepRenewsExpiringSigningEveryPass() {
	ctx := context.Background()
	s.storePlayCredential()
	repoID := uuid.New()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	_, err = s.svc.EnsureAndroidKeystore(ctx, "com.example.android")
	s.Require().NoError(err)
	asset, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	before := asset.Serial
	soon := time.Now().Add(10 * 24 * time.Hour)
	asset.ExpiresAt = &soon
	_, err = s.signing.Upsert(ctx, asset)
	s.Require().NoError(err)
	s.push.Calls = nil

	s.monitor.Sweep(ctx)

	after, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	s.NotEqual(before, after.Serial, "the sweep re-minted the soon-to-expire keystore")
	s.NotEmpty(s.push.Calls, "renewed secrets are pushed")
}

func (s *MonitorSuite) TestSweepToleratesNilIngester() {
	s.storeASCCredential()
	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "1.2.0", State: "REJECTED"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformIOS,
		Identifier:           "com.example.app",
		StoreAppID:           "asc-app-1",
		State:                domain.MobileStoreStateLive,
		ReviewState:          domain.ReviewStateWaiting,
		LastSubmittedVersion: "1.2.0",
	})
	s.Require().NoError(err)

	monitor := storeops.NewMonitor(s.svc, s.apps, nil)

	s.NotPanics(func() { monitor.Sweep(ctx) })

	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateRejected, got.ReviewState, "the row still transitions even without an ingester wired up")
}

func (s *MonitorSuite) TestSweepSkipsUnregisteredApp() {
	repoID := uuid.New()
	ctx := context.Background()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		State:        domain.MobileStoreStateUnregistered,
	})
	s.Require().NoError(err)

	s.NotPanics(func() { s.monitor.Sweep(ctx) })

	s.Empty(s.asc.AppByBundleIDCalls)
	s.Empty(s.asc.LatestVersionCalls)
}

func (s *MonitorSuite) TestStartDoesNotBlockAndStopIsSafeToCallTwice() {
	ctx := context.Background()

	s.monitor.Start(ctx, time.Hour)
	s.monitor.Stop()
	s.NotPanics(func() { s.monitor.Stop() }, "Stop must be safe to call twice")
}

func (s *MonitorSuite) TestMarkSubmittedOpensTheReviewGate() {
	s.storeASCCredential()
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformIOS,
		Identifier:   "com.example.app",
		StoreAppID:   "asc-app-1",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	s.monitor.Sweep(ctx)
	s.Empty(s.asc.LatestVersionCalls, "nothing submitted yet, nothing to poll")

	s.Require().NoError(s.svc.MarkSubmitted(ctx, repoID, domain.MobileStorePlatformIOS, ""))

	submitted, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateWaiting, submitted.ReviewState)

	s.asc.LatestVersionResult = port.AppStoreVersionInfo{Version: "2.0.0", State: "REJECTED"}
	s.monitor.Sweep(ctx)

	s.Len(s.asc.LatestVersionCalls, 1, "the submitted row is polled")
	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateRejected, got.ReviewState)
	s.Require().Len(s.ingester.Calls, 1)
	s.Equal("Store review rejected: com.example.app 2.0.0", s.ingester.Calls[0].Title)
}

func (s *MonitorSuite) TestMarkSubmittedRecordsVersionOnlyWhenKnown() {
	repoID := uuid.New()
	ctx := context.Background()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID:         repoID,
		Platform:             domain.MobileStorePlatformAndroid,
		Identifier:           "com.example.android",
		State:                domain.MobileStoreStateLive,
		LastSubmittedVersion: "1.0.0",
	})
	s.Require().NoError(err)

	s.Require().NoError(s.svc.MarkSubmitted(ctx, repoID, domain.MobileStorePlatformAndroid, ""))
	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal("1.0.0", got.LastSubmittedVersion, "an unknown version must not erase the recorded one")

	s.Require().NoError(s.svc.MarkSubmitted(ctx, repoID, domain.MobileStorePlatformAndroid, "1.1.0"))
	got, err = s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal("1.1.0", got.LastSubmittedVersion)
	s.Equal(domain.ReviewStateWaiting, got.ReviewState)
}

func (s *MonitorSuite) TestMarkSubmittedWithoutARowIsANoop() {
	ctx := context.Background()
	s.Require().NoError(s.svc.MarkSubmitted(ctx, uuid.New(), domain.MobileStorePlatformIOS, "1.0.0"))
	all, err := s.apps.ListAll(ctx)
	s.Require().NoError(err)
	s.Empty(all, "nothing is created for a repository that has no store app")
}

func (s *MonitorSuite) TestConcurrentSweepsIngestHaltedRolloutOnce() {
	s.storePlayCredential()
	s.play.TrackInfoResult = port.PlayTrackInfo{HasRelease: true, Status: "halted", VersionName: "3.0.0"}
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
		ReviewState:  domain.ReviewStateWaiting,
	})
	s.Require().NoError(err)

	arrived, release := s.ingester.block()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.monitor.Sweep(ctx)
		}()
	}

	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		release()
		wg.Wait()
		s.FailNow("no sweep reached the ingester")
	}
	release()
	wg.Wait()

	s.Len(s.ingester.Calls, 1, "overlapping sweeps must report a halted rollout once")
}

func (s *MonitorSuite) TestSweepRetriesPushAfterAFailedRenewalPush() {
	ctx := context.Background()
	s.storePlayCredential()
	repoID := uuid.New()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	_, err = s.svc.EnsureAndroidKeystore(ctx, "com.example.android")
	s.Require().NoError(err)
	asset, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	soon := time.Now().Add(10 * 24 * time.Hour)
	asset.ExpiresAt = &soon
	_, err = s.signing.Upsert(ctx, asset)
	s.Require().NoError(err)
	s.push.Calls = nil

	s.push.Err = errors.New("github is down")
	s.monitor.Sweep(ctx)
	s.NotEmpty(s.push.Calls, "the renewal attempted a push")

	renewed, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	s.True(renewed.ExpiresAt.After(time.Now().Add(365*24*time.Hour)),
		"the renewed keystore is far outside the renewal window, so ListExpiring will not return it again")

	s.push.Err = nil
	s.push.Calls = nil
	s.monitor.Sweep(ctx)

	names := map[string]bool{}
	for _, call := range s.push.Calls {
		s.Equal(repoID, call.RepositoryID)
		names[call.Name] = true
	}
	s.True(names["ANDROID_UPLOAD_KEYSTORE_B64"], "the next sweep re-pushes the stranded secrets: got %v", names)

	s.push.Calls = nil
	s.monitor.Sweep(ctx)
	s.Empty(s.push.Calls, "a successful push clears the retry")
}

func (s *MonitorSuite) TestSweepDoesNotRetryForeverWhenTheMintItselfFails() {
	ctx := context.Background()
	s.storePlayCredential()
	repoID := uuid.New()
	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.android",
		State:        domain.MobileStoreStateLive,
	})
	s.Require().NoError(err)

	_, err = s.svc.EnsureAndroidKeystore(ctx, "com.example.android")
	s.Require().NoError(err)
	asset, err := s.signing.Get(ctx, domain.SigningAssetUploadKeystore, "com.example.android")
	s.Require().NoError(err)
	soon := time.Now().Add(10 * 24 * time.Hour)
	asset.ExpiresAt = &soon
	_, err = s.signing.Upsert(ctx, asset)
	s.Require().NoError(err)

	s.push.Err = errors.New("github is down")
	s.push.Calls = nil
	s.monitor.Sweep(ctx)
	s.Require().NotEmpty(s.push.Calls, "the renewal attempted a push")

	s.Require().NoError(s.creds.Delete(ctx, domain.StoreCredentialGooglePlay))
	s.push.Err = nil
	s.push.Calls = nil
	s.monitor.Sweep(ctx)
	s.Empty(s.push.Calls, "a failed mint has nothing to push")

	s.storePlayCredential()
	s.push.Calls = nil
	s.monitor.Sweep(ctx)
	s.Empty(s.push.Calls, "a released claim must not resurrect the retry forever")
}

func (s *MonitorSuite) TestTrackSyncDoesNotEraseASubmitThatLandedMidSweep() {
	s.storePlayCredential()
	repoID := uuid.New()
	ctx := context.Background()

	_, err := s.apps.Upsert(ctx, domain.MobileStoreApp{
		RepositoryID: repoID,
		Platform:     domain.MobileStorePlatformAndroid,
		Identifier:   "com.example.midsweep",
		State:        domain.MobileStoreStateLive,
		ReviewState:  "",
	})
	s.Require().NoError(err)

	s.play.TracksHook = func() {
		s.Require().NoError(s.svc.MarkSubmitted(ctx, repoID, domain.MobileStorePlatformAndroid, "3.1.0"))
	}
	s.play.TracksResult = domain.StoreTracks{Internal: domain.TrackRelease{HasRelease: true, Version: "3.1.0"}}

	s.monitor.Sweep(ctx)

	s.Require().NotEmpty(s.play.TracksCalls, "the sweep never reached the store, so nothing was raced")
	got, err := s.apps.Get(ctx, repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Equal(domain.ReviewStateWaiting, got.ReviewState, "the submit was erased by the track sync")
	s.Equal("3.1.0", got.LastSubmittedVersion)

	s.Equal("3.1.0", got.Tracks.Internal.Version)
	s.Require().NotNil(got.TracksSyncedAt)
}
