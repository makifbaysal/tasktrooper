package release

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// CutPreview is what a human sees before cutting a draft batch release: the
// version this would probably ship as, the commit it would be cut at, and the
// notes it would carry — nothing here is persisted.
func (s *Service) CutPreview(ctx context.Context, releaseID uuid.UUID) (domain.ReleaseCutPreview, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.ReleaseCutPreview{}, err
	}
	if !recuttable(r) {
		return domain.ReleaseCutPreview{}, fmt.Errorf("%w: a cut preview only applies to a draft release, or a pending one that has never deployed (this one is %s)",
			domain.ErrReleaseWrongStatus, r.Status)
	}
	if len(r.Tasks) == 0 {
		return domain.ReleaseCutPreview{}, domain.ErrReleaseEmpty
	}

	repo, err := s.repo(ctx, r.RepositoryID)
	if err != nil {
		return domain.ReleaseCutPreview{}, err
	}

	sha, err := s.resolveCutCommit(ctx, repo, r)
	if err != nil {
		return domain.ReleaseCutPreview{}, err
	}

	previous := s.previousVersion(ctx, repo, r)
	suggested := suggestNextVersion(previous, r.Tasks)

	tag := ""
	if suggested != "" {
		tag = domain.ReleaseTag(r.Profile.TagPattern, suggested)
	}

	return domain.ReleaseCutPreview{
		SuggestedVersion: suggested,
		PreviousVersion:  previous,
		Tag:              tag,
		CommitSHA:        sha,
		Notes:            generateCutNotes(suggested, r.Tasks),
		Tasks:            r.Tasks,
	}, nil
}

// Cut is a human turning a draft into a pending release: it re-reads the
// component's CURRENT confirmed profile (a draft may have sat open under an
// older one) and freezes that, resolves the commit the same way the preview
// did, and wakes the release engineer the way a dispatch release's OpenPending
// catch-up would.
func (s *Service) Cut(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, req domain.ReleaseCutRequest) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if !recuttable(r) {
		return domain.Release{}, fmt.Errorf("%w: cut only applies to a draft release, or a pending one that has never deployed (this one is %s)",
			domain.ErrReleaseWrongStatus, r.Status)
	}
	if len(r.Tasks) == 0 {
		return domain.Release{}, domain.ErrReleaseEmpty
	}
	version := strings.TrimSpace(req.Version)
	if verr := domain.ValidReleaseVersion(version); verr != nil {
		return domain.Release{}, verr
	}

	profile, err := s.currentBatchProfile(ctx, r)
	if err != nil {
		return domain.Release{}, err
	}
	if pending := s.pendingDeployDependencies(ctx, r.TaskIDs()); len(pending) > 0 {
		return domain.Release{}, fmt.Errorf("%w: this release must ship after %s — release those first, or drop the dependency",
			domain.ErrDeployDependencyPending, strings.Join(pending, ", "))
	}

	repo, err := s.repo(ctx, r.RepositoryID)
	if err != nil {
		return domain.Release{}, err
	}

	sha, err := s.resolveCutCommit(ctx, repo, r)
	if err != nil {
		return domain.Release{}, err
	}

	notes := strings.TrimSpace(req.Notes)
	if notes == "" {
		notes = generateCutNotes(version, r.Tasks)
	}

	now := s.now()
	expect := r.Status
	r.Executor = profile.Executor
	r.Profile = profile
	r.Version = version
	r.Tag = domain.ReleaseTag(profile.TagPattern, version)
	r.CommitSHA = sha
	r.Notes = notes
	r.CutAt = &now
	r.Status = domain.ReleasePending

	updated, err := s.store.Update(ctx, r, expect)
	if err != nil {
		return domain.Release{}, err
	}
	_ = actor
	s.stampBeforeDeployConfirmations(ctx, updated)
	s.wakeNewestTask(ctx, updated, domain.ReleasePending)
	return updated, nil
}

func (s *Service) currentBatchProfile(ctx context.Context, r domain.Release) (domain.ComponentDelivery, error) {
	if r.ComponentID == nil {
		return domain.ComponentDelivery{}, fmt.Errorf("%w: this release has no component", domain.ErrReleaseWrongStatus)
	}
	component, err := s.componentByID(ctx, *r.ComponentID)
	if err != nil {
		return domain.ComponentDelivery{}, err
	}
	profile, confirmed := domain.DeliveryConfirmed(component.Delivery)
	if !confirmed || profile.Mode != domain.DeliveryBatch {
		return domain.ComponentDelivery{}, fmt.Errorf("%w: the component no longer batches releases", domain.ErrReleaseWrongStatus)
	}
	return profile, nil
}

// resolveCutCommit is the default branch head on origin, checked to contain
// every task's merge commit — they were merged there, so a missing one means
// origin has moved in a way this draft has not caught up with (a force-push,
// a merge commit rewritten by a squash elsewhere).
func (s *Service) resolveCutCommit(ctx context.Context, repo domain.Repository, r domain.Release) (string, error) {
	if s.git == nil {
		return "", fmt.Errorf("no git client is configured on this deployment")
	}
	head, err := s.git.RemoteHead(ctx, repo.RootPath)
	if err != nil {
		return "", fmt.Errorf("resolving the default branch head: %w", err)
	}
	var missing []string
	for _, t := range r.Tasks {
		sha := strings.TrimSpace(t.MergeCommitSHA)
		if sha == "" {
			missing = append(missing, taskLabel(t))
			continue
		}
		ok, ierr := s.git.IsAncestor(ctx, repo.RootPath, sha, head)
		if ierr != nil {
			log.Warn().Err(ierr).Str("task_id", t.ID.String()).Msg("release: checking a task's merge commit against the default branch failed")
			missing = append(missing, taskLabel(t))
			continue
		}
		if !ok {
			missing = append(missing, taskLabel(t))
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("these tasks' merge commits are not on the default branch: %s", strings.Join(missing, ", "))
	}
	return head, nil
}

// recuttable is a draft, or a pending release Deploy has never even been
// attempted on (DeployStartedAt nil — Deploy claims it the moment it tries):
// a human who picked the wrong version, or hit ErrReleaseTagExists, can pick
// a different one instead of the release being stuck.
func recuttable(r domain.Release) bool {
	if r.Status == domain.ReleaseDraft {
		return true
	}
	return r.Status == domain.ReleasePending && r.DeployStartedAt == nil
}

func taskLabel(t domain.ReleaseTaskRef) string {
	if t.Key != "" {
		return t.Key
	}
	return t.ID.String()
}

// previousVersion is the highest semver version among: the component's
// newest released release, the newest git tag matching the profile's tag
// pattern, and any release of the component that carries a tag at all
// (failed/rolled_back included — a version that was already tagged must
// never be picked again even if that attempt never shipped). When none of
// the three parses as semver, the first non-empty one is returned as-is (for
// display) and SuggestedVersion is left for a human to type.
func (s *Service) previousVersion(ctx context.Context, repo domain.Repository, r domain.Release) string {
	best := versionCandidate{}

	if last, err := s.store.LastReleased(ctx, r.RepositoryID, r.ComponentID, s.now()); err == nil {
		best = higherCandidate(best, newVersionCandidate(last.Version))
	} else if !errors.Is(err, domain.ErrReleaseNotFound) {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: finding the previous released version for a cut preview failed")
	}

	pattern := r.Profile.TagPattern
	if strings.TrimSpace(pattern) == "" {
		pattern = domain.DefaultReleaseTagPattern
	}

	if s.git != nil {
		tag, err := s.git.LatestTag(ctx, repo.RootPath, tagGlob(pattern))
		if err != nil {
			log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: finding the previous tag for a cut preview failed")
		} else if tag != "" {
			best = higherCandidate(best, newVersionCandidate(versionFromTag(pattern, tag)))
		}
	}

	best = higherCandidate(best, newVersionCandidate(s.highestTaggedComponentVersion(ctx, r, pattern)))

	return best.value
}

// highestTaggedComponentVersion scans every release of the component
// (any status, any outcome) for the highest version its Tag decodes to.
func (s *Service) highestTaggedComponentVersion(ctx context.Context, r domain.Release, pattern string) string {
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		RepositoryID: &r.RepositoryID,
		ComponentID:  r.ComponentID,
		Limit:        100,
	})
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: listing the component's releases for a cut preview failed")
		return ""
	}
	best := versionCandidate{}
	for _, rel := range releases {
		if strings.TrimSpace(rel.Tag) == "" {
			continue
		}
		best = higherCandidate(best, newVersionCandidate(versionFromTag(pattern, rel.Tag)))
	}
	return best.value
}

// versionCandidate lets previousVersion compare values that may or may not
// parse as semver: a semver candidate always outranks a non-semver one, two
// semver candidates compare numerically, and two non-semver ones keep
// whichever was found first (the old LastReleased-then-git-tag priority).
type versionCandidate struct {
	value               string
	major, minor, patch int
	semver              bool
}

func newVersionCandidate(v string) versionCandidate {
	v = strings.TrimSpace(v)
	if v == "" {
		return versionCandidate{}
	}
	major, minor, patch, ok := parseSemver(v)
	return versionCandidate{value: v, major: major, minor: minor, patch: patch, semver: ok}
}

func higherCandidate(a, b versionCandidate) versionCandidate {
	switch {
	case a.value == "":
		return b
	case b.value == "":
		return a
	case a.semver && b.semver:
		if a.major != b.major {
			if b.major > a.major {
				return b
			}
			return a
		}
		if a.minor != b.minor {
			if b.minor > a.minor {
				return b
			}
			return a
		}
		if b.patch > a.patch {
			return b
		}
		return a
	case b.semver:
		return b
	default:
		return a
	}
}

func patternPrefixSuffix(pattern string) (prefix, suffix string) {
	idx := strings.Index(pattern, "{version}")
	if idx < 0 {
		return pattern, ""
	}
	return pattern[:idx], pattern[idx+len("{version}"):]
}

func tagGlob(pattern string) string {
	prefix, suffix := patternPrefixSuffix(pattern)
	return prefix + "*" + suffix
}

func versionFromTag(pattern, tag string) string {
	prefix, suffix := patternPrefixSuffix(pattern)
	v := strings.TrimPrefix(tag, prefix)
	v = strings.TrimSuffix(v, suffix)
	return strings.TrimSpace(v)
}

// suggestNextVersion bumps previous by a semver patch when every task is a
// bug fix, else a minor; no previous version starts at 0.1.0; a previous that
// is not plain MAJOR[.MINOR[.PATCH]] (no pre-release/build suffix) returns ""
// so a human types the version themselves.
func suggestNextVersion(previous string, tasks []domain.ReleaseTaskRef) string {
	previous = strings.TrimSpace(previous)
	if previous == "" {
		return "0.1.0"
	}
	major, minor, patch, ok := parseSemver(previous)
	if !ok {
		return ""
	}
	if allBugTasks(tasks) {
		patch++
	} else {
		minor++
		patch = 0
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

func allBugTasks(tasks []domain.ReleaseTaskRef) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		if t.TaskType != domain.TaskTypeBug {
			return false
		}
	}
	return true
}

func parseSemver(v string) (major, minor, patch int, ok bool) {
	if strings.ContainsAny(v, "-+") {
		return 0, 0, 0, false
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return 0, 0, 0, false
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

// generateCutNotes renders the release notes markdown a cut prefills: a
// version heading, then Features (non-bug tasks) and Fixes (bug tasks),
// omitting a section with nothing in it.
func generateCutNotes(version string, tasks []domain.ReleaseTaskRef) string {
	var features, fixes []string
	for _, t := range tasks {
		line := fmt.Sprintf("- %s %s", t.Key, t.Title)
		if t.TaskType == domain.TaskTypeBug {
			fixes = append(fixes, line)
		} else {
			features = append(features, line)
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s", version)
	if len(features) > 0 {
		sb.WriteString("\n\n### Features\n")
		sb.WriteString(strings.Join(features, "\n"))
	}
	if len(fixes) > 0 {
		sb.WriteString("\n\n### Fixes\n")
		sb.WriteString(strings.Join(fixes, "\n"))
	}
	return sb.String()
}
