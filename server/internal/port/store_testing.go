package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestFlightBuild is one uploaded iOS build as App Store Connect reports it.
type TestFlightBuild struct {
	ID               string
	BuildNumber      string // CFBundleVersion
	MarketingVersion string // CFBundleShortVersionString, from the build's preReleaseVersion
	ProcessingState  string // PROCESSING | FAILED | INVALID | VALID
	Expired          bool
	// UsesNonExemptEncryption is nil until someone has answered App Store
	// Connect's export compliance question for the build; until then no tester,
	// internal or external, can install it.
	UsesNonExemptEncryption *bool
	UploadedAt              time.Time
}

// BetaGroup is one TestFlight group.
type BetaGroup struct {
	ID       string
	Name     string
	Internal bool
	// AllBuilds is hasAccessToAllBuilds: an internal group that receives every
	// build without being added to it.
	AllBuilds  bool
	PublicLink string // "" when the public link is off
	// TesterCount is -1 when App Store Connect would not say.
	TesterCount int
}

type BetaTester struct {
	ID        string
	Email     string
	FirstName string
	LastName  string
	State     string // App Store Connect's betaTesterState / inviteType, verbatim
}

// TestFlightClient is App Store Connect's TestFlight surface. It is a separate
// port from AppStoreClient because only the test-build flow needs it.
type TestFlightClient interface {
	// LatestBuildSequence is the highest leading integer of any build number
	// the app holds, across every version train; 0 when it has none.
	LatestBuildSequence(ctx context.Context, appID string) (int64, error)
	// FindBuild looks a build up by its CFBundleVersion; found is false while
	// App Store Connect has not registered the upload yet.
	FindBuild(ctx context.Context, appID, buildNumber string) (build TestFlightBuild, found bool, err error)
	// SetWhatToTest writes the build's "What to Test" text for locale,
	// creating the localization if it does not exist.
	SetWhatToTest(ctx context.Context, buildID, locale, text string) error
	// DeclareEncryption answers the export compliance question for the build.
	DeclareEncryption(ctx context.Context, buildID string, usesNonExemptEncryption bool) error
	BetaGroups(ctx context.Context, appID string) ([]BetaGroup, error)
	CreateBetaGroup(ctx context.Context, appID, name string, internal bool) (BetaGroup, error)
	// BuildGroupIDs are the groups a build has been added to.
	BuildGroupIDs(ctx context.Context, buildID string) ([]string, error)
	AddBuildToGroups(ctx context.Context, buildID string, groupIDs []string) error
	RemoveBuildFromGroups(ctx context.Context, buildID string, groupIDs []string) error
	// SubmitForBetaReview submits a build to Beta App Review, which external
	// groups require; a build already submitted is not an error.
	SubmitForBetaReview(ctx context.Context, buildID string) error
	// BetaReviewState is WAITING_FOR_REVIEW | IN_REVIEW | REJECTED | APPROVED,
	// or "" when the build was never submitted.
	BetaReviewState(ctx context.Context, buildID string) (string, error)
	GroupTesters(ctx context.Context, groupID string) ([]BetaTester, error)
	// AddTester invites email into the group, creating the tester if App Store
	// Connect does not know them yet.
	AddTester(ctx context.Context, groupID, email, firstName, lastName string) (BetaTester, error)
	RemoveTester(ctx context.Context, groupID, testerID string) error
}

// PlayTrack is one Play testing or production track.
type PlayTrack struct {
	Name     string // internal | alpha | beta | production | a custom closed track's id
	Releases []PlayTrackRelease
}

type PlayTrackRelease struct {
	Name         string
	Status       string // completed | inProgress | halted | draft
	VersionCodes []int64
	UserFraction float64
}

// InternalShareLink is what Play answers for an internal app sharing upload.
type InternalShareLink struct {
	DownloadURL            string
	CertificateFingerprint string
	SHA256                 string
}

// PlayTestingClient is Google Play's testing surface. Separate from
// GooglePlayClient for the same reason TestFlightClient is.
type PlayTestingClient interface {
	// LatestVersionCode is the highest versionCode Play holds for the app —
	// every uploaded bundle and APK, on a track or not; 0 when it has none.
	LatestVersionCode(ctx context.Context, packageName string) (int64, error)
	// UploadInternalSharing uploads the AAB at aabPath to internal app sharing.
	// Internal app sharing places no constraint on the versionCode, which is
	// why every task build goes there first.
	UploadInternalSharing(ctx context.Context, packageName, aabPath string) (InternalShareLink, error)
	ListTracks(ctx context.Context, packageName string) ([]PlayTrack, error)
	// ReleaseToTrack makes versionCode the track's release, uploading the AAB
	// at aabPath first when Play does not hold that versionCode yet, and
	// commits the edit. A testing track's release is completed at once.
	ReleaseToTrack(ctx context.Context, packageName, track, aabPath string, versionCode int64, releaseName, notes string) error
}

// StoreTestBuildStore persists test builds.
type StoreTestBuildStore interface {
	Create(ctx context.Context, b domain.StoreTestBuild) (domain.StoreTestBuild, error)
	Update(ctx context.Context, b domain.StoreTestBuild) (domain.StoreTestBuild, error)
	Get(ctx context.Context, id uuid.UUID) (domain.StoreTestBuild, error)
	// List is newest first. platform "" = both; taskID nil = every task.
	List(ctx context.Context, repositoryID uuid.UUID, platform string, taskID *uuid.UUID, limit int) ([]domain.StoreTestBuild, error)
	// MaxSequence is the highest sequence recorded for the repository's app on
	// platform; 0 when none.
	MaxSequence(ctx context.Context, repositoryID uuid.UUID, platform string) (int64, error)
	// MaxAttempt is the highest attempt recorded for the task on platform.
	MaxAttempt(ctx context.Context, taskID uuid.UUID, platform string) (int, error)
	// ListUnfinished feeds the restart sweep: builds a previous process left
	// mid-flight.
	ListUnfinished(ctx context.Context) ([]domain.StoreTestBuild, error)
	// AutoGroups are the groups a ready build is opened to; set is false when
	// the repository never chose, so the platform default applies.
	AutoGroups(ctx context.Context, repositoryID uuid.UUID, platform string) (groups []string, set bool, err error)
	SetAutoGroups(ctx context.Context, repositoryID uuid.UUID, platform string, groups []string) error
}

// PipelineFile is one generated release-pipeline file (script or workflow).
type PipelineFile struct {
	Path string
	Body string
	Mode uint32
}

// MobileBuildRequest is one run of the generated release script's stage
// channel for a test build. The script, not the caller, knows every build
// step; the request only says where and with which build number.
type MobileBuildRequest struct {
	BuildID    uuid.UUID
	Platform   string
	Identifier string
	// Env carries the non-secret knobs the script reads: BUILD_NUMBER,
	// MOBILE_RELEASE_UPLOAD.
	Env map[string]string
	// Log receives the run's output line by line, already redacted.
	Log func(line string)

	// Local engine: ProjectDir is a checkout of the commit to build, at the app
	// project's root; Script is the generated script's body; Secrets the
	// signing material, which never reaches argv or an environment block.
	ProjectDir string
	Script     string
	Secrets    map[string]string

	// Actions engine: Files are committed to the default branch before the
	// dispatch, Ref is the commit to build, ArtifactDir is where the run's
	// artifact is unpacked afterwards.
	Repository  domain.Repository
	Files       []PipelineFile
	Workflow    string
	Ref         string
	ArtifactDir string
	// OnRunURL is called once the dispatched run is known.
	OnRunURL func(url string)
}

type MobileBuildResult struct {
	// Artifact is the signed .ipa/.aab the run produced on this machine; ""
	// when it produced none.
	Artifact string
	RunURL   string
}

// MobileBuilder runs one test build to completion. Run blocks until the
// script has finished; a non-nil error means the build failed, and its text is
// what a person is shown.
type MobileBuilder interface {
	Run(ctx context.Context, req MobileBuildRequest) (MobileBuildResult, error)
}

// LocalMobileBuilder is the engine on this machine.
type LocalMobileBuilder interface {
	MobileBuilder
	// Available says whether this machine can build platform, and why not.
	Available(ctx context.Context, platform string) (ok bool, reason string)
}
