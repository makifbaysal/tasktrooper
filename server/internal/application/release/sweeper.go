package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const DefaultSweepInterval = 30 * time.Second

const sweepBatch = 200

const (
	pendingDeployTimeout = 60 * time.Minute
	noSignalGrace        = 15 * time.Minute
	unknownDeployTimeout = 60 * time.Minute
	errorCheckInterval   = 2 * time.Minute
	rollbackTimeout      = 30 * time.Minute
	rollbackNoSignalWait = 15 * time.Minute
)

// Start runs SweepOnce on interval (default 30s) until ctx is cancelled.
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.store == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	go func() {
		s.SweepOnce(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.SweepOnce(ctx)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("release sweeper started")
}

// SweepOnce advances every release the sweeper owns (Status.Watched()) by
// one step. Every transition is an optimistic store.Update(r, expect): on
// ErrReleaseWrongStatus the release is skipped — someone else (another
// sweep tick, an agent's Finish/Rollback) already moved it.
func (s *Service) SweepOnce(ctx context.Context) {
	if s.store == nil {
		return
	}
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		Statuses: []domain.ReleaseStatus{domain.ReleaseDeploying, domain.ReleaseVerifying, domain.ReleaseRollingBack},
		Limit:    sweepBatch,
	})
	if err != nil {
		log.Warn().Err(err).Msg("release sweeper: listing watched releases failed")
		return
	}
	for _, r := range releases {
		select {
		case <-ctx.Done():
			return
		default:
		}
		switch r.Status {
		case domain.ReleaseDeploying:
			s.sweepDeploying(ctx, r)
		case domain.ReleaseVerifying:
			s.sweepVerifying(ctx, r)
		case domain.ReleaseRollingBack:
			s.sweepRollingBack(ctx, r)
		}
	}
}

func (s *Service) sweepDeploying(ctx context.Context, r domain.Release) {
	if r.Mode == domain.DeliveryBatch {
		switch r.Executor {
		case domain.ExecutorLocal:
			s.sweepDeployingLocal(ctx, r)
			return
		case domain.ExecutorStore:
			s.sweepDeployingStore(ctx, r)
			return
		}
		// github_actions falls through: it watches the tag's workflow run
		// exactly the way resolveDeployStatus already does below.
	}
	status, err := s.resolveDeployStatus(ctx, r)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: resolving deploy status failed")
		return
	}
	now := s.now()
	startedAt := r.DeployStartedAt
	switch status.State {
	case domain.DeployWatchPending:
		if startedAt != nil && now.Sub(*startedAt) > pendingDeployTimeout {
			s.failDeploying(ctx, r, fmt.Sprintf("the deploy of %s did not settle within 60 minutes", domain.ShortSHA(r.CommitSHA)))
		}
	case domain.DeployWatchNoSignal:
		if startedAt != nil && now.Sub(*startedAt) > noSignalGrace {
			s.failDeploying(ctx, r, fmt.Sprintf("no deploy of %s appeared within 15 minutes — check the delivery profile (mode/workflow)", domain.ShortSHA(r.CommitSHA)))
		}
	case domain.DeployWatchUnknown:
		log.Warn().Str("release_id", r.ID.String()).Str("commit", r.CommitSHA).Msg("release sweeper: deploy status unknown")
		if startedAt != nil && now.Sub(*startedAt) > unknownDeployTimeout {
			s.failDeploying(ctx, r, fmt.Sprintf("the deploy status of %s could not be resolved within 60 minutes", domain.ShortSHA(r.CommitSHA)))
		}
	case domain.DeployWatchFailure:
		r.Deploy = &status
		r.Status = domain.ReleaseFailed
		r.FailureReason = status.Detail
		updated, err := s.store.Update(ctx, r, domain.ReleaseDeploying)
		if err != nil {
			s.logSweepUpdate(err, r.ID)
			return
		}
		s.handBack(ctx, updated)
	case domain.DeployWatchSuccess:
		s.settleDeploySuccess(ctx, r, status)
	}
}

func (s *Service) failDeploying(ctx context.Context, r domain.Release, reason string) {
	r.Status = domain.ReleaseFailed
	r.FailureReason = reason
	updated, err := s.store.Update(ctx, r, domain.ReleaseDeploying)
	if err != nil {
		s.logSweepUpdate(err, r.ID)
		return
	}
	s.handBack(ctx, updated)
}

func (s *Service) settleDeploySuccess(ctx context.Context, r domain.Release, status domain.DeployWatchStatus) {
	now := s.now()
	r.Deploy = &status
	r.DeployedAt = &now
	until := now.Add(time.Duration(r.Profile.Verify.SoakMinutes) * time.Minute)
	r.VerifyUntil = &until

	target, notes := s.resolveVerifyTarget(ctx, r)
	r.Checks.EnvironmentID = target.EnvironmentID
	r.Checks.BaseURL = target.BaseURL
	r.Checks.HealthURL = target.HealthURL
	r.Checks.Notes = notes

	smoke := s.runSmokeChecks(ctx, r.Profile.Verify.Smoke, target.BaseURL)
	r.Checks.Smoke = appendSmokeCapped(r.Checks.Smoke, smoke, maxStoredSmoke)
	if sample, ok := s.probeHealth(ctx, target.HealthURL); ok {
		r.Checks.Health = appendHealthCapped(r.Checks.Health, sample, maxStoredHealth)
	}

	if fail, ok := firstSmokeFailure(smoke); ok {
		r.Checks.EarlyStop = "smoke check failed: " + fail
		r.Status = domain.ReleaseAwaitingVerdict
		updated, err := s.store.Update(ctx, r, domain.ReleaseDeploying)
		if err != nil {
			s.logSweepUpdate(err, r.ID)
			return
		}
		s.handBack(ctx, updated)
		return
	}

	r.Status = domain.ReleaseVerifying
	if _, err := s.store.Update(ctx, r, domain.ReleaseDeploying); err != nil {
		s.logSweepUpdate(err, r.ID)
	}
}

func firstSmokeFailure(results []domain.SmokeResult) (string, bool) {
	for _, res := range results {
		if !res.OK {
			detail := res.Error
			if detail == "" {
				detail = fmt.Sprintf("got status %d", res.Status)
			}
			return fmt.Sprintf("%s %s (%s)", res.Check.Method, res.Check.Path, detail), true
		}
	}
	return "", false
}

func (s *Service) sweepVerifying(ctx context.Context, r domain.Release) {
	now := s.now()

	if sample, ok := s.probeHealth(ctx, r.Checks.HealthURL); ok {
		r.Checks.Health = appendHealthCapped(r.Checks.Health, sample, maxStoredHealth)
		if lastTwoFailed(r.Checks.Health) {
			r.Checks.EarlyStop = "health check failed twice"
		}
	}

	windowOver := r.VerifyUntil != nil && !now.Before(*r.VerifyUntil)

	if r.Checks.EnvironmentID != nil && (windowOver || s.errorCheckDue(r.ID, now)) {
		s.checkNewErrors(ctx, &r)
		s.markErrorChecked(r.ID, now)
	}

	if windowOver {
		final := s.runSmokeChecks(ctx, r.Profile.Verify.Smoke, r.Checks.BaseURL)
		r.Checks.Smoke = appendSmokeCapped(r.Checks.Smoke, final, maxStoredSmoke)
		if r.Checks.EarlyStop == "" {
			if fail, ok := firstSmokeFailure(final); ok {
				r.Checks.EarlyStop = "smoke check failed: " + fail
			}
		}
	}

	if r.Checks.EarlyStop == "" && !windowOver {
		if _, err := s.store.Update(ctx, r, domain.ReleaseVerifying); err != nil {
			s.logSweepUpdate(err, r.ID)
		}
		return
	}

	r.Status = domain.ReleaseAwaitingVerdict
	updated, err := s.store.Update(ctx, r, domain.ReleaseVerifying)
	if err != nil {
		s.logSweepUpdate(err, r.ID)
		return
	}
	s.clearErrorCheck(r.ID)
	s.handBack(ctx, updated)
}

func (s *Service) checkNewErrors(ctx context.Context, r *domain.Release) {
	if s.environments == nil || r.Checks.EnvironmentID == nil || r.DeployedAt == nil {
		return
	}
	groups, err := s.environments.Errors(ctx, *r.Checks.EnvironmentID, *r.DeployedAt)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: reading runtime errors failed")
		return
	}
	r.Checks.NewErrors = newErrorGroups(groups)
	if len(r.Checks.NewErrors) > r.Profile.Verify.MaxNewErrors {
		r.Checks.EarlyStop = fmt.Sprintf("%d new runtime error groups since the deploy", len(r.Checks.NewErrors))
	}
}

func newErrorGroups(groups []domain.RuntimeErrorGroup) []domain.RuntimeErrorGroup {
	seen := map[string]bool{}
	out := make([]domain.RuntimeErrorGroup, 0, len(groups))
	for _, g := range groups {
		if !g.New || seen[g.Fingerprint] {
			continue
		}
		seen[g.Fingerprint] = true
		out = append(out, g)
	}
	return out
}

func lastTwoFailed(samples []domain.HealthSample) bool {
	if len(samples) < 2 {
		return false
	}
	last := samples[len(samples)-2:]
	return !last[0].OK && !last[1].OK
}

// errorCheckDue / markErrorChecked / clearErrorCheck rate-limit runtime error
// reads to at most every 2 minutes. It's a soft cadence to avoid hammering
// the runtime errors API, not a correctness invariant, so an in-process,
// best-effort cache (reset on restart or between test Services) is enough.
func (s *Service) errorCheckDue(releaseID uuid.UUID, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastErrorCheck[releaseID]
	return !ok || now.Sub(last) >= errorCheckInterval
}

func (s *Service) markErrorChecked(releaseID uuid.UUID, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErrorCheck[releaseID] = now
}

func (s *Service) clearErrorCheck(releaseID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lastErrorCheck, releaseID)
}

func (s *Service) logSweepUpdate(err error, releaseID uuid.UUID) {
	if errors.Is(err, domain.ErrReleaseWrongStatus) {
		return
	}
	log.Warn().Err(err).Str("release_id", releaseID.String()).Msg("release sweeper: persisting a transition failed")
}

// resolveDeployStatus: github_actions watches the workflow run; vercel
// matches a cloud deployment by commit prefix when the component has a
// bound production environment, falling back to the commit-status signal
// otherwise.
func (s *Service) resolveDeployStatus(ctx context.Context, r domain.Release) (domain.DeployWatchStatus, error) {
	if r.Executor == domain.ExecutorVercel {
		if env, ok := s.boundVercelEnvironment(ctx, r.RepositoryID, r.ComponentID); ok && s.environments != nil {
			deployments, err := s.environments.Deployments(ctx, env.ID, 20)
			if err != nil {
				log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: reading cloud deployments failed")
			} else if d, ok := matchVercelDeployment(deployments, r.CommitSHA); ok {
				return s.vercelDeployStatus(r, d), nil
			}
		}
		return s.statusForCommit(ctx, r, "")
	}
	return s.statusForCommit(ctx, r, r.Profile.Workflow)
}

func (s *Service) statusForCommit(ctx context.Context, r domain.Release, workflow string) (domain.DeployWatchStatus, error) {
	if s.deployStatus == nil {
		return domain.DeployWatchStatus{State: domain.DeployWatchUnknown, MergeSHA: r.CommitSHA, CheckedAt: s.now()}, nil
	}
	return s.deployStatus.StatusForCommit(ctx, r.RepositoryID, r.CommitSHA, workflow)
}

func matchVercelDeployment(deployments []domain.CloudDeployment, sha string) (domain.CloudDeployment, bool) {
	sha = strings.TrimSpace(sha)
	for _, d := range deployments {
		if commitPrefixMatch(strings.TrimSpace(d.CommitSHA), sha) {
			return d, true
		}
	}
	return domain.CloudDeployment{}, false
}

// commitPrefixMatch matches either direction, at least 7 chars — a short sha
// recorded by one side and a full sha by the other must still line up.
func commitPrefixMatch(a, b string) bool {
	if len(a) < 7 || len(b) < 7 {
		return false
	}
	if len(a) <= len(b) {
		return strings.HasPrefix(b, a)
	}
	return strings.HasPrefix(a, b)
}

func (s *Service) vercelDeployStatus(r domain.Release, d domain.CloudDeployment) domain.DeployWatchStatus {
	out := domain.DeployWatchStatus{
		RepositoryID: r.RepositoryID,
		Env:          domain.DeployEnvProd,
		MergeSHA:     r.CommitSHA,
		Signal:       "cloud_deployment",
		RunURL:       d.URL,
		CheckedAt:    s.now(),
	}
	switch d.Status {
	case domain.CloudDeployReady:
		out.State = domain.DeployWatchSuccess
		out.Detail = fmt.Sprintf("the cloud deployment for %s is ready", domain.ShortSHA(r.CommitSHA))
	case domain.CloudDeployError, domain.CloudDeployCanceled:
		out.State = domain.DeployWatchFailure
		out.Detail = fmt.Sprintf("the cloud deployment for %s concluded %q", domain.ShortSHA(r.CommitSHA), d.Status)
	default:
		out.State = domain.DeployWatchPending
		out.Detail = fmt.Sprintf("the cloud deployment for %s is %q", domain.ShortSHA(r.CommitSHA), d.Status)
	}
	return out
}
