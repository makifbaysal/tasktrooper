package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Test build lifecycle. queued → building → (iOS: processing) → ready, with
// failed reachable from every non-terminal state and action_required parking a
// build on a decision only a person may make (export compliance).
const (
	TestBuildQueued         = "queued"
	TestBuildBuilding       = "building"
	TestBuildProcessing     = "processing"
	TestBuildActionRequired = "action_required"
	TestBuildReady          = "ready"
	TestBuildFailed         = "failed"
	// TestBuildDispatched is a release build handed to GitHub Actions: the
	// number is taken here so test builds never reuse it, and the run itself
	// is followed by the release flow, not by this row.
	TestBuildDispatched = "dispatched"
)

// TestBuildTerminal reports whether a build will not change state on its own.
func TestBuildTerminal(status string) bool {
	return status == TestBuildReady || status == TestBuildFailed || status == TestBuildDispatched
}

const (
	TestBuildTriggerHumanUAT = "human_uat"
	TestBuildTriggerManual   = "manual"
	TestBuildTriggerRelease  = "release"
)

// StoreTestBuild is one build of one task (or of the default branch when
// TaskID is nil) uploaded to a store's testing surface: TestFlight on iOS,
// internal app sharing on Android.
type StoreTestBuild struct {
	ID           uuid.UUID  `json:"id"`
	RepositoryID uuid.UUID  `json:"repository_id"`
	Platform     string     `json:"platform"`
	TaskID       *uuid.UUID `json:"task_id,omitempty"`
	TaskKey      string     `json:"task_key,omitempty"`
	TaskNumber   int        `json:"task_number,omitempty"`
	Attempt      int        `json:"attempt"`
	// Sequence is the store-wide monotonic counter both stores order builds
	// by: TestFlight refuses a build number below the highest of its version
	// train, Play a versionCode it has seen.
	Sequence    int64  `json:"sequence"`
	BuildNumber string `json:"build_number"`
	VersionName string `json:"version_name,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Engine      string `json:"engine,omitempty"`
	Status      string `json:"status"`
	Failure     string `json:"failure,omitempty"`
	// StoreBuildID is App Store Connect's build resource id once the upload
	// has been found; "" on Android.
	StoreBuildID string `json:"store_build_id,omitempty"`
	// InstallURL is the internal app sharing link on Android; TestFlight has
	// no per-build link, testers install from the TestFlight app.
	InstallURL string `json:"install_url,omitempty"`
	// ArtifactPath is the signed AAB kept on this machine so the same binary
	// can later be released to a Play track; never serialised.
	ArtifactPath string `json:"-"`
	HasArtifact  bool   `json:"has_artifact"`
	// Groups are the TestFlight beta group ids or Play track names this build
	// has been opened to.
	Groups     []string   `json:"groups"`
	Notes      string     `json:"notes,omitempty"`
	RunURL     string     `json:"run_url,omitempty"`
	LogTail    string     `json:"log_tail,omitempty"`
	Trigger    string     `json:"trigger"`
	CreatedBy  string     `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Label is how the build is named to a person: "T-54 · #2 · a1b2c3d".
func (b StoreTestBuild) Label() string {
	parts := make([]string, 0, 3)
	if b.TaskKey != "" {
		parts = append(parts, b.TaskKey)
	}
	if b.Attempt > 0 {
		parts = append(parts, "#"+strconv.Itoa(b.Attempt))
	}
	if sha := CommitSHA7(b.CommitSHA); sha != "" {
		parts = append(parts, sha)
	}
	return strings.Join(parts, " · ")
}

// CommitSHA7 is the seven-character form git prints in a one-line log, short
// enough to sit in a TestFlight note or a Play release name.
func CommitSHA7(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// TestBuildNumber is the store build number for one build: on iOS the
// CFBundleVersion "<sequence>.<task>.<attempt>" — three integers, the most
// App Store Connect accepts, ordered by the sequence first so every upload is
// higher than the last whatever task it belongs to — and on Android the bare
// versionCode, which Play wants as a single integer.
func TestBuildNumber(platform string, sequence int64, taskNumber, attempt int) string {
	if platform == MobileStorePlatformIOS && taskNumber > 0 && attempt > 0 {
		return fmt.Sprintf("%d.%d.%d", sequence, taskNumber, attempt)
	}
	return strconv.FormatInt(sequence, 10)
}

// MaxAndroidVersionCode is Play's ceiling for versionCode.
const MaxAndroidVersionCode = 2100000000

// LeadingBuildInteger is the first integer of a dotted build number, the part
// TestFlight compares first; ok is false for anything that does not start with
// one.
func LeadingBuildInteger(buildNumber string) (int64, bool) {
	head, _, _ := strings.Cut(strings.TrimSpace(buildNumber), ".")
	n, err := strconv.ParseInt(head, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

var (
	ErrTestBuildNotFound       = errors.New("test build not found")
	ErrTestBuildNoArtifact     = errors.New("this build's signed bundle is no longer on this machine; build the task again to release it to a track")
	ErrTestBuildNotReady       = errors.New("this build is not ready yet")
	ErrTestBuildUnsupported    = errors.New("this action does not apply to this platform")
	ErrTestBuildNoStoreApp     = errors.New("no store app is linked for this platform")
	ErrTestBuildAppNotTestable = errors.New("the store app cannot take test builds yet: finish its store onboarding first")
)

// StoreTestGroup is one place a test build can be opened to: a TestFlight
// beta group or a Play testing track.
type StoreTestGroup struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	// Kind is internal | external on iOS and internal | closed | open on
	// Android; production is never a test group.
	Kind string `json:"kind"`
	// AllBuilds marks a TestFlight internal group that receives every build
	// on its own; adding a build to it is a no-op.
	AllBuilds   bool   `json:"all_builds,omitempty"`
	PublicLink  string `json:"public_link,omitempty"`
	TesterCount int    `json:"tester_count"`
	// AutoDistribute marks the groups a new task build is opened to as soon
	// as it is ready.
	AutoDistribute bool `json:"auto_distribute"`
	// CurrentBuild is what the group/track currently serves, when known.
	CurrentBuild string `json:"current_build,omitempty"`
}

// StoreTester is one TestFlight tester of a group.
type StoreTester struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	State     string `json:"state,omitempty"`
}
