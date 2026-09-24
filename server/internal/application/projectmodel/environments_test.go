package projectmodel

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type EnvironmentIntegrationSuite struct {
	suite.Suite

	store    *fakeStore
	repos    *fakeRepos
	projects *fakeProjects
	envs     *fakeEnvironmentStore
	svc      *Service

	repo domain.Repository
	comp domain.Component
}

func TestEnvironmentIntegrationSuite(t *testing.T) {
	suite.Run(t, new(EnvironmentIntegrationSuite))
}

func (s *EnvironmentIntegrationSuite) SetupTest() {
	s.store = newFakeStore()
	s.repos = newFakeRepos()
	s.projects = newFakeProjects()
	s.envs = newFakeEnvironmentStore()
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})
	s.svc.SetEnvironmentReader(s.envs)

	s.repo = domain.Repository{ID: uuid.New(), Name: "acme"}
	s.repos.set(s.repo)
	s.comp = s.store.seedComponent(domain.Component{
		RepositoryID: s.repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Name: domain.Fact[string]{Detected: ptr("api")},
		Role: domain.Fact[domain.ComponentRole]{Detected: rolePtr(domain.ComponentRoleBackend)},
	})
}

func (s *EnvironmentIntegrationSuite) TestRepositoryModelWithNoReaderConfiguredReturnsEmptySlice() {
	svc := NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})
	model, err := svc.RepositoryModel(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.NotNil(model.Environments)
	s.Empty(model.Environments)
}

func (s *EnvironmentIntegrationSuite) TestRepositoryModelReadsEnvironmentsFromTheReader() {
	seeded := s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repo.ID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel, URL: "https://api.example.com",
	})

	model, err := s.svc.RepositoryModel(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.Require().Len(model.Environments, 1)
	s.Equal(seeded.ID, model.Environments[0].ID)
}

func (s *EnvironmentIntegrationSuite) TestRepositoryModelReviewIncludesSuggestedEnvironment() {
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repo.ID, ComponentID: s.comp.ID, Environment: domain.EnvironmentStaging,
		Status: domain.LinkSuggested, Confidence: domain.ConfidenceMedium,
	})

	model, err := s.svc.RepositoryModel(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.Require().Len(model.Review, 1)
	item := model.Review[0]
	s.Equal(domain.ReviewEnvironment, item.Kind)
	s.Equal(s.comp.ID, item.ComponentID)
	s.Equal(domain.ConfidenceMedium, item.Confidence)
}

func (s *EnvironmentIntegrationSuite) TestRepositoryModelReviewExcludesConfirmedAndDismissedEnvironments() {
	s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repo.ID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction, Status: domain.LinkConfirmed})
	s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repo.ID, ComponentID: s.comp.ID, Environment: domain.EnvironmentPreview, Status: domain.LinkDismissed})

	model, err := s.svc.RepositoryModel(context.Background(), s.repo.ID)
	s.Require().NoError(err)
	s.Empty(model.Review)
}

func (s *EnvironmentIntegrationSuite) TestProjectsOverviewEnvironmentSummaryIncludesHealth() {
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repo.ID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Provider: domain.CloudGCP,
		Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "svc-1", Name: "api"},
		URL:      "https://api.example.com",
		Health:   &domain.EnvironmentHealth{Status: domain.CloudStatusHealthy, ErrorCount24h: 3},
	})

	overview, err := s.svc.ProjectsOverview(context.Background())
	s.Require().NoError(err)
	s.Require().Len(overview.Unassigned, 1)
	summary := overview.Unassigned[0]
	s.Require().Len(summary.Environments, 1)
	es := summary.Environments[0]
	s.Equal(domain.EnvironmentProduction, es.Environment)
	s.Equal(domain.CloudGCP, es.Provider)
	s.Equal("api", es.ResourceName)
	s.Equal(domain.CloudStatusHealthy, es.Health)
	s.Equal(3, es.ErrorCount24h)
}

func (s *EnvironmentIntegrationSuite) TestProjectsOverviewNeverReturnsNilEnvironments() {
	overview, err := s.svc.ProjectsOverview(context.Background())
	s.Require().NoError(err)
	s.Require().Len(overview.Unassigned, 1)
	s.NotNil(overview.Unassigned[0].Environments)
}

func (s *EnvironmentIntegrationSuite) TestBriefRunsOnBlockListsConfirmedEnvironmentsAndLogsHint() {
	s.repo.RootPath = "/tmp/does-not-matter"
	s.repos.set(s.repo)
	acctID := uuid.New()
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repo.ID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel, AccountID: &acctID,
		Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1", Name: "acme-api"},
		URL:      "https://acme-api.vercel.app",
	})
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repo.ID, ComponentID: s.comp.ID, Environment: domain.EnvironmentStaging,
		Status: domain.LinkSuggested,
	})

	brief, err := s.svc.Brief(context.Background(), s.repo.ID, BriefScope{})
	s.Require().NoError(err)
	s.Contains(brief, "Runs on:")
	s.Contains(brief, "production: vercel acme-api — https://acme-api.vercel.app")
	s.Contains(brief, "Logs and errors: query_runtime_logs, list_runtime_errors")
	s.NotContains(brief, "staging:", "a suggested (not confirmed) environment must not appear in the brief")
}

func (s *EnvironmentIntegrationSuite) TestBriefRunsOnBlockOmitsLogsHintForUnboundCustomEnvironment() {
	s.repo.RootPath = "/tmp/does-not-matter"
	s.repos.set(s.repo)
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repo.ID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, URL: "https://custom.example.com",
	})

	brief, err := s.svc.Brief(context.Background(), s.repo.ID, BriefScope{})
	s.Require().NoError(err)
	s.Contains(brief, "Runs on:")
	s.Contains(brief, "production: https://custom.example.com")
	s.NotContains(brief, "Logs and errors:")
}

func (s *EnvironmentIntegrationSuite) TestBriefOmitsRunsOnBlockWithNoEnvironments() {
	s.repo.RootPath = "/tmp/does-not-matter"
	s.repos.set(s.repo)

	brief, err := s.svc.Brief(context.Background(), s.repo.ID, BriefScope{})
	s.Require().NoError(err)
	s.NotContains(brief, "Runs on:")
}
