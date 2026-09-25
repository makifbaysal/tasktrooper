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

// sweepBatch is also the store's raised internal cap: a request this
// large used to come back silently truncated to the store's old 100-row
// limit, stranding any watched release past the first 100.
const sweepBatch = 1000

const (
	pendingDeployTimeout = 60 * time.Minute
	noSignalGrace        = 15 * time.Minute
	unknownDeployTimeout = 60 * time.Minute
	errorCheckInterval   = 2 * time.Minute
	rollbackTimeout      = 30 * time.Minute
	rollbackNoSignalWait = 15 * time.Minute

	// handBackReWakeInterval / handBackMaxReWakes bound the watchdog's re-wake
	// of a settled release nobody is watching: slow enough not to spam a
	// release a human is simply slow to look at, capped so a release that
	// never gets a verdict does not wake forever.
	handBackReWakeInterval = 10 * time.Minute
	handBackMaxReWakes     = 6
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
// one step, then runs the hand-back watchdog. Every transition is an
// optimistic store.Update(r, expect): on ErrReleaseWrongStatus the release is
// skipped — someone else (another sweep tick, an agent's Finish/Rollback)
// already moved it.
func (s *Service) SweepOnce(ctx context.Context) {
	if s.store == nil {
		return
	}
	s.sweepWatchedReleases(ctx)
	s.sweepReleaseWatchdog(ctx)
}

func (s *Service) sweepWatchedReleases(ctx context.Context) {
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

// sweepReleaseWatchdog is the safety net around hand-back itself. A card
// parked on release_watch whose release already settled (a race between the
// sweeper and Watch, a crash mid hand-back) is un-stuck immediately. A
// settled release nobody has a parked card for (the agent run that would
// park it is gone, or a hand-back's dispatch was dropped) is re-woken on a
// slow, capped cadence instead of waiting forever — a released/rolled_back
// release is not re-checked because ReleaseListFilter only asks for
// awaiting_verdict/failed here.
func (s *Service) sweepReleaseWatchdog(ctx context.Context) {
	if s.parked == nil {
		return
	}
	parked, err := s.parked.ListBlockedByResource(ctx, domain.ResourceReleaseWatch, sweepBatch)
	if err != nil {
		log.Warn().Err(err).Msg("release sweeper: listing release_watch parks failed")
		return
	}
	parkedTaskIDs := make(map[uuid.UUID]bool, len(parked))
	for _, t := range parked {
		parkedTaskIDs[t.ID] = true
	}

	for _, task := range parked {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.freeStrandedPark(ctx, task)
	}

	s.rewakeUnparkedHandBacks(ctx, parkedTaskIDs)
}

// freeStrandedPark claims and wakes a release_watch park whose release is no
// longer Watched(): hand-back already happened (or was never needed) and
// nothing freed the card.
func (s *Service) freeStrandedPark(ctx context.Context, parked domain.BoardTask) {
	r, err := s.store.ForTask(ctx, parked.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrReleaseNotFound) {
			log.Warn().Err(err).Str("task_id", parked.ID.String()).Msg("release sweeper: resolving a parked card's release failed")
		}
		return
	}
	if r.Status.Watched() {
		return
	}
	task, ok, err := s.parked.TakeBlockedResourceTask(ctx, domain.ResourceReleaseWatch, parked.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", parked.ID.String()).Msg("release sweeper: claiming a stranded release_watch park failed")
		return
	}
	if !ok {
		return
	}
	r = s.stampHandBack(ctx, r)
	s.wake(ctx, r, task)
}

// rewakeUnparkedHandBacks re-sends a hand-back nothing is parked to receive:
// handBackDue reads the persisted bookkeeping straight off each release
// row rather than a process-memory map, so this survives a desktop relaunch
// and behaves the same across every sweeper instance.
func (s *Service) rewakeUnparkedHandBacks(ctx context.Context, parkedTaskIDs map[uuid.UUID]bool) {
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		Statuses: []domain.ReleaseStatus{domain.ReleaseAwaitingVerdict, domain.ReleaseFailed},
		Limit:    sweepBatch,
	})
	if err != nil {
		log.Warn().Err(err).Msg("release sweeper: listing awaiting-verdict/failed releases failed")
		return
	}
	now := s.now()
	for _, r := range releases {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if releaseHasParkedTask(r, parkedTaskIDs) {
			continue
		}
		if !handBackDue(r, now) {
			continue
		}
		s.handBack(ctx, r)
	}
}

func releaseHasParkedTask(r domain.Release, parkedTaskIDs map[uuid.UUID]bool) bool {
	for _, t := range r.Tasks {
		if parkedTaskIDs[t.ID] {
			return true
		}
	}
	return false
}

// handBackDue is N5's persisted re-wake gate, read straight off the release
// row rather than a process-memory map: a hand-back must actually have
// happened (LastHandBackAt set — the watchdog never invents a first one),
// more than ten minutes must have passed since it, no agent may have looked
// at the release since (AgentSeenAt nil or before LastHandBackAt — a live
// ForAgent call resets what the watchdog is waiting on), the re-wake cap
// must not be hit, and the release must not simply be abandoned (untouched
// for more than a day is someone else's problem to notice by then).
func handBackDue(r domain.Release, now time.Time) bool {
	if r.LastHandBackAt == nil {
		return false
	}
	if now.Sub(*r.LastHandBackAt) < handBackReWakeInterval {
		return false
	}
	if r.AgentSeenAt != nil && !r.AgentSeenAt.Before(*r.LastHandBackAt) {
		return false
	}
	if r.HandBackCount >= handBackMaxReWakes {
		return false
	}
	return now.Sub(r.UpdatedAt) <= 24*time.Hour
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
		s.failDeployingOnStatusErrorTimeout(ctx, r, err)
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

// failDeployingOnStatusErrorTimeout is H4: a status-lookup error must not
// reset the deploy timeout clock, or a release whose deploy watch keeps
// erroring (a rate limit, a flaky API) is stranded in deploying forever
// instead of ever reaching a human.
func (s *Service) failDeployingOnStatusErrorTimeout(ctx context.Context, r domain.Release, lastErr error) {
	if r.DeployStartedAt == nil {
		return
	}
	if s.now().Sub(*r.DeployStartedAt) > pendingDeployTimeout {
		s.failDeploying(ctx, r, fmt.Sprintf("the deploy status could not be read for 60 minutes: %s", lastErr))
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
