package storeops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	androidTrackInternal   = "internal"
	androidTrackProduction = "production"
)

var ErrConfirmMismatch = errors.New("storeops: confirmation phrase does not match the repository name")

var ErrAppNotReady = errors.New("storeops: mobile store app is not ready for this action")

var ErrInvalidRolloutFraction = errors.New("storeops: rollout fraction must be between 0 and 1")

var ErrStoreCredentialUnavailable = errors.New("storeops: store credential unavailable for this action")

func (s *Service) SetAuditor(store port.OpsAuditStore) { s.audit = store }

func (s *Service) recordAudit(ctx context.Context, repositoryID uuid.UUID, action, target, actor string, detail map[string]string, actionErr error) {
	if s.audit == nil {
		return
	}
	entry := domain.OpsAuditEntry{
		RepositoryID: &repositoryID,
		Action:       action,
		Target:       target,
		Actor:        actor,
		Detail:       detail,
		Outcome:      domain.OpsOutcomeOK,
	}
	if actionErr != nil {
		entry.Outcome = domain.OpsOutcomeError
		entry.Error = actionErr.Error()
	}
	if err := s.audit.Log(ctx, entry); err != nil {
		log.Error().Err(err).Str("action", action).Str("target", target).
			Msg("storeops: writing ops audit entry failed")
	}
}

func checkConfirm(confirm, repoName string) error {
	if strings.TrimSpace(confirm) != repoName {
		return ErrConfirmMismatch
	}
	return nil
}

func (s *Service) loadRepoAndApp(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.Repository, domain.MobileStoreApp, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.Repository{}, domain.MobileStoreApp{}, fmt.Errorf("storeops: loading repository: %w", err)
	}
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		return domain.Repository{}, domain.MobileStoreApp{}, fmt.Errorf("storeops: loading store app: %w", err)
	}
	return repo, app, nil
}

func (s *Service) ascClient(ctx context.Context) (port.AppStoreClient, error) {
	client, err := s.asc(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreCredentialUnavailable, err)
	}
	return client, nil
}

func (s *Service) playClient(ctx context.Context) (port.GooglePlayClient, error) {
	client, err := s.play(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreCredentialUnavailable, err)
	}
	return client, nil
}

func (s *Service) SubmitIOS(ctx context.Context, repositoryID uuid.UUID, confirm, actor string) error {
	const platform = domain.MobileStorePlatformIOS
	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: iOS app must be live before it can be submitted for review: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, nil, notReady)
		return notReady
	}
	if err := checkConfirm(confirm, repo.Name); err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, nil, err)
		return err
	}
	client, err := s.ascClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, nil, err)
		return err
	}
	version, err := client.LatestVersion(ctx, app.StoreAppID)
	if err != nil {
		wrapped := fmt.Errorf("storeops: loading latest App Store version: %w", err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, nil, wrapped)
		return wrapped
	}
	detail := map[string]string{"version": version.Version}
	if err := client.SubmitForReview(ctx, app.StoreAppID, version.Version); err != nil {
		wrapped := fmt.Errorf("storeops: submitting %s for review: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, detail, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreSubmit, platform, actor, detail, nil)
	return nil
}

func (s *Service) ReleaseIOS(ctx context.Context, repositoryID uuid.UUID, confirm, actor string) error {
	const platform = domain.MobileStorePlatformIOS
	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: iOS app must be live before a pending version can be released: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, notReady)
		return notReady
	}
	if err := checkConfirm(confirm, repo.Name); err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, err)
		return err
	}
	client, err := s.ascClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, err)
		return err
	}
	if err := client.ReleaseVersion(ctx, app.StoreAppID); err != nil {
		wrapped := fmt.Errorf("storeops: releasing %s: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRelease, platform, actor, nil, nil)
	return nil
}

func (s *Service) PromoteAndroid(ctx context.Context, repositoryID uuid.UUID, toTrack string, userFraction float64, confirm, actor string) error {
	const platform = domain.MobileStorePlatformAndroid
	if !validPlayTrack(toTrack) {

		invalid := fmt.Errorf("storeops: %q is not a Google Play track: %w", toTrack, ErrInvalidChannel)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, map[string]string{"to_track": toTrack}, invalid)
		return invalid
	}
	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	ready := app.State == domain.MobileStoreStateLive ||
		(toTrack == androidTrackInternal && app.State == domain.MobileStoreStateTestReady)
	if !ready {
		notReady := fmt.Errorf("storeops: android app is not ready to promote to %s: %w", toTrack, ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, map[string]string{"to_track": toTrack}, notReady)
		return notReady
	}
	if toTrack == androidTrackProduction {
		if err := checkConfirm(confirm, repo.Name); err != nil {
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, map[string]string{"to_track": toTrack}, err)
			return err
		}
	}
	client, err := s.playClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, map[string]string{"to_track": toTrack}, err)
		return err
	}
	detail := map[string]string{"from_track": androidTrackInternal, "to_track": toTrack}
	if err := client.PromoteTrack(ctx, app.Identifier, androidTrackInternal, toTrack, userFraction); err != nil {
		wrapped := fmt.Errorf("storeops: promoting %s to %s: %w", app.Identifier, toTrack, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, nil)
	return nil
}

func (s *Service) SetAndroidRollout(ctx context.Context, repositoryID uuid.UUID, userFraction float64, actor string) error {
	if userFraction < 0 || userFraction > 1 {
		return fmt.Errorf("storeops: rollout fraction %v is outside [0,1]: %w", userFraction, ErrInvalidRolloutFraction)
	}
	const platform = domain.MobileStorePlatformAndroid
	_, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: android app must be live to change its rollout: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRollout, platform, actor, nil, notReady)
		return notReady
	}
	client, err := s.playClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRollout, platform, actor, nil, err)
		return err
	}
	detail := map[string]string{"user_fraction": fmt.Sprintf("%v", userFraction)}
	if err := client.SetRolloutFraction(ctx, app.Identifier, androidTrackProduction, userFraction); err != nil {
		wrapped := fmt.Errorf("storeops: setting %s rollout fraction: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRollout, platform, actor, detail, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreRollout, platform, actor, detail, nil)
	return nil
}

func (s *Service) HaltAndroid(ctx context.Context, repositoryID uuid.UUID, confirm, actor string) error {
	const platform = domain.MobileStorePlatformAndroid
	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: android app must be live to halt its rollout: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, notReady)
		return notReady
	}
	if err := checkConfirm(confirm, repo.Name); err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, err)
		return err
	}
	client, err := s.playClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, err)
		return err
	}
	if err := client.HaltRollout(ctx, app.Identifier, androidTrackProduction); err != nil {
		wrapped := fmt.Errorf("storeops: halting %s rollout: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreHalt, platform, actor, nil, nil)
	return nil
}

func (s *Service) ResumeAndroid(ctx context.Context, repositoryID uuid.UUID, actor string) error {
	const platform = domain.MobileStorePlatformAndroid
	_, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	if app.State != domain.MobileStoreStateLive {
		notReady := fmt.Errorf("storeops: android app must be live to resume its rollout: %w", ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreResume, platform, actor, nil, notReady)
		return notReady
	}
	client, err := s.playClient(ctx)
	if err != nil {
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreResume, platform, actor, nil, err)
		return err
	}
	if err := client.ResumeRollout(ctx, app.Identifier, androidTrackProduction); err != nil {
		wrapped := fmt.Errorf("storeops: resuming %s rollout: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStoreResume, platform, actor, nil, wrapped)
		return wrapped
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStoreResume, platform, actor, nil, nil)
	return nil
}

func (s *Service) AllApps(ctx context.Context) ([]AppView, error) {
	apps, err := s.apps.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing all store apps: %w", err)
	}
	repos, err := s.repos.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing repositories: %w", err)
	}
	names := make(map[uuid.UUID]string, len(repos))
	for _, r := range repos {
		names[r.ID] = r.Name
	}
	views := make([]AppView, 0, len(apps))
	for _, app := range apps {
		views = append(views, AppView{MobileStoreApp: app, RepositoryName: names[app.RepositoryID]})
	}
	return views, nil
}

type AppView struct {
	domain.MobileStoreApp
	RepositoryName string `json:"repository_name"`
}

const (
	androidTrackAlpha = "alpha"
	androidTrackBeta  = "beta"
)

func validPlayTrack(track string) bool {
	switch track {
	case androidTrackInternal, androidTrackAlpha, androidTrackBeta, androidTrackProduction:
		return true
	}
	return false
}

var ErrInvalidChannel = errors.New("storeops: promotion must move one channel forward")

func (s *Service) PromoteChannel(ctx context.Context, repositoryID uuid.UUID, platform, from, to, confirm, actor string) error {

	pair := map[string]string{"from": from, "to": to}
	if !validPlatform(platform) {
		err := fmt.Errorf("storeops: promoting: unsupported platform %q: %w", platform, ErrInvalidPlatform)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, pair, err)
		return err
	}
	if !domain.ValidStoreChannel(from) || !domain.ValidStoreChannel(to) {
		err := fmt.Errorf("storeops: promoting %q -> %q: unknown channel: %w", from, to, ErrInvalidChannel)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, pair, err)
		return err
	}
	if next, ok := domain.NextChannel(from); !ok || next != to {
		err := fmt.Errorf("storeops: %q does not promote to %q: %w", from, to, ErrInvalidChannel)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, pair, err)
		return err
	}

	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return err
	}
	detail := map[string]string{"from": from, "to": to}
	toProduction := to == domain.StoreChannelProduction

	ready := app.State == domain.MobileStoreStateLive ||
		(!toProduction && app.State == domain.MobileStoreStateTestReady)
	if !ready {
		notReady := fmt.Errorf("storeops: %s app is not ready to promote to %s: %w", platform, to, ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, notReady)
		return notReady
	}
	if toProduction {
		if err := checkConfirm(confirm, repo.Name); err != nil {
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, err)
			return err
		}
	}

	switch platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.ascClient(ctx)
		if err != nil {
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, err)
			return err
		}
		s.skipTestBuilds(ctx, client, app)
		if err := client.PromoteChannel(ctx, app.StoreAppID, from, to); err != nil {
			wrapped := fmt.Errorf("storeops: promoting %s from %s to %s: %w", app.Identifier, from, to, err)
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, wrapped)
			return wrapped
		}
	case domain.MobileStorePlatformAndroid:
		client, err := s.playClient(ctx)
		if err != nil {
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, err)
			return err
		}
		fromTrack := playTrack(from, app.Tracks)
		toTrack := playTrack(to, app.Tracks)
		detail["from_track"] = fromTrack
		detail["to_track"] = toTrack

		if err := client.PromoteTrack(ctx, app.Identifier, fromTrack, toTrack, 1); err != nil {
			wrapped := fmt.Errorf("storeops: promoting %s from %s to %s: %w", app.Identifier, fromTrack, toTrack, err)
			s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, wrapped)
			return wrapped
		}
	}
	s.recordAudit(ctx, repositoryID, domain.OpsActionStorePromote, platform, actor, detail, nil)
	return nil
}

func playTrack(channel string, tracks domain.StoreTracks) string {
	switch channel {
	case domain.StoreChannelInternal:
		return androidTrackInternal
	case domain.StoreChannelProduction:
		return androidTrackProduction
	}
	if strings.Contains(strings.ToLower(tracks.External.Audience), "closed") {
		return androidTrackAlpha
	}
	return androidTrackBeta
}
