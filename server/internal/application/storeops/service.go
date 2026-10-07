package storeops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
}

type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	List(ctx context.Context) ([]domain.Repository, error)
}

type Commenter interface {
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

type Deps struct {
	Credentials port.StoreCredentialStore
	Apps        port.MobileStoreAppStore
	Signing     port.SigningAssetStore
	Cipher      *secrets.Cipher

	NewASC  func(domain.StoreCredential) (port.AppStoreClient, error)
	NewPlay func(domain.StoreCredential) (port.GooglePlayClient, error)

	PushSecret func(ctx context.Context, repositoryID uuid.UUID, name, value string) error
	Repos      RepositoryResolver
	Tasks      TaskCreator
	Comments   Commenter
}

type Service struct {
	credentials port.StoreCredentialStore
	apps        port.MobileStoreAppStore
	signing     port.SigningAssetStore
	cipher      *secrets.Cipher
	newASC      func(domain.StoreCredential) (port.AppStoreClient, error)
	newPlay     func(domain.StoreCredential) (port.GooglePlayClient, error)
	pushSecret  func(ctx context.Context, repositoryID uuid.UUID, name, value string) error
	repos       RepositoryResolver
	tasks       TaskCreator
	comments    Commenter

	audit port.OpsAuditStore

	actionsProbe ActionsProbe
	localProbe   LocalRunnerProbe
	startRelease ReleaseStarter
	parker       ReleaseParker

	mu sync.Mutex

	pendingPush map[renewTarget]bool

	tb *testBuilds
}

func NewService(d Deps) *Service {
	return &Service{
		credentials: d.Credentials,
		apps:        d.Apps,
		signing:     d.Signing,
		cipher:      d.Cipher,
		newASC:      d.NewASC,
		newPlay:     d.NewPlay,
		pushSecret:  d.PushSecret,
		repos:       d.Repos,
		tasks:       d.Tasks,
		comments:    d.Comments,
	}
}

type CredentialView struct {
	Provider   string    `json:"provider"`
	Configured bool      `json:"configured"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var knownCredentialProviders = []string{domain.StoreCredentialASC, domain.StoreCredentialGooglePlay}

var ErrInvalidCredential = errors.New("storeops: invalid credential")

var ErrInvalidPlatform = errors.New("storeops: unsupported mobile store platform")

var ErrIdentifierLocked = errors.New("storeops: a live store app's identifier cannot be changed")

func validProvider(provider string) bool {
	return provider == domain.StoreCredentialASC || provider == domain.StoreCredentialGooglePlay
}

func validPlatform(platform string) bool {
	return platform == domain.MobileStorePlatformIOS || platform == domain.MobileStorePlatformAndroid
}

func (s *Service) SaveCredential(ctx context.Context, provider string, data map[string]string) error {
	if !validProvider(provider) {
		return fmt.Errorf("storeops: unknown credential provider %q: %w", provider, ErrInvalidCredential)
	}
	if s.cipher == nil {
		return errors.New("storeops: secrets cipher not configured")
	}
	cred := domain.StoreCredential{Provider: provider, Data: data}

	switch provider {
	case domain.StoreCredentialASC:
		if s.newASC == nil {
			return errors.New("storeops: App Store Connect client factory not configured")
		}
		client, err := s.newASC(cred)
		if err != nil {
			return fmt.Errorf("storeops: building App Store Connect client: %w: %w", err, ErrInvalidCredential)
		}
		if err := client.ValidateAuth(ctx); err != nil {
			return fmt.Errorf("storeops: App Store Connect credential failed validation: %w: %w", err, ErrInvalidCredential)
		}
	case domain.StoreCredentialGooglePlay:
		if s.newPlay == nil {
			return errors.New("storeops: Google Play client factory not configured")
		}
		client, err := s.newPlay(cred)
		if err != nil {
			return fmt.Errorf("storeops: building Google Play client: %w: %w", err, ErrInvalidCredential)
		}
		if err := client.ValidateAuth(ctx); err != nil {
			return fmt.Errorf("storeops: Google Play credential failed validation: %w: %w", err, ErrInvalidCredential)
		}
	}

	plaintext, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("storeops: encoding credential: %w", err)
	}
	encrypted, err := s.cipher.Encrypt(string(plaintext))
	if err != nil {
		return fmt.Errorf("storeops: encrypting credential: %w", err)
	}
	if err := s.credentials.Set(ctx, provider, encrypted); err != nil {
		return fmt.Errorf("storeops: persisting credential: %w", err)
	}
	return nil
}

func (s *Service) Credentials(ctx context.Context) ([]CredentialView, error) {
	listed, err := s.credentials.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing credentials: %w", err)
	}
	views := make([]CredentialView, 0, len(knownCredentialProviders))
	for _, provider := range knownCredentialProviders {
		updatedAt, configured := listed[provider]
		views = append(views, CredentialView{Provider: provider, Configured: configured, UpdatedAt: updatedAt})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Provider < views[j].Provider })
	return views, nil
}

func (s *Service) DeleteCredential(ctx context.Context, provider string) error {
	if !validProvider(provider) {
		return fmt.Errorf("storeops: unknown credential provider %q: %w", provider, ErrInvalidCredential)
	}
	if err := s.credentials.Delete(ctx, provider); err != nil {
		return fmt.Errorf("storeops: deleting %s credential: %w", provider, err)
	}
	return nil
}

var ErrProviderNotConnected = errors.New("storeops: this provider is not connected yet")

func notConnected(provider string, err error) error {
	if errors.Is(err, port.ErrNotFound) {
		return fmt.Errorf("storeops: %s: %w", provider, ErrProviderNotConnected)
	}
	return err
}

func (s *Service) credential(ctx context.Context, provider string) (domain.StoreCredential, error) {
	if s.cipher == nil {
		return domain.StoreCredential{}, errors.New("storeops: secrets cipher not configured")
	}
	encrypted, updatedAt, err := s.credentials.Get(ctx, provider)
	if err != nil {
		return domain.StoreCredential{}, fmt.Errorf("storeops: loading %s credential: %w", provider, err)
	}
	plaintext, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return domain.StoreCredential{}, fmt.Errorf("storeops: decrypting %s credential: %w", provider, err)
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(plaintext), &data); err != nil {
		return domain.StoreCredential{}, fmt.Errorf("storeops: decoding %s credential: %w", provider, err)
	}
	return domain.StoreCredential{Provider: provider, Data: data, UpdatedAt: updatedAt}, nil
}

func (s *Service) asc(ctx context.Context) (port.AppStoreClient, error) {
	cred, err := s.credential(ctx, domain.StoreCredentialASC)
	if err != nil {
		return nil, err
	}
	if s.newASC == nil {
		return nil, errors.New("storeops: App Store Connect client factory not configured")
	}
	return s.newASC(cred)
}

func (s *Service) play(ctx context.Context) (port.GooglePlayClient, error) {
	cred, err := s.credential(ctx, domain.StoreCredentialGooglePlay)
	if err != nil {
		return nil, err
	}
	if s.newPlay == nil {
		return nil, errors.New("storeops: Google Play client factory not configured")
	}
	return s.newPlay(cred)
}

func (s *Service) AppsByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error) {
	apps, err := s.apps.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing store apps: %w", err)
	}
	return apps, nil
}

var ErrAppNotInStore = errors.New("storeops: the store console has no app with this identifier")

func (s *Service) ListStoreApps(ctx context.Context, provider string) ([]port.StoreAppRef, error) {
	if !validProvider(provider) {
		return nil, fmt.Errorf("storeops: unknown credential provider %q: %w", provider, ErrInvalidCredential)
	}
	var apps []port.StoreAppRef
	switch provider {
	case domain.StoreCredentialASC:
		client, err := s.ascClient(ctx)
		if err != nil {
			return nil, notConnected(provider, err)
		}
		apps, err = client.ListApps(ctx)
		if err != nil {
			return nil, fmt.Errorf("storeops: listing App Store Connect apps: %w", err)
		}
	default:
		client, err := s.playClient(ctx)
		if err != nil {
			return nil, notConnected(provider, err)
		}
		apps, err = client.ListApps(ctx)
		if err != nil {
			return nil, fmt.Errorf("storeops: listing Google Play apps: %w", err)
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Identifier < apps[j].Identifier })
	return apps, nil
}

func (s *Service) LinkStoreApp(ctx context.Context, repositoryID uuid.UUID, platform string, ref port.StoreAppRef) (domain.MobileStoreApp, error) {
	if !validPlatform(platform) {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: linking a store app: unsupported platform %q: %w", platform, ErrInvalidPlatform)
	}
	identifier := strings.TrimSpace(ref.Identifier)
	if identifier == "" {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: linking a store app needs an identifier: %w", ErrAppNotInStore)
	}

	app, err := s.apps.Get(ctx, repositoryID, platform)
	switch {
	case err == nil:
	case errors.Is(err, port.ErrNotFound):
		app = domain.MobileStoreApp{
			RepositoryID: repositoryID,
			Platform:     platform,
			State:        domain.MobileStoreStateUnregistered,
		}
	default:
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: loading store app: %w", err)
	}
	if err := checkIdentifierChange(app, platform, identifier); err != nil {
		return domain.MobileStoreApp{}, err
	}

	storeAppID := strings.TrimSpace(ref.StoreAppID)
	switch platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.ascClient(ctx)
		if err != nil {
			return domain.MobileStoreApp{}, err
		}
		confirmedID, found, err := client.AppByBundleID(ctx, identifier)
		if err != nil {
			return domain.MobileStoreApp{}, fmt.Errorf("storeops: looking up %s in App Store Connect: %w", identifier, err)
		}
		if !found {
			return domain.MobileStoreApp{}, fmt.Errorf("storeops: App Store Connect has no app for %s: %w", identifier, ErrAppNotInStore)
		}

		storeAppID = confirmedID
	case domain.MobileStorePlatformAndroid:
		client, err := s.playClient(ctx)
		if err != nil {
			return domain.MobileStoreApp{}, err
		}
		exists, err := client.AppExists(ctx, identifier)
		if err != nil {
			return domain.MobileStoreApp{}, fmt.Errorf("storeops: looking up %s in Google Play: %w", identifier, err)
		}
		if !exists {
			return domain.MobileStoreApp{}, fmt.Errorf("storeops: Google Play has no app for %s: %w", identifier, ErrAppNotInStore)
		}
	}

	app.Identifier = identifier
	app.StoreAppID = storeAppID
	if name := strings.TrimSpace(ref.Name); name != "" {

		app.AppName = name
	}

	// A failed read leaves the row where it was; the monitor reads the console
	// again on its next sweep, so the link itself is not refused over it.
	if presence, err := s.storePresence(ctx, app); err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("platform", platform).
			Msg("storeops: reading the linked app's store state failed")
	} else {
		app, _ = adoptStorePresence(app, presence, time.Now())
	}

	stored, err := s.apps.Upsert(ctx, app)
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("storeops: persisting the store app link: %w", err)
	}
	return stored, nil
}

func (s *Service) Tracks(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.StoreTracks, error) {
	if !validPlatform(platform) {
		return domain.StoreTracks{}, fmt.Errorf("storeops: reading store tracks: unsupported platform %q: %w", platform, ErrInvalidPlatform)
	}
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		return domain.StoreTracks{}, fmt.Errorf("storeops: loading store app: %w", err)
	}
	tracks, err := s.storeTracks(ctx, app)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	if _, err := s.apps.SetTracks(ctx, repositoryID, platform, tracks, time.Now()); err != nil {
		return domain.StoreTracks{}, fmt.Errorf("storeops: caching store tracks: %w", err)
	}
	return tracks, nil
}

func (s *Service) storeTracks(ctx context.Context, app domain.MobileStoreApp) (domain.StoreTracks, error) {
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.ascClient(ctx)
		if err != nil {
			return domain.StoreTracks{}, err
		}
		s.skipTestBuilds(ctx, client, app)
		tracks, err := client.Tracks(ctx, app.StoreAppID)
		if err != nil {
			return domain.StoreTracks{}, fmt.Errorf("storeops: reading TestFlight and App Store channels for %s: %w", app.Identifier, err)
		}
		return tracks, nil
	case domain.MobileStorePlatformAndroid:
		client, err := s.playClient(ctx)
		if err != nil {
			return domain.StoreTracks{}, err
		}
		tracks, err := client.Tracks(ctx, app.Identifier)
		if err != nil {
			return domain.StoreTracks{}, fmt.Errorf("storeops: reading Play tracks for %s: %w", app.Identifier, err)
		}
		return tracks, nil
	}
	return domain.StoreTracks{}, fmt.Errorf("storeops: reading store tracks: unsupported platform %q: %w", app.Platform, ErrInvalidPlatform)
}
