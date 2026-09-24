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

type HealthSuite struct {
	suite.Suite
	ctx      context.Context
	accounts *fakeAccounts
	envs     *fakeEnvironments
	provider *fakeProvider
	svc      *cloud.Service

	repoID uuid.UUID
	env    domain.ComponentEnvironment
}

func TestHealthSuite(t *testing.T) {
	suite.Run(t, new(HealthSuite))
}

func (s *HealthSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.envs = newFakeEnvironments()
	s.provider = &fakeProvider{kind: domain.CloudVercel}
	s.repoID = uuid.New()

	s.svc = cloud.NewService(cloud.Deps{
		Accounts:     s.accounts,
		Environments: s.envs,
		Providers:    []port.CloudProvider{s.provider},
		Components:   newFakeComponents(),
		Scans:        newFakeScans(),
		Repos:        newFakeRepos(domain.Repository{ID: s.repoID, Name: "acme"}),
	})

	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}
	s.env = s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel, AccountID: &acct.ID, Resource: &ref,
	})
	s.svc.SetHealthIntervals(time.Hour, 10*time.Millisecond, time.Millisecond)
}

func (s *HealthSuite) TestStartSweepsAfterFirstDelayAndWritesHealth() {
	readyAt := time.Now()
	s.provider.detail = domain.CloudResourceDetail{
		Status:           domain.CloudStatusHealthy,
		StatusDetail:     "all good",
		LatestDeployment: &domain.CloudDeployment{ID: "d1", CreatedAt: readyAt},
	}
	s.provider.errorGroups = []domain.RuntimeErrorGroup{{Fingerprint: "f1", Count: 4}}

	fixedNow := time.Now()
	s.svc.SetClock(func() time.Time { return fixedNow })

	sweepCtx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	s.svc.Start(sweepCtx)

	s.Require().Eventually(func() bool {
		got, err := s.envs.GetEnvironment(s.ctx, s.env.ID)
		return err == nil && got.Health != nil
	}, 500*time.Millisecond, 5*time.Millisecond, "health sweep never wrote a health record within its first-delay window")

	got, err := s.envs.GetEnvironment(s.ctx, s.env.ID)
	s.Require().NoError(err)
	s.Require().NotNil(got.Health)
	s.Equal(domain.CloudStatusHealthy, got.Health.Status)
	s.Equal(4, got.Health.ErrorCount24h)
	s.Require().NotNil(got.Health.LastDeployAt)
	s.True(readyAt.Equal(*got.Health.LastDeployAt))
}

func (s *HealthSuite) TestSweepSkipsUnconfirmedAndUnboundEnvironments() {
	s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentStaging, Status: domain.LinkSuggested})
	custom := s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentStaging, Status: domain.LinkConfirmed, URL: "https://custom.example.com"})

	sweepCtx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	s.svc.Start(sweepCtx)

	s.Require().Eventually(func() bool {
		got, err := s.envs.GetEnvironment(s.ctx, s.env.ID)
		return err == nil && got.Health != nil
	}, 500*time.Millisecond, 5*time.Millisecond)

	after, err := s.envs.GetEnvironment(s.ctx, custom.ID)
	s.Require().NoError(err)
	s.Nil(after.Health, "an unbound custom environment is never resource-probed by the health sweep")
}
