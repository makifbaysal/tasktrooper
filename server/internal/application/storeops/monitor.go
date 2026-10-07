package storeops

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultMonitorInterval = time.Minute

	signingRenewalWindow = 30 * 24 * time.Hour
)

type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

type Monitor struct {
	svc      *Service
	apps     port.MobileStoreAppStore
	ingester IncidentIngester

	mu sync.Mutex

	notified map[string]bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewMonitor(svc *Service, apps port.MobileStoreAppStore, ingester IncidentIngester) *Monitor {
	return &Monitor{
		svc:      svc,
		apps:     apps,
		ingester: ingester,
		notified: map[string]bool{},
	}
}

func (m *Monitor) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultMonitorInterval
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		m.Sweep(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Sweep(ctx)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("store monitor started")
}

func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

func (m *Monitor) Sweep(ctx context.Context) {
	apps, err := m.apps.ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("store monitor: list mobile store apps failed")
	} else {
		for _, app := range apps {
			select {
			case <-ctx.Done():
				return
			default:
			}
			m.sweepApp(ctx, app)
		}
	}

	if err := m.svc.RenewExpiringSigning(ctx, time.Now().Add(signingRenewalWindow)); err != nil {
		log.Warn().Err(err).Msg("store monitor: renew expiring signing assets failed")
	}
}

func (m *Monitor) sweepApp(ctx context.Context, app domain.MobileStoreApp) {
	switch app.State {
	case domain.MobileStoreStateUnregistered:
		// A row linked from the console but never read back (a link made
		// before links read the console, or one whose read failed) is
		// otherwise stuck unregistered forever, with every store action shut.
		if app.Identifier != "" && (app.Platform != domain.MobileStorePlatformIOS || app.StoreAppID != "") {
			m.reconcile(ctx, app)
		}
	case domain.MobileStoreStateOnboarding:
		m.sweepOnboarding(ctx, app)
	case domain.MobileStoreStateTestReady:
		m.sweepTestReady(ctx, m.syncTracks(ctx, app))
	case domain.MobileStoreStateLive:
		m.sweepLive(ctx, m.syncTracks(ctx, app))
	}
}

func (m *Monitor) syncTracks(ctx context.Context, app domain.MobileStoreApp) domain.MobileStoreApp {
	tracks, err := m.svc.storeTracks(ctx, app)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).Str("platform", app.Platform).
			Msg("store monitor: reading store channels failed")
		return app
	}
	stored, err := m.apps.SetTracks(ctx, app.RepositoryID, app.Platform, tracks, time.Now())
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: caching store channels failed")
		return app
	}
	return stored
}

func (m *Monitor) sweepOnboarding(ctx context.Context, app domain.MobileStoreApp) {
	verified, err := m.svc.VerifyOnboarding(ctx, app.RepositoryID, app.Platform)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).Str("platform", app.Platform).
			Msg("store monitor: verify onboarding failed")
		if verified.RepositoryID == uuid.Nil {
			return
		}
	}
	// onboarding -> live is not a lifecycle step, so an app that went live by
	// hand while a checklist item stayed open would otherwise wait forever.
	if verified.State == domain.MobileStoreStateOnboarding {
		m.reconcile(ctx, verified)
	}
}

func (m *Monitor) sweepTestReady(ctx context.Context, app domain.MobileStoreApp) {
	m.reconcile(ctx, app)
}

func (m *Monitor) reconcile(ctx context.Context, app domain.MobileStoreApp) {
	presence, err := m.svc.storePresence(ctx, app)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).Str("platform", app.Platform).
			Msg("store monitor: reading the store console failed")
		return
	}
	next, changed := adoptStorePresence(app, presence, time.Now())
	if !changed {
		return
	}
	stored, err := m.apps.Upsert(ctx, next)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: persisting the observed store state failed")
		return
	}
	if stored.State == domain.MobileStoreStateLive {
		m.commentOnboarding(ctx, stored, fmt.Sprintf("%s is now live in the %s store.", stored.Identifier, stored.Platform))
	}
}

func (s *Service) MarkSubmitted(ctx context.Context, repositoryID uuid.UUID, platform, version string) error {
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("storeops: loading store app: %w", err)
	}
	app.ReviewState = domain.ReviewStateWaiting
	if version != "" {
		app.LastSubmittedVersion = version
	}
	if _, err := s.apps.Upsert(ctx, app); err != nil {
		return fmt.Errorf("storeops: recording store submit: %w", err)
	}
	return nil
}

func (m *Monitor) sweepLive(ctx context.Context, app domain.MobileStoreApp) {
	if app.ReviewState != domain.ReviewStateWaiting && app.ReviewState != domain.ReviewStateInReview {
		return
	}
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		m.pollIOSReview(ctx, app)
	case domain.MobileStorePlatformAndroid:
		m.pollAndroidRollout(ctx, app)
	}
}

func ascReviewState(state string) string {
	switch state {
	case "WAITING_FOR_REVIEW":
		return domain.ReviewStateWaiting
	case "IN_REVIEW":
		return domain.ReviewStateInReview
	case "READY_FOR_SALE", "PENDING_DEVELOPER_RELEASE":
		return domain.ReviewStateApproved
	case "REJECTED", "METADATA_REJECTED", "DEVELOPER_REJECTED":
		return domain.ReviewStateRejected
	default:
		return ""
	}
}

func (m *Monitor) pollIOSReview(ctx context.Context, app domain.MobileStoreApp) {
	client, err := m.svc.asc(ctx)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: build app store connect client failed")
		return
	}
	info, err := client.LatestVersion(ctx, app.StoreAppID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: check app store review status failed")
		return
	}
	newState := ascReviewState(info.State)
	if newState == "" || newState == app.ReviewState {
		return
	}

	if newState == domain.ReviewStateRejected {
		if err := m.ingestIncident(ctx, domain.IncidentInput{
			RepositoryID: app.RepositoryID,
			Env:          domain.DeployEnvProd,
			Source:       domain.IncidentSourceProbe,
			Severity:     domain.IncidentSeverityHigh,
			Fingerprint:  domain.IncidentFingerprint("store_review", app.Platform, info.Version),
			Title:        fmt.Sprintf("Store review rejected: %s %s", app.Identifier, info.Version),
			Detail:       fmt.Sprintf("App Store Connect reported %s for %s.", info.State, app.Identifier),
			Payload: map[string]any{
				"platform":   app.Platform,
				"identifier": app.Identifier,
				"version":    info.Version,
				"asc_state":  info.State,
			},
		}); err != nil {

			return
		}
	}

	app.ReviewState = newState
	if newState == domain.ReviewStateApproved {

		app.LastReleasedVersion = info.Version
		if app.LastReleasedVersion == "" {
			app.LastReleasedVersion = app.LastSubmittedVersion
		}
	}

	stored, err := m.apps.Upsert(ctx, app)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: persisting review state failed")
		return
	}

	if newState == domain.ReviewStateApproved {
		m.commentOnboarding(ctx, stored, fmt.Sprintf("Store review approved for %s %s.", stored.Identifier, stored.LastReleasedVersion))
	}
}

func (m *Monitor) pollAndroidRollout(ctx context.Context, app domain.MobileStoreApp) {
	client, err := m.svc.play(ctx)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: build google play client failed")
		return
	}
	track, err := client.TrackInfo(ctx, app.Identifier, "production")
	if err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: check play production track failed")
		return
	}
	if track.Status != "halted" {
		return
	}

	fingerprint := domain.IncidentFingerprint("store_rollout", app.Identifier, track.VersionName)

	m.mu.Lock()
	already := m.notified[fingerprint]
	if !already {
		m.notified[fingerprint] = true
	}
	m.mu.Unlock()
	if already {
		return
	}

	if err := m.ingestIncident(ctx, domain.IncidentInput{
		RepositoryID: app.RepositoryID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceProbe,
		Severity:     domain.IncidentSeverityHigh,
		Fingerprint:  fingerprint,
		Title:        fmt.Sprintf("Store rollout halted: %s %s", app.Identifier, track.VersionName),
		Detail:       fmt.Sprintf("Google Play production track reported status %q for %s.", track.Status, app.Identifier),
		Payload: map[string]any{
			"platform":   app.Platform,
			"identifier": app.Identifier,
			"version":    track.VersionName,
			"status":     track.Status,
		},
	}); err != nil {

		m.mu.Lock()
		delete(m.notified, fingerprint)
		m.mu.Unlock()
		return
	}
}

func (m *Monitor) commentOnboarding(ctx context.Context, app domain.MobileStoreApp, content string) {
	if app.OnboardingTaskID == nil || m.svc.comments == nil {
		return
	}
	if _, err := m.svc.comments.AddComment(ctx, app.RepositoryID, *app.OnboardingTaskID, domain.CreateTaskCommentRequest{
		Content:    content,
		AuthorType: "system",
	}); err != nil {
		log.Warn().Err(err).Str("repository_id", app.RepositoryID.String()).
			Msg("store monitor: posting comment failed")
	}
}

func (m *Monitor) ingestIncident(ctx context.Context, in domain.IncidentInput) error {
	if m.ingester == nil {
		return nil
	}
	if _, err := m.ingester.Ingest(ctx, in); err != nil {
		log.Warn().Err(err).Str("fingerprint", in.Fingerprint).Msg("store monitor: incident ingest failed")
		return err
	}
	return nil
}
