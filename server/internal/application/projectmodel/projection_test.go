package projectmodel

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ProjectionSuite struct {
	suite.Suite

	store     *fakeStore
	repos     *fakeRepos
	projector *fakeProjector
	pipelines *fakePipelines
	svc       *Service

	repo domain.Repository
}

func TestProjectionSuite(t *testing.T) {
	suite.Run(t, new(ProjectionSuite))
}

func (s *ProjectionSuite) SetupTest() {
	s.store = newFakeStore()
	s.repo = domain.Repository{ID: uuid.New(), Name: "widgets", RootPath: "/repos/widgets"}
	s.repos = newFakeRepos(s.repo)
	s.projector = &fakeProjector{repos: s.repos}
	s.pipelines = newFakePipelines()

	s.svc = NewService(Deps{
		Store:     s.store,
		Repos:     s.repos,
		Projector: s.projector,
		Pipelines: s.pipelines,
	})
}

func (s *ProjectionSuite) TestZeroComponentsLeavesRepositoryUnchanged() {
	err := s.svc.project(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	meta, subProjects, mobile, identity, targets := s.projector.callCounts()
	s.Zero(meta + subProjects + mobile + identity + targets)
	s.Zero(s.pipelines.replaceCallCount())
}

func (s *ProjectionSuite) TestSingleComponentKind() {
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         ".",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Status:       domain.ComponentStatusActive,
	})

	err := s.svc.project(context.Background(), s.repo.ID)
	s.Require().NoError(err)

	updated, err := s.repos.Get(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.Equal(domain.RepoKindBackend, updated.Kind)
	s.Empty(updated.SubRepoKinds)
	s.Empty(updated.SubProjects)
}

func (s *ProjectionSuite) TestMonorepoKindAndSubProjects() {
	coverageThreshold := 80.0
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         "apps/web",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleFrontend)},
		Docs:         domain.RepositoryDocs{CodingStandards: "apps/web/.ai/coding-standards.md"},
		Gates:        domain.ComponentGates{CoverageEnabled: ptr(true), CoverageThreshold: &coverageThreshold},
		Status:       domain.ComponentStatusActive,
	})
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         "services/api",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Status:       domain.ComponentStatusActive,
	})

	err := s.svc.project(context.Background(), s.repo.ID)
	s.Require().NoError(err)

	updated, err := s.repos.Get(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.Equal(domain.RepoKindMonorepo, updated.Kind)
	s.Equal([]string{domain.RepoKindBackend, domain.RepoKindFrontend}, updated.SubRepoKinds)
	s.Require().Len(updated.SubProjects, 2)

	web := updated.SubProjects[0]
	if web.Path != "apps/web" {
		web = updated.SubProjects[1]
	}
	s.Equal(domain.RepoKindFrontend, web.Kind)
	s.Equal("apps/web/.ai/coding-standards.md", web.Docs.CodingStandards)
	s.Require().NotNil(web.CoverageEnabled)
	s.True(*web.CoverageEnabled)
	s.Require().NotNil(web.CoverageThreshold)
	s.Equal(80.0, *web.CoverageThreshold)
}

func (s *ProjectionSuite) TestMonorepoSubProjectCarriesMobileFacts() {
	mobile := domain.MobileFacts{
		Platform:     domain.MobilePlatformIOS,
		Identity:     domain.AppIdentity{BundleID: "com.acme.app"},
		BuildTargets: domain.BuildTargets{XcodeScheme: "App"},
	}
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         "apps/mobile",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleMobile)},
		Mobile:       &domain.Fact[domain.MobileFacts]{Detected: &mobile},
		Status:       domain.ComponentStatusActive,
	})
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         "services/api",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Status:       domain.ComponentStatusActive,
	})

	err := s.svc.project(context.Background(), s.repo.ID)
	s.Require().NoError(err)

	updated, err := s.repos.Get(context.Background(), s.repo.ID)
	s.Require().NoError(err)

	var mobileSub domain.RepoSubProject
	for _, sp := range updated.SubProjects {
		if sp.Path == "apps/mobile" {
			mobileSub = sp
		}
	}
	s.Equal(domain.MobilePlatformIOS, mobileSub.MobilePlatform)
	s.Equal("com.acme.app", mobileSub.DetectedAppIdentity.BundleID)
	s.Equal("App", mobileSub.DetectedBuildTargets.XcodeScheme)

	// A single mobile component (not this monorepo case) is the only one that
	// also writes the repository-level mobile fields.
	s.Empty(updated.MobilePlatform)
}

func (s *ProjectionSuite) TestSingleMobileComponentWritesRepositoryFields() {
	mobile := domain.MobileFacts{
		Platform:     domain.MobilePlatformAndroid,
		Identity:     domain.AppIdentity{PackageName: "com.acme.app"},
		BuildTargets: domain.BuildTargets{GradleModule: "app"},
	}
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         ".",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleMobile)},
		Mobile:       &domain.Fact[domain.MobileFacts]{Detected: &mobile},
		Status:       domain.ComponentStatusActive,
	})

	err := s.svc.project(context.Background(), s.repo.ID)
	s.Require().NoError(err)

	updated, err := s.repos.Get(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.Equal(domain.RepoKindMobile, updated.Kind)
	s.Equal(domain.MobilePlatformAndroid, updated.MobilePlatform)
	s.Equal("com.acme.app", updated.DetectedAppIdentity.PackageName)
	s.Equal("app", updated.DetectedBuildTargets.GradleModule)
}

func (s *ProjectionSuite) TestProjectionOnlyWritesWhenChanged() {
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         ".",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Status:       domain.ComponentStatusActive,
	})

	s.Require().NoError(s.svc.project(context.Background(), s.repo.ID))
	meta1, subProjects1, _, _, _ := s.projector.callCounts()
	s.Equal(1, meta1)
	s.Equal(0, subProjects1)

	s.Require().NoError(s.svc.project(context.Background(), s.repo.ID))
	meta2, subProjects2, _, _, _ := s.projector.callCounts()
	s.Equal(meta1, meta2, "a second projection with nothing changed must not write again")
	s.Equal(subProjects1, subProjects2)
}

func (s *ProjectionSuite) TestPipelineSlotsOneRowPerCategory() {
	comp := s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         ".",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Status:       domain.ComponentStatusActive,
	})

	s.store.seedCheck(domain.ComponentCheck{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		ComponentID:  comp.ID,
		Source:       domain.CheckSourceCI,
		Workflow:     "ci.yml",
		JobKey:       "lint",
		JobName:      "Lint",
		Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckLint)},
		Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateRequired)},
		Status:       domain.ModelStatusActive,
	})
	s.store.seedCheck(domain.ComponentCheck{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		ComponentID:  comp.ID,
		Source:       domain.CheckSourceCI,
		Workflow:     "ci.yml",
		JobKey:       "typecheck",
		JobName:      "Typecheck",
		Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckTypecheck)},
		Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateRequired)},
		Status:       domain.ModelStatusActive,
	})
	s.store.seedCheck(domain.ComponentCheck{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		ComponentID:  comp.ID,
		Source:       domain.CheckSourceCI,
		Workflow:     "ci.yml",
		JobKey:       "test",
		JobName:      "Test",
		Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckTest)},
		Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateRequired)},
		Status:       domain.ModelStatusActive,
	})
	s.store.seedCheck(domain.ComponentCheck{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		ComponentID:  comp.ID,
		Source:       domain.CheckSourceCI,
		Workflow:     "deploy-prod.yml",
		JobKey:       "deploy",
		Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckDeploy)},
		Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateInfo)},
		Environment:  domain.EnvironmentProduction,
		Status:       domain.ModelStatusActive,
	})
	// A check with gate off must never claim a slot.
	s.store.seedCheck(domain.ComponentCheck{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		ComponentID:  comp.ID,
		Source:       domain.CheckSourceCI,
		Workflow:     "ci.yml",
		JobKey:       "build",
		JobName:      "Build",
		Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckBuild)},
		Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateOff)},
		Status:       domain.ModelStatusActive,
	})

	err := s.svc.project(context.Background(), s.repo.ID)
	s.Require().NoError(err)

	jobs, err := s.pipelines.ListByRepository(context.Background(), s.repo.ID)
	s.Require().NoError(err)

	byCategory := map[string]domain.RepositoryPipelineJob{}
	for _, j := range jobs {
		byCategory[j.Category] = j
	}
	s.Require().Contains(byCategory, domain.PipelineCategoryValidate)
	s.Equal(domain.PipelineTargetJob, byCategory[domain.PipelineCategoryValidate].TargetKind)
	s.Contains([]string{"Lint", "Typecheck"}, byCategory[domain.PipelineCategoryValidate].TargetRef)

	s.Require().Contains(byCategory, domain.PipelineCategoryTest)
	s.Equal("Test", byCategory[domain.PipelineCategoryTest].TargetRef)

	s.Require().Contains(byCategory, domain.PipelineCategoryProdDeploy)
	s.Equal(domain.PipelineTargetWorkflow, byCategory[domain.PipelineCategoryProdDeploy].TargetKind)
	s.Equal("deploy-prod.yml", byCategory[domain.PipelineCategoryProdDeploy].TargetRef)

	s.NotContains(byCategory, domain.PipelineCategoryBuild)

	// One row per category even though lint AND typecheck both map to validate.
	validateCount := 0
	for _, j := range jobs {
		if j.Category == domain.PipelineCategoryValidate {
			validateCount++
		}
	}
	s.Equal(1, validateCount)
}

func (s *ProjectionSuite) TestPipelineReplaceOnlyWhenChanged() {
	comp := s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         ".",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Status:       domain.ComponentStatusActive,
	})
	s.store.seedCheck(domain.ComponentCheck{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		ComponentID:  comp.ID,
		Source:       domain.CheckSourceCI,
		Workflow:     "ci.yml",
		JobKey:       "test",
		JobName:      "Test",
		Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckTest)},
		Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateRequired)},
		Status:       domain.ModelStatusActive,
	})

	s.Require().NoError(s.svc.project(context.Background(), s.repo.ID))
	s.Equal(1, s.pipelines.replaceCallCount())

	s.Require().NoError(s.svc.project(context.Background(), s.repo.ID))
	s.Equal(1, s.pipelines.replaceCallCount(), "no check changed, so the pipeline slots must not be rewritten")
}

func (s *ProjectionSuite) TestProjectQualityGatesSingleComponent() {
	on := true
	threshold := 55.0
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         ".",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Gates: domain.ComponentGates{
			CoverageEnabled: &on, CoverageThreshold: &threshold,
			MutationEnabled: &on, MutationThreshold: &threshold,
		},
		Status: domain.ComponentStatusActive,
	})

	s.Require().NoError(s.svc.project(context.Background(), s.repo.ID))

	s.Equal(1, s.projector.qualityGatesCallCount())
	s.Equal(domain.QualityGate{Enabled: true, Threshold: 55.0}, s.projector.lastQualityGatesCoverage)
	s.Equal(domain.QualityGate{Enabled: true, Threshold: 55.0}, s.projector.lastQualityGatesMutation)

	updated, err := s.repos.Get(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.True(updated.RequireOverallCoverage)
	s.Equal(55.0, updated.CoverageThreshold)
	s.True(updated.MutationEnabled)
	s.Equal(55.0, updated.MutationThreshold)
}

// TestProjectQualityGatesNilGatesMeansDefaults proves a component with no
// gate overrides projects the all-off default, not whatever the repository
// happened to carry before (which it was writing to itself in the pre-model
// world).
func (s *ProjectionSuite) TestProjectQualityGatesNilGatesMeansDefaults() {
	s.repo.RequireOverallCoverage = true
	s.repo.CoverageThreshold = 90
	s.repos.set(s.repo)

	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         ".",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Status:       domain.ComponentStatusActive,
	})

	s.Require().NoError(s.svc.project(context.Background(), s.repo.ID))

	s.Equal(1, s.projector.qualityGatesCallCount())
	updated, err := s.repos.Get(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.False(updated.RequireOverallCoverage)
	s.Zero(updated.CoverageThreshold)
	s.False(updated.MutationEnabled)
	s.Zero(updated.MutationThreshold)
}

// TestProjectQualityGatesMonorepoAlwaysDefaults proves a monorepo's repo-level
// gate columns always project to the all-off default: each sub-project
// carries its own gates (projectSubProjects), so the repo columns have
// nothing to say.
func (s *ProjectionSuite) TestProjectQualityGatesMonorepoAlwaysDefaults() {
	on := true
	threshold := 42.0
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         "apps/web",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleFrontend)},
		Gates:        domain.ComponentGates{CoverageEnabled: &on, CoverageThreshold: &threshold},
		Status:       domain.ComponentStatusActive,
	})
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         "services/api",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Status:       domain.ComponentStatusActive,
	})

	s.repo.RequireOverallCoverage = true
	s.repo.CoverageThreshold = 77
	s.repos.set(s.repo)

	s.Require().NoError(s.svc.project(context.Background(), s.repo.ID))

	s.Equal(1, s.projector.qualityGatesCallCount())
	updated, err := s.repos.Get(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.False(updated.RequireOverallCoverage, "a monorepo carries its gates on each sub-project, not the repo")
	s.Zero(updated.CoverageThreshold)
}

func (s *ProjectionSuite) TestProjectQualityGatesUnchangedSkipsWrite() {
	on := true
	threshold := 50.0
	s.store.seedComponent(domain.Component{
		ID:           uuid.New(),
		RepositoryID: s.repo.ID,
		Path:         ".",
		Role:         domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)},
		Gates:        domain.ComponentGates{CoverageEnabled: &on, CoverageThreshold: &threshold},
		Status:       domain.ComponentStatusActive,
	})
	s.repo.RequireOverallCoverage = true
	s.repo.CoverageThreshold = 50
	s.repos.set(s.repo)

	s.Require().NoError(s.svc.project(context.Background(), s.repo.ID))
	s.Equal(0, s.projector.qualityGatesCallCount(), "the repo already matches the component's gates; nothing to write")
}
