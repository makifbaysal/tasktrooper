package projectmodel

import (
	"context"
	"path"
	"sort"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// project rewrites the legacy repository fields and pipeline slots from the
// repository's current components and checks, writing only what changed so a
// scan that detects nothing new does not churn every consumer's cache.
func (s *Service) project(ctx context.Context, repositoryID uuid.UUID) error {
	components, err := s.store.ListComponents(ctx, repositoryID)
	if err != nil {
		return err
	}
	checks, err := s.store.ListChecks(ctx, repositoryID)
	if err != nil {
		return err
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return err
	}

	active := make([]domain.Component, 0, len(components))
	for _, c := range components {
		if c.Status == domain.ComponentStatusActive {
			active = append(active, c)
		}
	}
	if len(active) == 0 {
		return nil
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Path < active[j].Path })

	monorepo := len(active) > 1
	if err := s.projectMeta(ctx, repo, active, monorepo); err != nil {
		return err
	}
	if err := s.projectSubProjects(ctx, repo, active, monorepo); err != nil {
		return err
	}
	if err := s.projectQualityGates(ctx, repo, active, monorepo); err != nil {
		return err
	}
	if len(active) == 1 && active[0].Role.Get().LegacyRepoKind() == domain.RepoKindMobile {
		if err := s.projectSingleMobile(ctx, repo, active[0]); err != nil {
			return err
		}
	}
	return s.projectPipeline(ctx, repositoryID, active, checks, monorepo)
}

func (s *Service) projectMeta(ctx context.Context, repo domain.Repository, active []domain.Component, monorepo bool) error {
	kind := active[0].Role.Get().LegacyRepoKind()
	var subRepoKinds []string
	if monorepo {
		kind = domain.RepoKindMonorepo
		subRepoKinds = distinctLegacyKinds(active)
	}

	var kindPtr *string
	if kind != repo.Kind {
		kindPtr = &kind
	}
	var subRepoKindsPtr *[]string
	if !stringSliceEqual(subRepoKinds, repo.SubRepoKinds) {
		subRepoKindsPtr = &subRepoKinds
	}
	if kindPtr == nil && subRepoKindsPtr == nil {
		return nil
	}
	_, err := s.projector.UpdateMeta(ctx, repo.ID, kindPtr, subRepoKindsPtr)
	return err
}

// projectQualityGates writes the repository's coverage/mutation columns from
// the component model: a single-component repo takes that component's own
// gates (nil meaning the default, off/0), a monorepo always projects the
// all-off default because each sub-project carries its own gates
// (projectSubProjects already projects those). Without this, a
// single-component repo's board coverage gate (Repository.EffectiveCoverageGate,
// read with subProjectPath "") would read only the repo columns and the
// component's "Kalite kapıları" card would do nothing.
func (s *Service) projectQualityGates(ctx context.Context, repo domain.Repository, active []domain.Component, monorepo bool) error {
	var coverage, mutation domain.QualityGate
	if !monorepo {
		gates := active[0].Gates
		if gates.CoverageEnabled != nil {
			coverage.Enabled = *gates.CoverageEnabled
		}
		if gates.CoverageThreshold != nil {
			coverage.Threshold = *gates.CoverageThreshold
		}
		if gates.MutationEnabled != nil {
			mutation.Enabled = *gates.MutationEnabled
		}
		if gates.MutationThreshold != nil {
			mutation.Threshold = *gates.MutationThreshold
		}
	}
	if coverage.Enabled == repo.RequireOverallCoverage && coverage.Threshold == repo.CoverageThreshold &&
		mutation.Enabled == repo.MutationEnabled && mutation.Threshold == repo.MutationThreshold {
		return nil
	}
	_, err := s.projector.UpdateQualityGates(ctx, repo.ID, coverage, mutation)
	return err
}

func distinctLegacyKinds(active []domain.Component) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range active {
		k := c.Role.Get().LegacyRepoKind()
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) projectSubProjects(ctx context.Context, repo domain.Repository, active []domain.Component, monorepo bool) error {
	var subProjects []domain.RepoSubProject
	if monorepo {
		subProjects = make([]domain.RepoSubProject, 0, len(active))
		for _, c := range active {
			mobile := effectiveMobile(c)
			subProjects = append(subProjects, domain.RepoSubProject{
				Path:                 c.Path,
				Kind:                 c.Role.Get().LegacyRepoKind(),
				MobilePlatform:       mobile.Platform,
				DetectedAppIdentity:  mobile.Identity,
				DetectedBuildTargets: mobile.BuildTargets,
				Docs:                 c.Docs,
				CoverageEnabled:      c.Gates.CoverageEnabled,
				CoverageThreshold:    c.Gates.CoverageThreshold,
				MutationEnabled:      c.Gates.MutationEnabled,
				MutationThreshold:    c.Gates.MutationThreshold,
			})
		}
	}

	if subProjectsEqual(subProjects, repo.SubProjects) {
		return nil
	}
	validated, err := domain.ValidateSubProjects(subProjects)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repo.ID.String()).Msg("project: sub_projects failed validation; keeping the stored value")
		return nil
	}
	_, err = s.projector.UpdateSubProjects(ctx, repo.ID, validated)
	return err
}

func (s *Service) projectSingleMobile(ctx context.Context, repo domain.Repository, c domain.Component) error {
	mobile := effectiveMobile(c)
	if mobile.Platform != repo.MobilePlatform {
		if _, err := s.projector.UpdateMobilePlatform(ctx, repo.ID, mobile.Platform); err != nil {
			return err
		}
	}
	if mobile.Identity != repo.DetectedAppIdentity {
		if _, err := s.projector.UpdateDetectedAppIdentity(ctx, repo.ID, mobile.Identity); err != nil {
			return err
		}
	}
	if mobile.BuildTargets != repo.DetectedBuildTargets {
		if _, err := s.projector.UpdateDetectedBuildTargets(ctx, repo.ID, mobile.BuildTargets); err != nil {
			return err
		}
	}
	return nil
}

func effectiveMobile(c domain.Component) domain.MobileFacts {
	if c.Mobile == nil {
		return domain.MobileFacts{}
	}
	return c.Mobile.Get()
}

type pipelineSlotKey struct {
	subProjectPath string
	subRepoKind    string
	category       string
}

func (s *Service) projectPipeline(ctx context.Context, repositoryID uuid.UUID, active []domain.Component, checks []domain.ComponentCheck, monorepo bool) error {
	componentByID := make(map[uuid.UUID]domain.Component, len(active))
	for _, c := range active {
		componentByID[c.ID] = c
	}

	type candidate struct {
		check domain.ComponentCheck
		comp  domain.Component
	}
	candidates := make([]candidate, 0, len(checks))
	for _, ch := range checks {
		if ch.Status != domain.ModelStatusActive || ch.Missing {
			continue
		}
		comp, ok := componentByID[ch.ComponentID]
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{check: ch, comp: comp})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].comp.Path != candidates[j].comp.Path {
			return candidates[i].comp.Path < candidates[j].comp.Path
		}
		if candidates[i].check.Workflow != candidates[j].check.Workflow {
			return candidates[i].check.Workflow < candidates[j].check.Workflow
		}
		return candidates[i].check.JobKey < candidates[j].check.JobKey
	})

	seen := map[pipelineSlotKey]bool{}
	var jobs []domain.RepositoryPipelineJob
	for _, cand := range candidates {
		ch, comp := cand.check, cand.comp
		category, targetKind, targetRef, ok := pipelineSlotFor(ch)
		if !ok {
			continue
		}
		key := pipelineSlotKey{category: category}
		if monorepo {
			key.subProjectPath = comp.Path
			key.subRepoKind = comp.Role.Get().LegacyRepoKind()
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		jobs = append(jobs, domain.RepositoryPipelineJob{
			RepositoryID:   repositoryID,
			SubProjectPath: key.subProjectPath,
			SubRepoKind:    key.subRepoKind,
			Category:       category,
			TargetKind:     targetKind,
			TargetRef:      targetRef,
			AutoDetected:   true,
		})
	}

	existing, err := s.pipelines.ListByRepository(ctx, repositoryID)
	if err != nil {
		return err
	}
	if pipelineJobsEqual(jobs, existing) {
		return nil
	}
	_, err = s.pipelines.ReplaceForRepository(ctx, repositoryID, jobs)
	return err
}

func pipelineSlotFor(ch domain.ComponentCheck) (category, targetKind, targetRef string, ok bool) {
	purpose := ch.Purpose.Get()
	switch purpose {
	case domain.CheckLint, domain.CheckTypecheck, domain.CheckBuild, domain.CheckTest:
		if ch.Gate.Get() != domain.CheckGateRequired {
			return "", "", "", false
		}
		switch purpose {
		case domain.CheckLint, domain.CheckTypecheck:
			category = domain.PipelineCategoryValidate
		case domain.CheckBuild:
			category = domain.PipelineCategoryBuild
		case domain.CheckTest:
			category = domain.PipelineCategoryTest
		}
		targetKind = domain.PipelineTargetJob
		targetRef = ch.JobName
		if targetRef == "" {
			targetRef = ch.JobKey
		}
		return category, targetKind, targetRef, targetRef != ""
	case domain.CheckDeploy:
		switch ch.Environment {
		case domain.EnvironmentProduction:
			// A push-only deploy workflow cannot be dispatched: mapping it to
			// prod_deploy anyway made the release/pipeline gate try to
			// workflow_dispatch it and get a 422 back from GitHub.
			if !ch.Dispatchable {
				return "", "", "", false
			}
			category = domain.PipelineCategoryProdDeploy
		case domain.EnvironmentStaging:
			category = domain.PipelineCategoryStageDeploy
		default:
			return "", "", "", false
		}
		targetKind = domain.PipelineTargetWorkflow
		targetRef = path.Base(ch.Workflow)
		return category, targetKind, targetRef, targetRef != "" && targetRef != "."
	default:
		return "", "", "", false
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func subProjectsEqual(a, b []domain.RepoSubProject) bool {
	if len(a) != len(b) {
		return false
	}
	sortSubProjects := func(s []domain.RepoSubProject) []domain.RepoSubProject {
		out := append([]domain.RepoSubProject(nil), s...)
		sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
		return out
	}
	as, bs := sortSubProjects(a), sortSubProjects(b)
	for i := range as {
		x, y := as[i], bs[i]
		if x.Path != y.Path || x.Kind != y.Kind || x.MobilePlatform != y.MobilePlatform ||
			x.DetectedAppIdentity != y.DetectedAppIdentity || x.DetectedBuildTargets != y.DetectedBuildTargets ||
			x.Docs != y.Docs || !boolPtrEqual(x.CoverageEnabled, y.CoverageEnabled) ||
			!float64PtrEqual(x.CoverageThreshold, y.CoverageThreshold) || !boolPtrEqual(x.MutationEnabled, y.MutationEnabled) ||
			!float64PtrEqual(x.MutationThreshold, y.MutationThreshold) {
			return false
		}
	}
	return true
}

func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func float64PtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func pipelineJobsEqual(a []domain.RepositoryPipelineJob, b []domain.RepositoryPipelineJob) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(j domain.RepositoryPipelineJob) pipelineSlotKey {
		return pipelineSlotKey{subProjectPath: j.SubProjectPath, subRepoKind: j.SubRepoKind, category: j.Category}
	}
	byKey := make(map[pipelineSlotKey]domain.RepositoryPipelineJob, len(b))
	for _, j := range b {
		byKey[key(j)] = j
	}
	for _, j := range a {
		other, ok := byKey[key(j)]
		if !ok || other.TargetKind != j.TargetKind || other.TargetRef != j.TargetRef {
			return false
		}
	}
	return true
}
