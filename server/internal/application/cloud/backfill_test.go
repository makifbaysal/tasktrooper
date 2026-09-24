package cloud_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type BackfillSuite struct {
	suite.Suite
	ctx        context.Context
	accounts   *fakeAccounts
	envs       *fakeEnvironments
	components *fakeComponents
	deploys    *fakeDeployTargets
	legacy     *fakeLegacy
	vercel     *fakeProvider
	gcp        *fakeProvider
	svc        *cloud.Service

	repoID uuid.UUID
	comp   domain.Component
}

func TestBackfillSuite(t *testing.T) {
	suite.Run(t, new(BackfillSuite))
}

func (s *BackfillSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.envs = newFakeEnvironments()
	s.components = newFakeComponents()
	s.deploys = newFakeDeployTargets()
	s.legacy = &fakeLegacy{}
	s.vercel = &fakeProvider{kind: domain.CloudVercel, verifyMeta: map[string]string{"team_slug": "acme"}}
	s.gcp = &fakeProvider{kind: domain.CloudGCP, verifyMeta: map[string]string{"project_id": "proj-1"}}

	s.repoID = uuid.New()
	s.comp = s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "."})

	s.svc = cloud.NewService(cloud.Deps{
		Accounts:      s.accounts,
		Environments:  s.envs,
		Providers:     []port.CloudProvider{s.vercel, s.gcp},
		Components:    s.components,
		Scans:         newFakeScans(),
		Repos:         newFakeRepos(domain.Repository{ID: s.repoID, Name: "acme"}),
		DeployTargets: s.deploys,
		Legacy:        s.legacy,
	})
}

func (s *BackfillSuite) TestBackfillCreatesVercelAccountFromLegacyToken() {
	s.legacy.hasVercel = true
	s.legacy.vercelCred = domain.LegacyVercelCredential{Token: "legacy-token", TeamID: "team_1"}

	s.svc.Boot(s.ctx)
	s.Require().Eventually(func() bool {
		accounts, _ := s.svc.ListAccounts(s.ctx)
		return len(accounts) == 1
	}, 2*time.Second, 10*time.Millisecond)

	accounts, err := s.svc.ListAccounts(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(accounts, 1)
	s.Equal(domain.CloudVercel, accounts[0].Provider)
	s.Equal(domain.CloudAccountOK, accounts[0].Status)
	s.Equal("acme", accounts[0].Label)
}

func (s *BackfillSuite) TestBackfillStoresUnverifiedAccountOnVerifyFailure() {
	s.legacy.hasVercel = true
	s.legacy.vercelCred = domain.LegacyVercelCredential{Token: "bad-token"}
	s.vercel.verifyErr = context.DeadlineExceeded

	s.svc.Boot(s.ctx)
	s.Require().Eventually(func() bool {
		accounts, _ := s.svc.ListAccounts(s.ctx)
		return len(accounts) == 1
	}, 2*time.Second, 10*time.Millisecond)

	accounts, err := s.svc.ListAccounts(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(accounts, 1)
	s.Equal(domain.CloudAccountUnverified, accounts[0].Status)
	s.NotEmpty(accounts[0].StatusDetail)
}

func (s *BackfillSuite) TestBackfillSkipsAccountCreationWhenOneAlreadyExists() {
	_, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "existing"})
	s.Require().NoError(err)

	s.legacy.hasVercel = true
	s.legacy.vercelCred = domain.LegacyVercelCredential{Token: "legacy-token"}

	s.svc.Boot(s.ctx)
	time.Sleep(50 * time.Millisecond)

	accounts, err := s.svc.ListAccounts(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(accounts, 1, "an existing vercel account must never be duplicated by the backfill")
}

func (s *BackfillSuite) TestBackfillEnvironmentFromVercelProjectLink() {
	_, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)

	s.legacy.projectLinks = []domain.LegacyVercelProjectLink{
		{RepositoryID: s.repoID, SubProjectPath: "", ProjectID: "prj_1", ProjectName: "acme", ProductionURL: "https://acme.vercel.app"},
	}

	s.svc.Boot(s.ctx)
	s.Require().Eventually(func() bool {
		envs, _ := s.svc.ListEnvironments(s.ctx, s.repoID)
		return len(envs) == 1
	}, 2*time.Second, 10*time.Millisecond)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	e := envs[0]
	s.Equal(domain.EnvironmentProduction, e.Environment)
	s.Equal(domain.LinkConfirmed, e.Status)
	s.Equal(domain.LinkSourceUser, e.Source)
	s.Equal("https://acme.vercel.app", e.URL)
	s.Require().NotNil(e.Resource)
	s.Equal("prj_1", e.Resource.ID)
}

func (s *BackfillSuite) TestBackfillIsIdempotentAcrossTwoBoots() {
	_, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)
	s.legacy.projectLinks = []domain.LegacyVercelProjectLink{
		{RepositoryID: s.repoID, ProjectID: "prj_1", ProjectName: "acme", ProductionURL: "https://acme.vercel.app"},
	}

	s.svc.Boot(s.ctx)
	s.Require().Eventually(func() bool {
		envs, _ := s.svc.ListEnvironments(s.ctx, s.repoID)
		return len(envs) == 1
	}, 2*time.Second, 10*time.Millisecond)

	s.svc.Boot(s.ctx)
	time.Sleep(50 * time.Millisecond)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Len(envs, 1, "a second boot must not duplicate an environment already backfilled")
}

func (s *BackfillSuite) TestBackfillSkipsComponentEnvPairThatAlreadyHasARow() {
	s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction, Status: domain.LinkSuggested})
	s.legacy.projectLinks = []domain.LegacyVercelProjectLink{
		{RepositoryID: s.repoID, ProjectID: "prj_1", ProductionURL: "https://acme.vercel.app"},
	}

	s.svc.Boot(s.ctx)
	time.Sleep(50 * time.Millisecond)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	s.Equal(domain.LinkSuggested, envs[0].Status, "an existing row is never overwritten by the backfill")
}

func (s *BackfillSuite) TestBackfillEnvironmentFromDeployTargetBecomesCustom() {
	s.deploys.seed(domain.DeployTarget{RepositoryID: s.repoID, Env: domain.DeployEnvProd, BaseURL: "https://legacy-target.example.com"})

	s.svc.Boot(s.ctx)
	s.Require().Eventually(func() bool {
		envs, _ := s.svc.ListEnvironments(s.ctx, s.repoID)
		return len(envs) == 1
	}, 2*time.Second, 10*time.Millisecond)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	e := envs[0]
	s.Equal("https://legacy-target.example.com", e.URL)
	s.Nil(e.AccountID)
	s.Empty(e.Provider)
	s.Equal(domain.LinkSourceUser, e.Source)
}

func (s *BackfillSuite) TestBackfillIgnoresPreprodAndLocalDeployTargets() {
	s.deploys.seed(domain.DeployTarget{RepositoryID: s.repoID, Env: domain.DeployEnvPreProd, BaseURL: "https://preprod.example.com"})
	s.deploys.seed(domain.DeployTarget{RepositoryID: s.repoID, Env: domain.DeployEnvLocal, BaseURL: "https://local.example.com"})

	s.svc.Boot(s.ctx)
	time.Sleep(50 * time.Millisecond)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Empty(envs)
}
