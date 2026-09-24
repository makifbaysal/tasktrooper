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

type MatchSuite struct {
	suite.Suite
	ctx        context.Context
	accounts   *fakeAccounts
	envs       *fakeEnvironments
	components *fakeComponents
	scans      *fakeScans
	repos      *fakeRepos
	deploys    *fakeDeployTargets
	vercel     *fakeProvider
	gcp        *fakeProvider
	aws        *fakeProvider
	svc        *cloud.Service

	repoID uuid.UUID
	comp   domain.Component
}

func TestMatchSuite(t *testing.T) {
	suite.Run(t, new(MatchSuite))
}

func (s *MatchSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.envs = newFakeEnvironments()
	s.components = newFakeComponents()
	s.scans = newFakeScans()
	s.deploys = newFakeDeployTargets()
	s.vercel = &fakeProvider{kind: domain.CloudVercel}
	s.gcp = &fakeProvider{kind: domain.CloudGCP}
	s.aws = &fakeProvider{kind: domain.CloudAWS}

	s.repoID = uuid.New()
	repo := domain.Repository{ID: s.repoID, Name: "acme-app"}
	s.repos = newFakeRepos(repo)
	s.comp = s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "."})

	s.svc = cloud.NewService(cloud.Deps{
		Accounts:      s.accounts,
		Environments:  s.envs,
		Providers:     []port.CloudProvider{s.vercel, s.gcp, s.aws},
		Components:    s.components,
		Scans:         s.scans,
		Repos:         s.repos,
		DeployTargets: s.deploys,
	})
}

func (s *MatchSuite) createAccount(provider domain.CloudProviderKind) domain.CloudAccount {
	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: provider, Status: domain.CloudAccountOK}, map[string]string{"x": "y"})
	s.Require().NoError(err)
	return acct
}

func (s *MatchSuite) scanResult(signals ...domain.DeploySignal) domain.ScanResult {
	return domain.ScanResult{
		Components:    []domain.DetectedComponent{{Path: "."}},
		DeploySignals: signals,
		Git:           domain.ScanGit{RemoteSlug: "acme/app"},
	}
}

func (s *MatchSuite) TestVercelExactProjectIDAutoConfirms() {
	acct := s.createAccount(domain.CloudVercel)
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Provider: domain.CloudVercel, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1", Name: "acme-app"}, URL: "https://acme.vercel.app"},
	}

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{
		ComponentPath: ".", Provider: "vercel", Ref: map[string]string{"project_id": "prj_1"},
	}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	e := envs[0]
	s.Equal(domain.LinkConfirmed, e.Status)
	s.True(e.AutoConfirmed)
	s.Equal(domain.ConfidenceExact, e.Confidence)
	s.Require().NotNil(e.AccountID)
	s.Equal(acct.ID, *e.AccountID)
	s.Equal("https://acme.vercel.app", e.URL)
	s.Empty(e.Candidates)
}

func (s *MatchSuite) TestVercelMultipleNameMatchesSuggestWithCandidates() {
	acct := s.createAccount(domain.CloudVercel)
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Provider: domain.CloudVercel, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_a", Name: "acme-app"}},
		{AccountID: acct.ID, Provider: domain.CloudVercel, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_b", Name: "acme-app"}},
	}

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{ComponentPath: ".", Provider: "vercel"}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	e := envs[0]
	s.Equal(domain.LinkSuggested, e.Status)
	s.Equal(domain.ConfidenceMedium, e.Confidence)
	s.False(e.AutoConfirmed)
	s.Len(e.Candidates, 2)
	s.Nil(e.AccountID)
}

func (s *MatchSuite) TestNoAccountSuggestsWithReason() {
	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{ComponentPath: ".", Provider: "vercel"}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	e := envs[0]
	s.Equal(domain.LinkSuggested, e.Status)
	s.Nil(e.AccountID)
	s.Equal(domain.CloudVercel, e.Provider)
	s.Contains(e.Reason, "connect a")
	s.Empty(e.Candidates)
}

func (s *MatchSuite) TestGCPExactServiceAndRegion() {
	acct := s.createAccount(domain.CloudGCP)
	s.gcp.resources = []domain.CloudResource{
		{AccountID: acct.ID, Provider: domain.CloudGCP, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "svc-1", Name: "api", Region: "us-central1"}},
		{AccountID: acct.ID, Provider: domain.CloudGCP, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "svc-2", Name: "api", Region: "europe-west1"}},
	}

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{
		ComponentPath: ".", Provider: "gcp_cloud_run", Ref: map[string]string{"service": "api", "region": "us-central1"},
	}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	e := envs[0]
	s.Equal(domain.LinkConfirmed, e.Status)
	s.True(e.AutoConfirmed)
	s.Require().NotNil(e.Resource)
	s.Equal("svc-1", e.Resource.ID)
}

func (s *MatchSuite) TestAWSExactServiceWithCluster() {
	acct := s.createAccount(domain.CloudAWS)
	s.aws.resources = []domain.CloudResource{
		{AccountID: acct.ID, Provider: domain.CloudAWS, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceECSService, ID: "arn:1", Name: "api", Extra: map[string]string{"cluster": "prod"}}},
		{AccountID: acct.ID, Provider: domain.CloudAWS, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceECSService, ID: "arn:2", Name: "api", Extra: map[string]string{"cluster": "staging"}}},
	}

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{
		ComponentPath: ".", Provider: "aws_ecs", Ref: map[string]string{"service": "api", "cluster": "prod"},
	}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	s.Equal("arn:1", envs[0].Resource.ID)
	s.True(envs[0].AutoConfirmed)
}

func (s *MatchSuite) TestUserSourcedRowIsNeverTouched() {
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Source: domain.LinkSourceUser, URL: "https://human-set.example.com",
	})

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{ComponentPath: ".", Provider: "vercel"}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	s.Equal("https://human-set.example.com", envs[0].URL)
	s.Equal(domain.LinkSourceUser, envs[0].Source)
}

func (s *MatchSuite) TestHumanConfirmedNonAutoRowIsNeverTouched() {
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Source: domain.LinkSourceScan, AutoConfirmed: false, URL: "https://human-confirmed.example.com",
	})

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{ComponentPath: ".", Provider: "vercel"}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Equal("https://human-confirmed.example.com", envs[0].URL)
}

func (s *MatchSuite) TestDismissedRowIsNeverTouched() {
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkDismissed, Source: domain.LinkSourceScan,
	})

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{ComponentPath: ".", Provider: "vercel"}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Equal(domain.LinkDismissed, envs[0].Status)
}

func (s *MatchSuite) TestAutoConfirmedRowIsReconsideredOnRematch() {
	acct := s.createAccount(domain.CloudVercel)
	s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Source: domain.LinkSourceScan, AutoConfirmed: true,
		AccountID: &acct.ID, Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_old"},
	})
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Provider: domain.CloudVercel, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_new", Name: "acme-app"}, URL: "https://new.vercel.app"},
	}

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{
		ComponentPath: ".", Provider: "vercel", Ref: map[string]string{"project_id": "prj_new"},
	}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Equal("prj_new", envs[0].Resource.ID)
}

func (s *MatchSuite) TestUnsupportedProviderRecordsNothing() {
	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{ComponentPath: ".", Provider: "fly"}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Empty(envs)
}

func (s *MatchSuite) TestUnknownComponentPathIsSkipped() {
	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{ComponentPath: "missing", Provider: "vercel"}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Empty(envs)
}

func (s *MatchSuite) TestNoDeploySignalsIsANoOp() {
	err := s.svc.MatchScan(s.ctx, s.repoID, domain.ScanResult{})
	s.Require().NoError(err)
}

func (s *MatchSuite) TestMissingEnvironmentDefaultsToProduction() {
	acct := s.createAccount(domain.CloudVercel)
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Provider: domain.CloudVercel, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}},
	}
	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{
		ComponentPath: ".", Provider: "vercel", Ref: map[string]string{"project_id": "prj_1"},
	}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	s.Equal(domain.EnvironmentProduction, envs[0].Environment)
}
