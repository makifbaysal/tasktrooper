package cloud_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ProjectionSuite struct {
	suite.Suite
	ctx        context.Context
	accounts   *fakeAccounts
	envs       *fakeEnvironments
	components *fakeComponents
	deploys    *fakeDeployTargets
	svc        *cloud.Service

	repoID uuid.UUID
}

func TestProjectionSuite(t *testing.T) {
	suite.Run(t, new(ProjectionSuite))
}

func (s *ProjectionSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.envs = newFakeEnvironments()
	s.components = newFakeComponents()
	s.deploys = newFakeDeployTargets()
	s.repoID = uuid.New()

	s.svc = cloud.NewService(cloud.Deps{
		Accounts:      s.accounts,
		Environments:  s.envs,
		Providers:     []port.CloudProvider{&fakeProvider{kind: domain.CloudVercel}},
		Components:    s.components,
		Scans:         newFakeScans(),
		Repos:         newFakeRepos(domain.Repository{ID: s.repoID, Name: "acme"}),
		DeployTargets: s.deploys,
	})
}

func (s *ProjectionSuite) TestConfirmedProductionEnvironmentProjectsToProdTarget() {
	comp := s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "."})
	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}

	_, err = s.svc.BindEnvironment(s.ctx, comp.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{
		AccountID: &acct.ID, Resource: &ref, URL: "https://prod.example.com",
	})
	s.Require().NoError(err)

	target, err := s.deploys.Get(s.ctx, s.repoID, "", domain.DeployEnvProd)
	s.Require().NoError(err)
	s.Equal("https://prod.example.com", target.BaseURL)
	s.Equal(domain.DeployProviderVercel, target.Provider)
}

func (s *ProjectionSuite) TestProjectionPreservesVarsAndNeverDeletesTargets() {
	comp := s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "."})
	s.deploys.seed(domain.DeployTarget{
		RepositoryID: s.repoID, Env: domain.DeployEnvProd,
		Vars: map[string]string{"custom_var": "keep-me"}, TemplateID: "some-template", HealthURL: "https://health.example.com",
	})

	_, err := s.svc.BindEnvironment(s.ctx, comp.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{URL: "https://new-base.example.com"})
	s.Require().NoError(err)

	target, err := s.deploys.Get(s.ctx, s.repoID, "", domain.DeployEnvProd)
	s.Require().NoError(err)
	s.Equal("https://new-base.example.com", target.BaseURL)
	s.Equal("keep-me", target.Vars["custom_var"])
	s.Equal("some-template", target.TemplateID)
	s.Equal("https://health.example.com", target.HealthURL, "an existing health url is kept when the environment has none")

	s.Require().NoError(s.svc.DeleteEnvironment(s.ctx, mustSingleEnvID(s.T(), s.envs, s.repoID)))
	_, err = s.deploys.Get(s.ctx, s.repoID, "", domain.DeployEnvProd)
	s.Require().NoError(err, "projection never deletes a deploy target")
}

func (s *ProjectionSuite) TestMultiComponentRepositoryScopesBySubProjectPath() {
	compA := s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "api"})
	s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "web"})

	_, err := s.svc.BindEnvironment(s.ctx, compA.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{URL: "https://api.example.com"})
	s.Require().NoError(err)

	target, err := s.deploys.Get(s.ctx, s.repoID, "api", domain.DeployEnvProd)
	s.Require().NoError(err)
	s.Equal("https://api.example.com", target.BaseURL)
}

func (s *ProjectionSuite) TestStagingEnvironmentProjectsToStageTarget() {
	comp := s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "."})
	_, err := s.svc.BindEnvironment(s.ctx, comp.ID, domain.EnvironmentStaging, domain.SaveEnvironmentRequest{URL: "https://stage.example.com"})
	s.Require().NoError(err)

	target, err := s.deploys.Get(s.ctx, s.repoID, "", domain.DeployEnvStage)
	s.Require().NoError(err)
	s.Equal("https://stage.example.com", target.BaseURL)
}

func (s *ProjectionSuite) TestPreviewEnvironmentIsNeverProjected() {
	comp := s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "."})
	_, err := s.svc.BindEnvironment(s.ctx, comp.ID, domain.EnvironmentPreview, domain.SaveEnvironmentRequest{URL: "https://preview.example.com"})
	s.Require().NoError(err)

	targets, err := s.deploys.ListByRepository(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Empty(targets)
}

func mustSingleEnvID(t interface{ Errorf(string, ...interface{}) }, envs *fakeEnvironments, repoID uuid.UUID) uuid.UUID {
	all, _ := envs.ListEnvironments(context.Background(), repoID)
	if len(all) != 1 {
		t.Errorf("expected exactly one environment, got %d", len(all))
		return uuid.Nil
	}
	return all[0].ID
}
