package storeops_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TestBuildSuite struct {
	suite.Suite

	prevKey    string
	prevKeySet bool

	apps     *fakeMobileStoreAppStore
	asc      *fakeASC
	play     *fakePlay
	push     *fakePushSecret
	comments *fakeCommenter
	builds   *fakeTestBuildStore
	tf       *fakeTestFlight
	pt       *fakePlayTesting
	local    *fakeMobileBuilder
	actions  *fakeMobileBuilder
	source   *fakeTestBuildSource

	svc    *storeops.Service
	repoID uuid.UUID
	task   domain.BoardTask
}

func TestTestBuildSuite(t *testing.T) {
	suite.Run(t, new(TestBuildSuite))
}

func (s *TestBuildSuite) SetupTest() {
	s.prevKey, s.prevKeySet = os.LookupEnv("MCP_SECRETS_KEY")
	s.Require().NoError(os.Setenv("MCP_SECRETS_KEY", "test-storeops-testbuild-key"))
	cipher, err := secrets.NewCipherFromEnv()
	s.Require().NoError(err)

	s.apps = newFakeMobileStoreAppStore()
	s.asc = &fakeASC{}
	s.play = &fakePlay{}
	s.push = &fakePushSecret{}
	s.comments = newFakeCommenter()
	s.builds = newFakeTestBuildStore()
	s.tf = newFakeTestFlight()
	s.pt = &fakePlayTesting{}
	s.local = &fakeMobileBuilder{OK: true, writeAAB: true, outputDir: s.T().TempDir()}
	s.actions = &fakeMobileBuilder{writeAAB: true, outputDir: s.T().TempDir()}
	s.source = &fakeTestBuildSource{SHA: "a1b2c3d4e5f6"}

	s.repoID = uuid.New()
	repos := newFakeRepositoryResolver()
	repos.set(domain.Repository{
		ID:                   s.repoID,
		Name:                 "trooper",
		DetectedBuildTargets: domain.BuildTargets{XcodeScheme: "App", GradleModule: "app"},
	})
	s.task = domain.BoardTask{ID: uuid.New(), RepositoryID: s.repoID, Key: "T-54", TaskNumber: 54, Title: "Checkout shows the coupon"}

	s.svc = storeops.NewService(storeops.Deps{
		Credentials: newFakeCredentialStore(),
		Apps:        s.apps,
		Signing:     newFakeSigningAssetStore(),
		Cipher:      cipher,
		NewASC:      newFakeASCFactory(s.asc, nil),
		NewPlay:     newFakePlayFactory(s.play, nil),
		PushSecret:  s.push.Push,
		Repos:       repos,
		Comments:    s.comments,
	})
	s.svc.SetTestBuilds(storeops.TestBuildDeps{
		Builds:         s.builds,
		NewTestFlight:  func(domain.StoreCredential) (port.TestFlightClient, error) { return s.tf, nil },
		NewPlayTesting: func(domain.StoreCredential) (port.PlayTestingClient, error) { return s.pt, nil },
		Local:          s.local,
		Actions:        s.actions,
		Source:         s.source,
		Tasks:          &fakeTaskReader{tasks: map[uuid.UUID]domain.BoardTask{s.task.ID: s.task}},
		ArtifactDir:    s.T().TempDir(),
		PollInterval:   time.Millisecond,
	})

	ctx := context.Background()
	s.Require().NoError(s.svc.SaveCredential(ctx, domain.StoreCredentialASC, map[string]string{
		"key_id": "K1MEE23AB", "issuer_id": "issuer", "p8": "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----",
	}))
	s.Require().NoError(s.svc.SaveCredential(ctx, domain.StoreCredentialGooglePlay, map[string]string{
		"service_account_json": `{"client_email":"deploy@project.iam.gserviceaccount.com","private_key":"fake"}`,
	}))
	s.ascIssuesSigning()
}

func (s *TestBuildSuite) TearDownTest() {
	s.svc.WaitTestBuilds()
	if s.prevKeySet {
		os.Setenv("MCP_SECRETS_KEY", s.prevKey)
	} else {
		os.Unsetenv("MCP_SECRETS_KEY")
	}
}

func (s *TestBuildSuite) ascIssuesSigning() {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	s.Require().NoError(err)
	expires := time.Now().Add(300 * 24 * time.Hour)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dist"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expires}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	s.Require().NoError(err)
	s.asc.CreateCertificateResult = port.StoreCert{ID: "CERT1", Serial: "1", DER: der, ExpiresAt: expires}
	s.asc.CreateProfileResult = port.StoreProfile{ID: "PROF1", Content: []byte("profile"), ExpiresAt: expires}
}

func (s *TestBuildSuite) linkApp(platform, state string) {
	app := domain.MobileStoreApp{RepositoryID: s.repoID, Platform: platform, State: state}
	if platform == domain.MobileStorePlatformIOS {
		app.Identifier, app.StoreAppID = "com.example.app", "asc-app-1"
	} else {
		app.Identifier = "com.example.android"
	}
	_, err := s.apps.Upsert(context.Background(), app)
	s.Require().NoError(err)
}

func (s *TestBuildSuite) start(platforms ...string) []domain.StoreTestBuild {
	builds, err := s.svc.StartTestBuilds(context.Background(), s.repoID, &s.task.ID, platforms, domain.TestBuildTriggerManual, "console")
	s.Require().NoError(err)
	s.svc.WaitTestBuilds()
	return builds
}

func (s *TestBuildSuite) reload(id uuid.UUID) domain.StoreTestBuild {
	b, err := s.builds.Get(context.Background(), id)
	s.Require().NoError(err)
	return b
}

func declared(v bool) *bool { return &v }

func (s *TestBuildSuite) TestIOSBuildIsNumberedAboveTheStoreAndOpenedToInternalGroups() {
	s.linkApp(domain.MobileStorePlatformIOS, domain.MobileStoreStateLive)
	s.tf.Sequence = 411
	s.tf.BuildFound = true
	s.tf.Build = port.TestFlightBuild{ProcessingState: "VALID", MarketingVersion: "1.4.0", UsesNonExemptEncryption: declared(false)}
	s.tf.Groups = []port.BetaGroup{
		{ID: "devs", Name: "Developers", Internal: true},
		{ID: "everyone", Name: "Team", Internal: true, AllBuilds: true},
		{ID: "beta", Name: "Public beta", Internal: false},
	}

	started := s.start(domain.MobileStorePlatformIOS)
	s.Require().Len(started, 1)
	s.Equal("412.54.1", started[0].BuildNumber)

	got := s.reload(started[0].ID)
	s.Equal(domain.TestBuildReady, got.Status, got.Failure)
	s.Equal(domain.ReleaseEngineLocal, got.Engine)
	s.Equal("1.4.0", got.VersionName)
	s.Equal("a1b2c3d4e5f6", got.CommitSHA)
	s.ElementsMatch([]string{"devs", "everyone"}, got.Groups, "external groups wait for a person; all-builds groups get it anyway")
	s.Equal([]string{"devs"}, s.tf.Added["asc-412.54.1"])
	s.Empty(s.tf.Submitted)
	s.Contains(s.tf.WhatToTest["asc-412.54.1"], "T-54 · #1 · a1b2c3d")

	req := s.local.requests()[0]
	s.Equal("412.54.1", req.Env["BUILD_NUMBER"])
	s.Contains(req.Secrets, "ASC_KEY_P8")
	s.Contains(req.Script, "xcodebuild")
	s.Require().NotEmpty(s.comments.Calls)
	s.Contains(s.comments.Calls[len(s.comments.Calls)-1].Request.Content, "412.54.1")
}

// The UAT round-trip: rejected, fixed, back in UAT — the next build says so.
func (s *TestBuildSuite) TestARebuildTakesTheNextAttemptAndSequence() {
	s.linkApp(domain.MobileStorePlatformIOS, domain.MobileStoreStateLive)
	s.tf.Sequence = 10
	s.tf.BuildFound = true
	s.tf.Build = port.TestFlightBuild{ProcessingState: "VALID", UsesNonExemptEncryption: declared(false)}

	first := s.start(domain.MobileStorePlatformIOS)
	s.source.SHA = "ffff000011112222"
	second := s.start(domain.MobileStorePlatformIOS)

	s.Equal("11.54.1", first[0].BuildNumber)
	s.Equal("12.54.2", second[0].BuildNumber)
}

func (s *TestBuildSuite) TestHumanUATDoesNotRebuildTheSameCommit() {
	s.linkApp(domain.MobileStorePlatformIOS, domain.MobileStoreStateLive)
	s.tf.BuildFound = true
	s.tf.Build = port.TestFlightBuild{ProcessingState: "VALID", UsesNonExemptEncryption: declared(false)}

	s.start(domain.MobileStorePlatformIOS)
	s.svc.OnTaskEnteredTestStage(context.Background(), s.repoID, s.task.ID)
	s.svc.WaitTestBuilds()

	all, err := s.builds.List(context.Background(), s.repoID, "", nil, 0)
	s.Require().NoError(err)
	s.Len(all, 1, "no new commit since the last ready build")

	s.source.SHA = "9999aaaa"
	s.svc.OnTaskEnteredTestStage(context.Background(), s.repoID, s.task.ID)
	s.svc.WaitTestBuilds()
	all, err = s.builds.List(context.Background(), s.repoID, "", nil, 0)
	s.Require().NoError(err)
	s.Len(all, 2)
}

func (s *TestBuildSuite) TestMissingExportComplianceWaitsForThePerson() {
	s.linkApp(domain.MobileStorePlatformIOS, domain.MobileStoreStateTestReady)
	s.tf.BuildFound = true
	s.tf.Build = port.TestFlightBuild{ProcessingState: "VALID"}
	s.tf.Groups = []port.BetaGroup{{ID: "devs", Internal: true}}

	started := s.start(domain.MobileStorePlatformIOS)
	got := s.reload(started[0].ID)
	s.Equal(domain.TestBuildActionRequired, got.Status)
	s.Empty(s.tf.Added)

	answered, err := s.svc.AnswerExportCompliance(context.Background(), s.repoID, got.ID, false, "console")
	s.Require().NoError(err)
	s.Equal(domain.TestBuildReady, answered.Status)
	s.Equal(false, s.tf.Declared[got.StoreBuildID])
	s.Equal([]string{"devs"}, answered.Groups)
}

func (s *TestBuildSuite) TestOpeningToAnExternalGroupSubmitsForBetaReview() {
	s.linkApp(domain.MobileStorePlatformIOS, domain.MobileStoreStateLive)
	s.tf.BuildFound = true
	s.tf.Build = port.TestFlightBuild{ProcessingState: "VALID", UsesNonExemptEncryption: declared(false)}
	s.tf.Groups = []port.BetaGroup{{ID: "beta", Name: "Public", Internal: false}}
	s.Require().NoError(s.svc.SetTestAutoGroups(context.Background(), s.repoID, domain.MobileStorePlatformIOS, []string{}))

	started := s.start(domain.MobileStorePlatformIOS)
	opened, err := s.svc.OpenTestBuild(context.Background(), s.repoID, started[0].ID, []string{"beta"}, "console")
	s.Require().NoError(err)
	s.Contains(opened.Groups, "beta")
	s.Equal([]string{opened.StoreBuildID}, s.tf.Submitted)

	closed, err := s.svc.CloseTestBuild(context.Background(), s.repoID, started[0].ID, []string{"beta"}, "console")
	s.Require().NoError(err)
	s.NotContains(closed.Groups, "beta")
}

func (s *TestBuildSuite) TestAndroidBuildGoesToInternalSharingAndCanBeReleasedToATrack() {
	s.linkApp(domain.MobileStorePlatformAndroid, domain.MobileStoreStateLive)
	s.pt.VersionCode = 1041

	started := s.start(domain.MobileStorePlatformAndroid)
	s.Require().Len(started, 1)
	s.Equal("1042", started[0].BuildNumber)

	got := s.reload(started[0].ID)
	s.Equal(domain.TestBuildReady, got.Status, got.Failure)
	s.True(got.HasArtifact)
	s.NotEmpty(got.InstallURL)
	s.Empty(got.Groups, "Android opens to no track until asked")
	s.Equal("false", s.local.requests()[0].Env["MOBILE_RELEASE_UPLOAD"])
	_, err := os.Stat(got.ArtifactPath)
	s.Require().NoError(err, "the bundle outlives the build's checkout")

	opened, err := s.svc.OpenTestBuild(context.Background(), s.repoID, got.ID, []string{"alpha"}, "console")
	s.Require().NoError(err)
	s.Equal([]string{"alpha"}, opened.Groups)
	s.Require().Len(s.pt.Releases, 1)
	s.Equal(int64(1042), s.pt.Releases[0].VersionCode)
	s.Equal(got.ArtifactPath, s.pt.Releases[0].AAB)
	s.True(strings.HasPrefix(s.pt.Releases[0].Name, "1042 (T-54"))

	_, err = s.svc.OpenTestBuild(context.Background(), s.repoID, got.ID, []string{"production"}, "console")
	s.Error(err)
}

func (s *TestBuildSuite) TestWithoutALocalToolchainTheBuildGoesToActions() {
	s.linkApp(domain.MobileStorePlatformAndroid, domain.MobileStoreStateLive)
	s.local.OK = false
	s.local.Reason = "no JDK"

	started := s.start(domain.MobileStorePlatformAndroid)
	got := s.reload(started[0].ID)
	s.Equal(domain.TestBuildReady, got.Status, got.Failure)
	s.Equal(domain.ReleaseEngineActions, got.Engine)
	s.Equal(1, s.source.Published)
	s.NotEmpty(s.push.Calls, "Actions reads the signing material from repository secrets")
	req := s.actions.requests()[0]
	s.Equal("a1b2c3d4e5f6", req.Ref)
	s.NotEmpty(req.Workflow)
	s.NotEmpty(req.Files)
}

func (s *TestBuildSuite) TestAFailedBuildSaysWhyOnTheTask() {
	s.linkApp(domain.MobileStorePlatformAndroid, domain.MobileStoreStateLive)
	s.local.Err = errors.New("release script exited 1: gradle: compilation failed")

	started := s.start(domain.MobileStorePlatformAndroid)
	got := s.reload(started[0].ID)
	s.Equal(domain.TestBuildFailed, got.Status)
	s.Contains(got.Failure, "compilation failed")
	s.NotEmpty(got.LogTail)
	s.Require().NotEmpty(s.comments.Calls)
	s.Contains(s.comments.Calls[len(s.comments.Calls)-1].Request.Content, "failed")
}

func (s *TestBuildSuite) TestAnAppThatCannotTakeTestBuildsIsRefusedWhenAskedFor() {
	s.linkApp(domain.MobileStorePlatformAndroid, domain.MobileStoreStateOnboarding)
	_, err := s.svc.StartTestBuilds(context.Background(), s.repoID, &s.task.ID, []string{domain.MobileStorePlatformAndroid}, domain.TestBuildTriggerManual, "console")
	s.ErrorIs(err, domain.ErrTestBuildAppNotTestable)

	_, err = s.svc.StartTestBuilds(context.Background(), s.repoID, &s.task.ID, []string{domain.MobileStorePlatformIOS}, domain.TestBuildTriggerManual, "console")
	s.ErrorIs(err, domain.ErrTestBuildNoStoreApp)
}

func (s *TestBuildSuite) TestTestGroupsListTracksWithoutProduction() {
	s.linkApp(domain.MobileStorePlatformAndroid, domain.MobileStoreStateLive)
	s.pt.Tracks = []port.PlayTrack{
		{Name: "internal", Releases: []port.PlayTrackRelease{{VersionCodes: []int64{40, 41}}}},
		{Name: "alpha"},
		{Name: "beta"},
		{Name: "production"},
	}
	groups, err := s.svc.TestGroups(context.Background(), s.repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.Require().Len(groups, 3)
	s.Equal("internal", groups[0].Kind)
	s.Equal("41", groups[0].CurrentBuild)
	s.Equal("closed", groups[1].Kind)
	s.Equal("open", groups[2].Kind)
}
