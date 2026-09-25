package projectmodel

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Boot runs once at process start: it fails any scan a killed previous
// process left queued/running, then migrates the pre-project-model
// repositories in the background so startup is never blocked on it.
func (s *Service) Boot(ctx context.Context) {
	if n, err := s.store.FailInterruptedScans(ctx); err != nil {
		log.Warn().Err(err).Msg("project model backfill: failing interrupted scans failed")
	} else if n > 0 {
		log.Info().Int("count", n).Msg("project model backfill: failed interrupted scans")
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Any("panic", r).Msg("project model backfill panicked")
			}
		}()
		s.migrateLegacy(s.bgCtx)
	}()
}

// migrateLegacy is the one-time carry-forward of everything the project model
// replaced. Phase 1 scans every repository with zero components, oldest
// first, and turns its legacy kind/sub-projects/pipeline-job rows into
// overrides on what the scan found; a repository stays at zero components
// (and so is retried next boot) until this succeeds, which is what makes the
// phase safe to run on every boot. Phase 2 then carries dependencies into
// links for every repository that now has components, guarded per-row by its
// own idempotency marker instead of a repository-level gate, because those
// rows can legitimately still be missing on a repository this process itself
// just gave its first components.
func (s *Service) migrateLegacy(ctx context.Context) {
	repos, err := s.repos.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("project model backfill: listing repositories failed")
		return
	}

	pending := make([]domain.Repository, 0, len(repos))
	for _, r := range repos {
		components, err := s.store.ListComponents(ctx, r.ID)
		if err != nil {
			log.Warn().Err(err).Str("repository", r.Name).Msg("project model backfill: listing components failed")
			continue
		}
		if len(components) == 0 {
			pending = append(pending, r)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt.Before(pending[j].CreatedAt) })

	for _, repo := range pending {
		s.migrateOneRepository(ctx, repo)
	}

	repos, err = s.repos.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("project model backfill: re-listing repositories failed")
		return
	}
	for _, repo := range repos {
		components, err := s.store.ListComponents(ctx, repo.ID)
		if err != nil {
			log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: listing components failed")
			continue
		}
		if len(components) == 0 {
			continue
		}
		s.migrateLegacyDependencies(ctx, repo, components)
	}
}

func (s *Service) migrateOneRepository(ctx context.Context, repo domain.Repository) {
	var pipelineJobs []domain.RepositoryPipelineJob
	if s.pipelines != nil {
		jobs, err := s.pipelines.ListByRepository(ctx, repo.ID)
		if err != nil {
			log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: listing pipeline jobs failed")
		} else {
			pipelineJobs = jobs
		}
	}

	scan, _, err := s.StartScan(ctx, repo.ID, domain.ScanTriggerMigrate)
	if err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: starting scan failed; will retry next boot")
		return
	}
	finished, err := s.waitForScan(ctx, scan.ID)
	if err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: scan did not finish; will retry next boot")
		return
	}
	if finished.Status != domain.ScanSucceeded {
		log.Warn().Str("repository", repo.Name).Str("scan_error", finished.Error).Msg("project model backfill: scan failed; will retry next boot")
		return
	}

	if err := s.applyLegacyOverrides(ctx, repo, pipelineJobs); err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: applying legacy overrides failed")
	}
	s.logProjection(ctx, repo.ID)
}

func (s *Service) waitForScan(ctx context.Context, scanID uuid.UUID) (domain.ProjectScan, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		scan, err := s.store.GetScan(ctx, scanID)
		if err != nil {
			return domain.ProjectScan{}, err
		}
		if scan.Finished() {
			return scan, nil
		}
		select {
		case <-ctx.Done():
			return scan, fmt.Errorf("scan %s did not finish before the backfill deadline", scanID)
		case <-ticker.C:
		}
	}
}

// legacyKindToRole is the one mapping the backfill trusts to turn an old
// repo/sub-project kind into a component role override; a kind outside this
// set (only ever "monorepo", which is not a role) is left for the scan's own
// detection.
var legacyKindToRole = map[string]domain.ComponentRole{
	domain.RepoKindBackend:  domain.ComponentRoleBackend,
	domain.RepoKindFrontend: domain.ComponentRoleFrontend,
	domain.RepoKindMobile:   domain.ComponentRoleMobile,
	domain.RepoKindWorker:   domain.ComponentRoleWorker,
}

func componentAtPath(components []domain.Component, p string) (domain.Component, bool) {
	for _, c := range components {
		if c.Path == p {
			return c, true
		}
	}
	return domain.Component{}, false
}

func upsertComponentInList(components []domain.Component, saved domain.Component) []domain.Component {
	for i, c := range components {
		if c.ID == saved.ID {
			components[i] = saved
			return components
		}
	}
	return append(components, saved)
}

func (s *Service) applyLegacyOverrides(ctx context.Context, repo domain.Repository, pipelineJobs []domain.RepositoryPipelineJob) error {
	components, err := s.store.ListComponents(ctx, repo.ID)
	if err != nil {
		return fmt.Errorf("apply legacy overrides: %w", err)
	}

	if repo.Kind == domain.RepoKindMonorepo {
		for _, sp := range repo.SubProjects {
			comp, found := componentAtPath(components, sp.Path)
			if !found {
				comp = domain.Component{
					ID:            uuid.New(),
					RepositoryID:  repo.ID,
					Path:          sp.Path,
					Status:        domain.ComponentStatusActive,
					ManuallyAdded: true,
				}
			}
			if role, mappable := legacyKindToRole[sp.Kind]; mappable && comp.Role.Get().LegacyRepoKind() != sp.Kind {
				comp.Role.Override = &role
			}
			comp.Docs = sp.Docs
			comp.Gates = domain.ComponentGates{
				CoverageEnabled:   sp.CoverageEnabled,
				CoverageThreshold: sp.CoverageThreshold,
				MutationEnabled:   sp.MutationEnabled,
				MutationThreshold: sp.MutationThreshold,
			}
			saved, err := s.store.SaveComponent(ctx, comp)
			if err != nil {
				log.Warn().Err(err).Str("repository", repo.Name).Str("path", sp.Path).Msg("project model backfill: saving sub-project component failed")
				continue
			}
			components = upsertComponentInList(components, saved)
		}
	} else if role, mappable := legacyKindToRole[repo.Kind]; mappable {
		if root, ok := componentAtPath(components, "."); ok && root.Role.Get().LegacyRepoKind() != repo.Kind {
			root.Role.Override = &role
			saved, err := s.store.SaveComponent(ctx, root)
			if err != nil {
				log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: saving root component role failed")
			} else {
				components = upsertComponentInList(components, saved)
			}
		}
	}

	checks, err := s.store.ListChecks(ctx, repo.ID)
	if err != nil {
		return fmt.Errorf("apply legacy overrides: %w", err)
	}
	s.applyLegacyPipelineJobs(ctx, repo, components, checks, pipelineJobs)
	return nil
}

func matchingChecks(checks []domain.ComponentCheck, scopeComponentID *uuid.UUID, pred func(domain.ComponentCheck) bool) []domain.ComponentCheck {
	var out []domain.ComponentCheck
	for _, c := range checks {
		if scopeComponentID != nil && c.ComponentID != *scopeComponentID {
			continue
		}
		if pred(c) {
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) applyLegacyPipelineJobs(ctx context.Context, repo domain.Repository, components []domain.Component, checks []domain.ComponentCheck, jobs []domain.RepositoryPipelineJob) {
	for _, job := range jobs {
		var scopeComponentID *uuid.UUID
		if job.SubProjectPath != "" {
			comp, ok := componentAtPath(components, job.SubProjectPath)
			if !ok {
				continue
			}
			id := comp.ID
			scopeComponentID = &id
		}

		switch job.Category {
		case domain.PipelineCategoryValidate, domain.PipelineCategoryBuild, domain.PipelineCategoryTest:
			for _, chk := range matchingChecks(checks, scopeComponentID, func(c domain.ComponentCheck) bool {
				return job.TargetRef != "" && (c.JobName == job.TargetRef || c.JobKey == job.TargetRef)
			}) {
				gate := domain.CheckGateRequired
				chk.Gate.Override = &gate
				if _, err := s.store.SaveCheck(ctx, chk); err != nil {
					log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: setting gate override failed")
				}
			}
		case domain.PipelineCategoryStageDeploy, domain.PipelineCategoryPreProdDeploy, domain.PipelineCategoryProdDeploy:
			targetBase := workflowBasename(job.TargetRef)
			for _, chk := range matchingChecks(checks, scopeComponentID, func(c domain.ComponentCheck) bool {
				return c.Workflow != "" && workflowBasename(c.Workflow) == targetBase
			}) {
				purpose := domain.CheckDeploy
				chk.Purpose.Override = &purpose
				if _, err := s.store.SaveCheck(ctx, chk); err != nil {
					log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: setting deploy purpose override failed")
				}
			}
		}
	}
}

func rootOrFirstActiveComponent(components []domain.Component) (domain.Component, bool) {
	active := activeComponents(components)
	if len(active) == 0 {
		return domain.Component{}, false
	}
	if root, ok := componentAtPath(active, "."); ok {
		return root, true
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Path < active[j].Path })
	return active[0], true
}

func legacyLinkMigrated(links []domain.ComponentLink, marker string) bool {
	for _, l := range links {
		if strings.Contains(l.Detail, marker) {
			return true
		}
	}
	return false
}

func (s *Service) migrateLegacyDependencies(ctx context.Context, repo domain.Repository, components []domain.Component) {
	if s.legacy == nil {
		return
	}
	deps, err := s.legacy.ListLegacyDependencies(ctx, repo.ID)
	if err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: listing legacy dependencies failed")
		return
	}
	if len(deps) == 0 {
		return
	}
	from, ok := rootOrFirstActiveComponent(components)
	if !ok {
		return
	}
	links, err := s.store.ListLinks(ctx, repo.ID)
	if err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: listing links failed")
		return
	}

	for _, dep := range deps {
		marker := "legacy:" + dep.ID.String()
		if legacyLinkMigrated(links, marker) {
			continue
		}
		link, ok := s.buildLegacyDependencyLink(ctx, repo, from, dep, marker)
		if !ok {
			continue
		}
		saved, err := s.store.SaveLink(ctx, link)
		if err != nil {
			log.Warn().Err(err).Str("repository", repo.Name).Msg("project model backfill: saving legacy dependency link failed")
			continue
		}
		links = append(links, saved)
	}
}

func (s *Service) buildLegacyDependencyLink(ctx context.Context, repo domain.Repository, from domain.Component, dep domain.LegacyDependency, marker string) (domain.ComponentLink, bool) {
	detail := marker
	if dep.Note != "" {
		detail = dep.Note + " " + marker
	}
	link := domain.ComponentLink{
		ID:              uuid.New(),
		RepositoryID:    repo.ID,
		FromComponentID: from.ID,
		Detail:          detail,
		Confidence:      domain.ConfidenceExact,
		Status:          domain.LinkConfirmed,
		Source:          domain.LinkSourceUser,
	}

	switch dep.TargetKind {
	case domain.DependencyTargetRepo:
		if dep.TargetRepositoryID == nil {
			return domain.ComponentLink{}, false
		}
		targetComponents, err := s.store.ListComponents(ctx, *dep.TargetRepositoryID)
		if err != nil {
			return domain.ComponentLink{}, false
		}
		target, ok := rootOrFirstActiveComponent(targetComponents)
		if !ok {
			return domain.ComponentLink{}, false
		}
		link.ToComponentID = &target.ID
		link.Protocol = domain.LinkHTTP

	case domain.DependencyTargetSubRepo:
		if dep.TargetRepositoryID == nil {
			return domain.ComponentLink{}, false
		}
		targetComponents, err := s.store.ListComponents(ctx, *dep.TargetRepositoryID)
		if err != nil {
			return domain.ComponentLink{}, false
		}
		target, ok := componentAtPath(targetComponents, dep.TargetSubProjectPath)
		if !ok {
			return domain.ComponentLink{}, false
		}
		link.ToComponentID = &target.ID
		link.Protocol = domain.LinkHTTP

	case domain.DependencyTargetDatabase:
		vendor := dep.DatabaseEngine
		if vendor == "" {
			vendor = "database"
		}
		name := dep.DatabaseLabel
		if name == "" {
			name = dep.DatabaseEngine
		}
		details := map[string]string{}
		if dep.DatabaseHost != "" {
			details["host"] = dep.DatabaseHost
		}
		if dep.DatabasePort != 0 {
			details["port"] = strconv.Itoa(dep.DatabasePort)
		}
		if dep.DatabaseName != "" {
			details["name"] = dep.DatabaseName
		}
		if dep.DatabaseEnv != "" {
			details["env"] = dep.DatabaseEnv
		}
		resource, err := s.store.EnsureResource(ctx, domain.SystemResource{
			Kind:        domain.ResourceDatabase,
			Vendor:      vendor,
			Name:        name,
			IdentityKey: "legacy-db:" + dep.ID.String(),
			Details:     details,
		})
		if err != nil {
			return domain.ComponentLink{}, false
		}
		link.ToResourceID = &resource.ID
		link.Protocol = domain.LinkSQL

	default:
		return domain.ComponentLink{}, false
	}

	return link, true
}
