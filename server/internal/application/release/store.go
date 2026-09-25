package release

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	storeReleaseActor = "release engineer"
	storeBuildTimeout = 120 * time.Minute
)

// deployBatchStore starts a release build on every platform the repository
// has a linked, identified store app for. A platform whose start fails keeps
// its error on the record rather than aborting the others; only when every
// platform failed does the whole release fail.
func (s *Service) deployBatchStore(ctx context.Context, r domain.Release) (domain.Release, error) {
	if s.storeOps == nil {
		return domain.Release{}, fmt.Errorf("no store release integration is configured on this deployment")
	}
	apps, err := s.storeOps.AppsByRepository(ctx, r.RepositoryID)
	if err != nil {
		return domain.Release{}, fmt.Errorf("listing linked store apps: %w", err)
	}

	var builds []domain.ReleaseStoreBuild
	anyStarted := false
	for _, app := range apps {
		if strings.TrimSpace(app.Identifier) == "" {
			continue
		}
		build := domain.ReleaseStoreBuild{Platform: app.Platform}
		if tracks, terr := s.storeOps.Tracks(ctx, r.RepositoryID, app.Platform); terr != nil {
			log.Warn().Err(terr).Str("release_id", r.ID.String()).Str("platform", app.Platform).
				Msg("release: reading the baseline store track before a batch build failed")
		} else {
			build.BaselineBuild = tracks.Internal.Build
		}
		start, serr := s.storeOps.StartBuild(ctx, r.RepositoryID, app.Platform, "", storeReleaseActor)
		if serr != nil {
			build.Error = serr.Error()
		} else {
			build.Engine = start.Engine
			anyStarted = true
		}
		builds = append(builds, build)
	}
	if len(builds) == 0 {
		return domain.Release{}, fmt.Errorf("this repository has no linked store app to release")
	}

	r.StoreBuilds = builds
	if !anyStarted {
		r.Status = domain.ReleaseFailed
		r.FailureReason = "starting the store build failed for every platform: " + storeBuildErrorsSummary(builds)
		updated, err := s.store.Update(ctx, r, domain.ReleasePending)
		if err != nil {
			return domain.Release{}, err
		}
		s.handBack(ctx, updated)
		return updated, nil
	}

	now := s.now()
	r.Status = domain.ReleaseDeploying
	r.DeployStartedAt = &now
	return s.store.Update(ctx, r, domain.ReleasePending)
}

func storeBuildErrorsSummary(builds []domain.ReleaseStoreBuild) string {
	var parts []string
	for _, b := range builds {
		if b.Error != "" {
			parts = append(parts, b.Platform+": "+b.Error)
		}
	}
	return strings.Join(parts, "; ")
}

// sweepDeployingStore polls every started platform's internal-channel build
// number; a platform whose Tracks read failed this tick is treated the same
// as one still on its baseline build — waited on, not failed — so a
// transient store-API error cannot fail an otherwise healthy release.
func (s *Service) sweepDeployingStore(ctx context.Context, r domain.Release) {
	if s.storeOps == nil {
		return
	}
	now := s.now()
	allDone := true
	changed := false
	var waiting []string

	for i := range r.StoreBuilds {
		b := &r.StoreBuilds[i]
		if b.Error != "" || b.Build != "" {
			continue
		}
		tracks, err := s.storeOps.Tracks(ctx, r.RepositoryID, b.Platform)
		if err != nil {
			log.Warn().Err(err).Str("release_id", r.ID.String()).Str("platform", b.Platform).
				Msg("release sweeper: reading a store track failed")
			allDone = false
			waiting = append(waiting, b.Platform)
			continue
		}
		if tracks.Internal.Build != "" && tracks.Internal.Build != b.BaselineBuild {
			b.Build = tracks.Internal.Build
			changed = true
			continue
		}
		allDone = false
		waiting = append(waiting, b.Platform)
	}

	if allDone {
		status := domain.DeployWatchStatus{
			RepositoryID: r.RepositoryID,
			MergeSHA:     r.CommitSHA,
			Signal:       "store_build",
			State:        domain.DeployWatchSuccess,
			Detail:       "every started store build has a new internal-channel build",
			CheckedAt:    now,
		}
		s.settleDeploySuccess(ctx, r, status)
		return
	}

	if r.DeployStartedAt != nil && now.Sub(*r.DeployStartedAt) > storeBuildTimeout {
		s.failDeploying(ctx, r, fmt.Sprintf(
			"the store build did not finish within 120 minutes — still waiting on %s", strings.Join(waiting, ", ")))
		return
	}

	if changed {
		if _, err := s.store.Update(ctx, r, domain.ReleaseDeploying); err != nil {
			s.logSweepUpdate(err, r.ID)
		}
	}
}
