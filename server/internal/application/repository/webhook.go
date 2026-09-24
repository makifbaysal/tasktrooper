package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const defaultPushReindexInterval = 2 * time.Minute

const pushDeliveryTTL = 15 * time.Minute

type pushRepoState struct {
	running   bool
	pending   bool
	lastStart time.Time
	timer     *time.Timer
}

func (s *Service) SetPublicBaseURL(u string) {
	s.publicBaseURL = strings.TrimSuffix(strings.TrimSpace(u), "/")
}

func (s *Service) WebhooksReachable() bool {
	return webhooksReachable(s.publicBaseURL)
}

func webhooksReachable(base string) bool {
	if base == "" {
		return false
	}
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast()
	}
	return true
}

func (s *Service) webhookTargetURL() string {
	return s.publicBaseURL + "/v1/github/webhook"
}

func (s *Service) pushInterval() time.Duration {
	if s.pushReindexInterval > 0 {
		return s.pushReindexInterval
	}
	return defaultPushReindexInterval
}

func (s *Service) SetupWebhook(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.Repository{}, err
	}
	repo = s.syncRemoteURL(ctx, repo, "")
	owner, name, ok := githubapi.ParseOwnerRepo(repo.RemoteURL)
	if !ok {
		return domain.Repository{}, fmt.Errorf("repository has no GitHub remote to attach a webhook to")
	}
	if s.githubToken == nil {
		return domain.Repository{}, fmt.Errorf("GitHub is not connected — connect it from Settings")
	}
	token, err := s.githubToken(ctx)
	if err != nil {
		return domain.Repository{}, err
	}
	if token == "" {
		return domain.Repository{}, fmt.Errorf("GitHub is not connected — connect it from Settings")
	}
	if s.publicBaseURL == "" {
		return domain.Repository{}, fmt.Errorf("public base URL is not configured (server.public_base_url), GitHub cannot reach this instance")
	}
	if !webhooksReachable(s.publicBaseURL) {
		return domain.Repository{}, fmt.Errorf("GitHub cannot deliver webhooks to %s; pushes are picked up by polling instead", s.publicBaseURL)
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return domain.Repository{}, fmt.Errorf("generate webhook secret: %w", err)
	}
	hookID, err := githubapi.EnsureRepoWebhookAt(ctx, s.githubAPIBase, token, owner, name, s.webhookTargetURL(), secret)
	if err != nil {
		return domain.Repository{}, fmt.Errorf("github webhook setup: %w", err)
	}
	if err := s.repos.SetWebhook(ctx, repositoryID, secret, hookID); err != nil {

		return domain.Repository{}, fmt.Errorf("webhook created on GitHub but its secret could not be stored, run setup again: %w", err)
	}
	return s.repos.Get(ctx, repositoryID)
}

func generateWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Service) setupWebhookAsync(ctx context.Context, repositoryID uuid.UUID) {
	if s.githubToken == nil || !webhooksReachable(s.publicBaseURL) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := s.SetupWebhook(ctx, repositoryID); err != nil {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).
				Msg("github push webhook setup failed; retry from repository settings")
		}
	}()
}

func (s *Service) ReconcileWebhooks(ctx context.Context) {
	if s.githubToken == nil || !webhooksReachable(s.publicBaseURL) {
		return
	}
	func() {
		token, err := s.githubToken(ctx)
		if err != nil || token == "" {

			return
		}
		repos, err := s.repos.List(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("webhook reconcile: could not list repositories")
			return
		}
		target := s.webhookTargetURL()
		for _, repo := range repos {
			owner, name, ok := githubapi.ParseOwnerRepo(repo.RemoteURL)
			if !ok {
				continue
			}
			if !repo.WebhookInstalled {
				if _, err := s.SetupWebhook(ctx, repo.ID); err != nil {
					log.Warn().Err(err).Str("repository_id", repo.ID.String()).Str("name", repo.Name).
						Msg("webhook backfill failed; retry from repository settings")
					continue
				}
				log.Info().Str("repository_id", repo.ID.String()).Str("name", repo.Name).
					Msg("webhook backfill: repository webhook installed")
				continue
			}
			_, changed, err := githubapi.ReconcileRepoWebhookEvents(ctx, token, owner, name, target)
			if err != nil {
				log.Warn().Err(err).Str("repository_id", repo.ID.String()).Str("name", repo.Name).
					Msg("webhook reconcile: could not converge the hook's event list")
				continue
			}
			if changed {
				log.Info().Str("repository_id", repo.ID.String()).Str("name", repo.Name).
					Strs("events", githubapi.WebhookEvents).
					Msg("webhook reconcile: existing hook widened to the events the board needs")
			}
		}
	}()
}

func (s *Service) ResolvePushTarget(ctx context.Context, fullName string) (domain.Repository, string, bool) {
	fullName = strings.TrimSuffix(strings.TrimSpace(fullName), ".git")
	if fullName == "" {
		return domain.Repository{}, "", false
	}
	repos, err := s.repos.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("webhook: could not list repositories")
		return domain.Repository{}, "", false
	}
	for _, repo := range repos {
		owner, name, ok := githubapi.ParseOwnerRepo(repo.RemoteURL)
		if !ok {
			continue
		}
		if strings.EqualFold(owner+"/"+name, fullName) {
			secret, err := s.repos.WebhookSecret(ctx, repo.ID)
			if err != nil {

				log.Error().Err(err).Str("repository_id", repo.ID.String()).Msg("webhook: could not read secret")
				return repo, "", true
			}
			return repo, secret, true
		}
	}
	return domain.Repository{}, "", false
}

func (s *Service) HandleGitHubPush(ctx context.Context, repositoryID uuid.UUID, deliveryID, ref, defaultBranch, afterSHA string) (bool, string) {

	if defaultBranch == "" || ref != "refs/heads/"+defaultBranch {
		return false, "ref is not the default branch"
	}
	if s.seenDelivery(ctx, deliveryID, time.Now()) {
		return false, "duplicate delivery"
	}

	if s.indexer != nil && afterSHA != "" {
		if idx, err := s.indexer.GetProjectStatus(ctx, repositoryID); err == nil &&
			idx.Status == domain.IndexStatusCompleted && idx.CommitSHA == afterSHA {
			return false, "index already at pushed commit"
		}
	}
	return true, s.schedulePushReindex(repositoryID)
}

func (s *Service) HandleGitHubWorkflowEvent(ctx context.Context, repositoryID uuid.UUID, deliveryID, action, status, headSHA string) (bool, string) {
	if strings.TrimSpace(headSHA) == "" {
		return false, "no head sha in payload"
	}

	if action != "completed" && status != "completed" {
		return false, "run is not completed yet"
	}
	if s.seenDelivery(ctx, deliveryID, time.Now()) {
		return false, "duplicate delivery"
	}
	if s.pipelines == nil {
		return false, "pipeline runner is not configured"
	}
	n, err := s.pipelines.ResolveByHeadSHA(ctx, repositoryID, headSHA)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("head_sha", headSHA).
			Msg("webhook: resolving pipelines for a finished workflow run failed")
		return false, "resolving pipelines failed: " + err.Error()
	}
	if n == 0 {

		return false, "no pipeline is waiting on this commit"
	}
	return true, fmt.Sprintf("resolved %d pipeline(s)", n)
}

type DeliveryLedger interface {
	MarkSeen(ctx context.Context, deliveryID string, retain time.Duration) (bool, error)
}

func (s *Service) SetDeliveryLedger(l DeliveryLedger) {
	s.deliveries = l
}

func (s *Service) seenDelivery(ctx context.Context, deliveryID string, now time.Time) bool {
	if deliveryID == "" {
		return false
	}
	if s.deliveries != nil {
		first, err := s.deliveries.MarkSeen(ctx, deliveryID, pushDeliveryTTL)
		if err == nil {
			return !first
		}
		log.Warn().Err(err).Str("delivery_id", deliveryID).
			Msg("webhook: delivery ledger unreachable, falling back to this process's own memory")
	}
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	if s.pushDeliveries == nil {
		s.pushDeliveries = make(map[string]time.Time)
	}
	for id, at := range s.pushDeliveries {
		if now.Sub(at) > pushDeliveryTTL {
			delete(s.pushDeliveries, id)
		}
	}
	if _, ok := s.pushDeliveries[deliveryID]; ok {
		return true
	}
	s.pushDeliveries[deliveryID] = now
	return false
}

func (s *Service) pushStateLocked(repositoryID uuid.UUID) *pushRepoState {
	if s.pushRepos == nil {
		s.pushRepos = make(map[uuid.UUID]*pushRepoState)
	}
	st, ok := s.pushRepos[repositoryID]
	if !ok {
		st = &pushRepoState{}
		s.pushRepos[repositoryID] = st
	}
	return st
}

func (s *Service) schedulePushReindex(repositoryID uuid.UUID) string {
	s.pushMu.Lock()
	st := s.pushStateLocked(repositoryID)
	if st.running {
		st.pending = true
		s.pushMu.Unlock()
		return "queued behind the running reindex"
	}
	if st.timer != nil {
		s.pushMu.Unlock()
		return "reindex already scheduled"
	}
	if wait := s.pushInterval() - time.Since(st.lastStart); !st.lastStart.IsZero() && wait > 0 {
		st.timer = time.AfterFunc(wait, func() { s.firePushReindex(repositoryID) })
		s.pushMu.Unlock()
		return fmt.Sprintf("debounced, reindex in %s", wait.Round(time.Second))
	}
	st.running = true
	st.lastStart = time.Now()
	s.pushMu.Unlock()
	go s.launchPushReindex(repositoryID)
	return "reindex started"
}

func (s *Service) launchPushReindex(repositoryID uuid.UUID) {
	if s.pushRunFn != nil {
		s.pushRunFn(repositoryID)
		return
	}
	s.startPushReindex(repositoryID)
}

func (s *Service) firePushReindex(repositoryID uuid.UUID) {
	s.pushMu.Lock()
	st := s.pushStateLocked(repositoryID)
	st.timer = nil
	if st.running {
		st.pending = true
		s.pushMu.Unlock()
		return
	}
	st.running = true
	st.lastStart = time.Now()
	s.pushMu.Unlock()
	s.launchPushReindex(repositoryID)
}

func (s *Service) startPushReindex(repositoryID uuid.UUID) {

	pushCtx := context.Background()

	ctx, cancel := context.WithTimeout(pushCtx, 30*time.Second)
	repo, err := s.repos.Get(ctx, repositoryID)
	cancel()
	if err != nil {
		log.Error().Err(err).Str("repository_id", repositoryID.String()).Msg("webhook reindex: repository lookup failed")
		s.pushReindexDone(repositoryID)
		return
	}
	s.pullAndRestartIndexNotify(pushCtx, repo.ID, repo.RootPath, func() {
		s.pushReindexDone(repositoryID)
		if s.modelRefresher != nil {
			pctx, pcancel := context.WithTimeout(pushCtx, 30*time.Second)
			defer pcancel()
			s.modelRefresher.RefreshAfterPush(pctx, repositoryID, "push")
		}
	})
}

func (s *Service) pushReindexDone(repositoryID uuid.UUID) {
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	st := s.pushStateLocked(repositoryID)
	st.running = false
	if !st.pending {
		return
	}
	st.pending = false
	if st.timer != nil {

		return
	}
	wait := s.pushInterval() - time.Since(st.lastStart)

	if minWait := min(time.Second, s.pushInterval()); wait < minWait {
		wait = minWait
	}
	st.timer = time.AfterFunc(wait, func() { s.firePushReindex(repositoryID) })
}
