package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var ErrNotFound = errors.New("not found")

var ErrAppListingUnavailable = errors.New("store app listing is unavailable for this credential")

type StoreAppRef struct {
	StoreAppID string `json:"store_app_id"` // ASC app resource id; "" for Play, which keys on the package name
	Identifier string `json:"identifier"`   // bundle ID / package name
	Name       string `json:"name"`         // display name as the console shows it
	State      string `json:"state,omitempty"`
}

type StoreCredentialStore interface {
	Set(ctx context.Context, provider string, encrypted []byte) error
	Get(ctx context.Context, provider string) ([]byte, time.Time, error)
	Delete(ctx context.Context, provider string) error
	List(ctx context.Context) (map[string]time.Time, error)
}

type MobileStoreAppStore interface {
	Upsert(ctx context.Context, app domain.MobileStoreApp) (domain.MobileStoreApp, error)
	SetTracks(ctx context.Context, repositoryID uuid.UUID, platform string, tracks domain.StoreTracks, syncedAt time.Time) (domain.MobileStoreApp, error)
	Get(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.MobileStoreApp, error)
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error)
	// ListAll feeds storeops.Monitor.
	ListAll(ctx context.Context) ([]domain.MobileStoreApp, error)
}

type SigningAssetStore interface {
	Upsert(ctx context.Context, asset domain.SigningAsset) (domain.SigningAsset, error)
	Get(ctx context.Context, kind, identifier string) (domain.SigningAsset, error)
	ListExpiring(ctx context.Context, before time.Time) ([]domain.SigningAsset, error)
}

type StoreCert struct {
	ID        string
	Serial    string
	DER       []byte
	ExpiresAt time.Time
}

type StoreProfile struct {
	ID        string
	Name      string
	Content   []byte // decoded .mobileprovision
	ExpiresAt time.Time
}

type AppStoreVersionInfo struct {
	Version string
	State   string // raw ASC state e.g. READY_FOR_SALE, WAITING_FOR_REVIEW, IN_REVIEW, REJECTED, PENDING_DEVELOPER_RELEASE
}

type AppStoreClient interface {
	ValidateAuth(ctx context.Context) error
	AppByBundleID(ctx context.Context, bundleID string) (appID string, found bool, err error)
	EnsureBundleID(ctx context.Context, bundleID, name string) error // register if absent, idempotent
	CreateCertificate(ctx context.Context, csrPEM []byte) (StoreCert, error)
	CreateProfile(ctx context.Context, bundleID, certID, name string) (StoreProfile, error)
	LatestVersion(ctx context.Context, appID string) (AppStoreVersionInfo, error)
	SubmitForReview(ctx context.Context, appID, version string) error
	ReleaseVersion(ctx context.Context, appID string) error
	ListApps(ctx context.Context) ([]StoreAppRef, error)
	Tracks(ctx context.Context, appID string) (domain.StoreTracks, error)
	PromoteChannel(ctx context.Context, appID, from, to string) error
}

type PlayTrackInfo struct {
	HasRelease   bool
	VersionName  string
	Status       string // completed | inProgress | halted | draft
	UserFraction float64
}

type GooglePlayClient interface {
	ValidateAuth(ctx context.Context) error // token exchange only
	AppExists(ctx context.Context, packageName string) (bool, error)
	TrackInfo(ctx context.Context, packageName, track string) (PlayTrackInfo, error)
	PromoteTrack(ctx context.Context, packageName, fromTrack, toTrack string, userFraction float64) error
	SetRolloutFraction(ctx context.Context, packageName, track string, userFraction float64) error
	HaltRollout(ctx context.Context, packageName, track string) error
	ResumeRollout(ctx context.Context, packageName, track string) error
	ListApps(ctx context.Context) ([]StoreAppRef, error)
	Tracks(ctx context.Context, packageName string) (domain.StoreTracks, error)
}
