package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Mobile store platforms.
const (
	MobileStorePlatformIOS     = "ios"
	MobileStorePlatformAndroid = "android"
)

// Mobile store app lifecycle. The row answers "is this a first publish or an
// update" — nothing else in the system may guess that.
const (
	MobileStoreStateUnregistered = "unregistered"
	MobileStoreStateOnboarding   = "onboarding"
	MobileStoreStateTestReady    = "test_ready"
	MobileStoreStateLive         = "live"
)

// Review states mirrored from the stores after a prod submit.
const (
	ReviewStateWaiting  = "waiting_for_review"
	ReviewStateInReview = "in_review"
	ReviewStateApproved = "approved"
	ReviewStateRejected = "rejected"
)

var (
	// ErrMobileAppNotLive blocks TriggerRelease: the first store submit is a
	// manual product decision (listing, screenshots, privacy forms).
	ErrMobileAppNotLive = errors.New("mobile app is not live in the store yet: finish the first manual store submit; automatic prod submits start after that")
	// ErrMobileAppNotTestReady blocks stage deploys while onboarding items (app
	// record, first Play upload) are still open.
	ErrMobileAppNotTestReady = errors.New("mobile app onboarding is not finished: complete the store onboarding checklist task first")
)

// ChecklistItem is one manual onboarding step, verified against the store API
// — never trusted from user claims.
type ChecklistItem struct {
	Key        string     `json:"key"`
	Title      string     `json:"title"`
	Done       bool       `json:"done"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
}

// MobileStoreApp is one repository's presence on one store.
type MobileStoreApp struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	Platform     string    `json:"platform"`
	Identifier   string    `json:"identifier"` // bundle ID / package name
	StoreAppID   string    `json:"store_app_id,omitempty"`
	// AppName is the store console's own display name, written the moment the
	// app is chosen; "" = not chosen yet.
	AppName              string          `json:"app_name,omitempty"`
	State                string          `json:"state"`
	ReviewState          string          `json:"review_state,omitempty"`
	LastSubmittedVersion string          `json:"last_submitted_version,omitempty"`
	LastReleasedVersion  string          `json:"last_released_version,omitempty"`
	Checklist            []ChecklistItem `json:"checklist,omitempty"`
	OnboardingTaskID     *uuid.UUID      `json:"onboarding_task_id,omitempty"`
	FirstPublishedAt     *time.Time      `json:"first_published_at,omitempty"`
	// Tracks is a cache of the three store channels — see StoreTracks. Always
	// serialised, zero value included, because a consumer diffing "have we ever
	// synced" needs {} and an absent key to read the same way.
	Tracks StoreTracks `json:"tracks"`
	// TracksSyncedAt is when Tracks was last refreshed, nil when it never has
	// been — a zero StoreTracks cannot by itself tell "never synced" from
	// "synced once, found nothing".
	TracksSyncedAt *time.Time `json:"tracks_synced_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (a MobileStoreApp) ChecklistDone() bool {
	for _, item := range a.Checklist {
		if !item.Done {
			return false
		}
	}
	return true
}

// CanTransition encodes the lifecycle: forward one step at a time, with one
// backward edge (test_ready -> onboarding) for re-verification failures.
func (a MobileStoreApp) CanTransition(to string) bool {
	switch a.State {
	case MobileStoreStateUnregistered:
		return to == MobileStoreStateOnboarding
	case MobileStoreStateOnboarding:
		return to == MobileStoreStateTestReady
	case MobileStoreStateTestReady:
		return to == MobileStoreStateLive || to == MobileStoreStateOnboarding
	}
	return false
}

// IsStoreProvider reports whether a deploy provider ships to an app store (and
// therefore goes through the mobile store lifecycle gates).
func IsStoreProvider(p string) bool {
	return p == DeployProviderAppStore || p == DeployProviderGooglePlay
}

// StoreProviderPlatform maps a store deploy provider to its platform.
func StoreProviderPlatform(p string) string {
	switch p {
	case DeployProviderAppStore:
		return MobileStorePlatformIOS
	case DeployProviderGooglePlay:
		return MobileStorePlatformAndroid
	}
	return ""
}

// Store credential providers (the "which console" axis).
const (
	StoreCredentialASC        = "asc"
	StoreCredentialGooglePlay = "google_play"
)

// StoreCredential is the decrypted view handed to clients; the store layer only
// ever persists the encrypted payload.
type StoreCredential struct {
	Provider  string            `json:"provider"`
	Data      map[string]string `json:"data"` // asc: key_id, issuer_id, p8 ; google_play: service_account_json
	UpdatedAt time.Time         `json:"updated_at"`
}

// Signing asset kinds.
const (
	SigningAssetDistCert       = "dist_cert"       // identifier "" (one per Apple team)
	SigningAssetProfile        = "profile"         // identifier = bundle ID
	SigningAssetUploadKeystore = "upload_keystore" // identifier = package name
)

// SigningAsset is an encrypted signing artifact the system generated and owns.
type SigningAsset struct {
	ID         uuid.UUID  `json:"id"`
	Kind       string     `json:"kind"`
	Identifier string     `json:"identifier"`
	Serial     string     `json:"serial,omitempty"`
	Data       []byte     `json:"-"` // decrypted blob (p12 / mobileprovision / JKS), never serialized
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Store release channels — the product's own vocabulary for where a build sits.
// Each store spells them differently (TestFlight internal groups vs Play's
// `internal` track), and the adapters translate; nothing above them has to know
// which store it is looking at.
const (
	StoreChannelInternal   = "internal"
	StoreChannelExternal   = "external"
	StoreChannelProduction = "production"
)

// ValidStoreChannel reports whether c is one of the three channels.
func ValidStoreChannel(c string) bool {
	switch c {
	case StoreChannelInternal, StoreChannelExternal, StoreChannelProduction:
		return true
	}
	return false
}

// NextChannel is the one legal promotion step out of c: strictly forward, one
// at a time — nothing goes straight from internal to production, because on
// both stores that would skip the review/testing stage the middle channel
// exists to hold.
func NextChannel(c string) (string, bool) {
	switch c {
	case StoreChannelInternal:
		return StoreChannelExternal, true
	case StoreChannelExternal:
		return StoreChannelProduction, true
	}
	return "", false
}

// Normalized track statuses. The raw store words differ (ASC's
// READY_FOR_SALE/WAITING_FOR_REVIEW, Play's completed/inProgress/halted), so the
// adapters map onto this set and the UI renders one vocabulary.
const (
	TrackStatusNone       = "none"        // nothing has ever reached this channel
	TrackStatusDraft      = "draft"       // uploaded, not handed to anyone yet
	TrackStatusInReview   = "in_review"   // the store is looking at it
	TrackStatusRollingOut = "rolling_out" // live for a fraction of users
	TrackStatusHalted     = "halted"      // rollout stopped in place
	TrackStatusLive       = "live"        // fully released on this channel
	TrackStatusUnknown    = "unknown"     // store reported a status this adapter does not map; the channel is NOT empty
)

// TrackRelease is what one channel currently holds. HasRelease false means the
// channel is empty — every other field is then meaningless, which is why it is
// stated rather than inferred from an empty Version.
type TrackRelease struct {
	HasRelease bool   `json:"has_release"`
	Version    string `json:"version,omitempty"` // marketing version / versionName
	Build      string `json:"build,omitempty"`   // build number / versionCode
	Status     string `json:"status,omitempty"`  // one of TrackStatus*
	// UserFraction is the staged-rollout share, 0 when the release is not
	// staged. Only production stages on either store.
	UserFraction float64 `json:"user_fraction,omitempty"`
	// Audience is the store's own count for this channel, already worded for
	// display ("12 internal testers", "2 groups · 340 people"). Free text
	// because the two stores count different things.
	Audience  string     `json:"audience,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// PendingVersion is a newer version queued behind this one — drafted, in
	// review or approved but not released — so a live channel can say both what
	// customers have and what is coming. "" = nothing queued.
	PendingVersion string `json:"pending_version,omitempty"`
	PendingStatus  string `json:"pending_status,omitempty"`
}

// StoreTracks is one app's three channels, read from the store console and
// persisted on the row as a cache so the panel renders without a live store
// round-trip; TracksSyncedAt says how stale it is.
type StoreTracks struct {
	Internal   TrackRelease `json:"internal"`
	External   TrackRelease `json:"external"`
	Production TrackRelease `json:"production"`
}

// Channel returns the named channel, and whether the name was one of the three.
func (t StoreTracks) Channel(name string) (TrackRelease, bool) {
	switch name {
	case StoreChannelInternal:
		return t.Internal, true
	case StoreChannelExternal:
		return t.External, true
	case StoreChannelProduction:
		return t.Production, true
	}
	return TrackRelease{}, false
}

// ReleaseEngineAuto is the default and means "GitHub Actions, and the local
// runner when Actions cannot run" — the failure this exists for is an org whose
// Actions are blocked at the billing level, which is not a build error and must
// not read as one. When NEITHER engine can run, the release is blocked rather
// than quietly downgraded: an iOS build needs macOS, and an install with no
// paired Mac and no Actions minutes has no honest way to ship.
const (
	ReleaseEngineAuto    = "auto"
	ReleaseEngineActions = "github_actions"
	ReleaseEngineLocal   = "local"
)

// ValidReleaseEngine reports whether e is a known engine; "" is not valid — the
// column defaults to ReleaseEngineAuto, so an empty value is a bug.
func ValidReleaseEngine(e string) bool {
	switch e {
	case ReleaseEngineAuto, ReleaseEngineActions, ReleaseEngineLocal:
		return true
	}
	return false
}

// ErrNoReleaseEngine blocks a mobile release when GitHub Actions cannot be
// dispatched and no local runner is paired — callers surface it by moving the
// task to blocked, never by falling back to a third path, because there isn't
// one.
var ErrNoReleaseEngine = errors.New("no release engine available: GitHub Actions cannot run and no local runner is paired for this platform")
