package release

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DefaultHealthWindow mirrors deploywatch.DefaultHealthWindow: how long after
// a release's FinishedAt/DeployedAt a production incident is still
// attributed to it.
const DefaultHealthWindow = 15 * time.Minute

// attributionReleaseLookback bounds how many of a repository's newest
// `released` releases AttributeRelease considers — the window is a handful
// of minutes, so anything beyond a few recent releases cannot match anyway.
const attributionReleaseLookback = 20

// HealthWindow reports how long after a release finished an incident is
// still attributed to it. Satisfies prodops.ReleaseAttributor alongside
// AttributeRelease, so the integrator can wire release attribution ahead of
// deploywatch's task-keyed fallback.
func (s *Service) HealthWindow() time.Duration { return s.healthWindow }

// AttributeRelease finds the newest `released` release of the repository
// whose FinishedAt (or DeployedAt, when FinishedAt is unset) falls within the
// health window before onset, and names its newest task as the one the
// incident belongs to. Only the production environment attributes — a
// release has no other notion of "environment" to key on.
func (s *Service) AttributeRelease(ctx context.Context, repositoryID uuid.UUID, env string, onset time.Time) (domain.ReleaseAttribution, bool) {
	if s.store == nil || repositoryID == uuid.Nil {
		return domain.ReleaseAttribution{}, false
	}
	if strings.TrimSpace(env) == "" {
		env = domain.DeployEnvProd
	}
	if env != domain.DeployEnvProd {
		return domain.ReleaseAttribution{}, false
	}
	if onset.IsZero() {
		onset = s.now()
	}

	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		RepositoryID: &repositoryID,
		Statuses:     []domain.ReleaseStatus{domain.ReleaseReleased},
		Limit:        attributionReleaseLookback,
	})
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).
			Msg("release: listing released releases for attribution failed")
		return domain.ReleaseAttribution{}, false
	}

	live, ok := liveRelease(releases, onset, s.healthWindow)
	if !ok || len(live.Tasks) == 0 {
		return domain.ReleaseAttribution{}, false
	}
	newest := live.Tasks[len(live.Tasks)-1]
	return domain.ReleaseAttribution{
		TaskID:       newest.ID,
		TaskKey:      newest.Key,
		Title:        newest.Title,
		MergeSHA:     live.CommitSHA,
		Env:          env,
		DeployedAt:   releaseFinishedAt(live),
		AutoRollback: live.Profile.AutoRollback,
	}, true
}

// releaseFinishedAt is the moment a release's verdict took effect: FinishedAt
// when it is set, else DeployedAt (a release can be `released` only after
// FinishedAt is set, but the fallback keeps this safe if that ever changes).
func releaseFinishedAt(r domain.Release) time.Time {
	if r.FinishedAt != nil {
		return *r.FinishedAt
	}
	if r.DeployedAt != nil {
		return *r.DeployedAt
	}
	return time.Time{}
}

// liveRelease picks, among releases finished at or before onset and within
// window of it, the one that finished most recently — the release onset's
// incident is most plausibly attributed to, mirroring
// deploywatch.liveRun's selection.
func liveRelease(releases []domain.Release, onset time.Time, window time.Duration) (domain.Release, bool) {
	var best domain.Release
	found := false
	for _, r := range releases {
		finished := releaseFinishedAt(r)
		if finished.IsZero() || finished.After(onset) {
			continue
		}
		if onset.Sub(finished) > window {
			continue
		}
		if !found || finished.After(releaseFinishedAt(best)) {
			best = r
			found = true
		}
	}
	return best, found
}
