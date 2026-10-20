package cloud_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type RuntimeSuite struct {
	suite.Suite
	ctx      context.Context
	accounts *fakeAccounts
	envs     *fakeEnvironments
	provider *fakeProvider
	svc      *cloud.Service

	repoID uuid.UUID
	acct   domain.CloudAccount
	env    domain.ComponentEnvironment
}

func TestRuntimeSuite(t *testing.T) {
	suite.Run(t, new(RuntimeSuite))
}

func (s *RuntimeSuite) SetupTest() {
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
	s.acct = acct

	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}
	s.env = s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel, AccountID: &s.acct.ID, Resource: &ref,
	})
}

func (s *RuntimeSuite) TestOverviewUnboundEnvironment() {
	unbound := s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentStaging, URL: "https://custom.example.com"})
	out, err := s.svc.Overview(s.ctx, unbound.ID)
	s.Require().NoError(err)
	s.Equal("not connected to a cloud account", out.Unavailable)
	s.Equal("not_connected", out.UnavailableCode)
	s.Nil(out.Detail)
}

func (s *RuntimeSuite) TestOverviewProviderErrorSetsUnavailableCode() {
	s.provider.detailErr = context.DeadlineExceeded

	out, err := s.svc.Overview(s.ctx, s.env.ID)
	s.Require().NoError(err)
	s.Equal("provider_error", out.UnavailableCode)
}

func (s *RuntimeSuite) TestOverviewReturnsDetailDeploymentsAndErrors() {
	s.provider.detail = domain.CloudResourceDetail{Status: domain.CloudStatusHealthy}
	s.provider.deployments = []domain.CloudDeployment{{ID: "d1"}}
	s.provider.errorGroups = []domain.RuntimeErrorGroup{{Fingerprint: "f1", Count: 3}}

	out, err := s.svc.Overview(s.ctx, s.env.ID)
	s.Require().NoError(err)
	s.Require().NotNil(out.Detail)
	s.Equal(domain.CloudStatusHealthy, out.Detail.Status)
	s.Len(out.Deployments, 1)
	s.Len(out.ErrorsLast, 1)
}

func (s *RuntimeSuite) TestOverviewProviderAuthErrorMarksAccountAndReportsUnavailable() {
	s.provider.detailErr = port.ErrCloudAuth

	out, err := s.svc.Overview(s.ctx, s.env.ID)
	s.Require().NoError(err)
	s.Contains(out.Unavailable, "reconnecting")
	s.Equal("cloud_auth", out.UnavailableCode)

	acct, err := s.svc.GetAccount(s.ctx, s.acct.ID)
	s.Require().NoError(err)
	s.Equal(domain.CloudAccountError, acct.Status)
}

func (s *RuntimeSuite) TestLogsAppliesDefaultsAndClamps() {
	s.provider.logs = domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{{Message: "hi"}}}

	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.svc.SetClock(func() time.Time { return fixedNow })

	page, err := s.svc.Logs(s.ctx, s.env.ID, domain.RuntimeLogQuery{Limit: 5000})
	s.Require().NoError(err)
	s.Len(page.Entries, 1)
}

func (s *RuntimeSuite) TestLogsCachesWithinTTL() {
	s.provider.logs = domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{{Message: "hi"}}}

	q := domain.RuntimeLogQuery{Since: time.Now().Add(-time.Hour), Until: time.Now()}
	_, err := s.svc.Logs(s.ctx, s.env.ID, q)
	s.Require().NoError(err)

	s.provider.logsErr = context.Canceled // if a second live call happened, this would surface as an error
	_, err = s.svc.Logs(s.ctx, s.env.ID, q)
	s.Require().NoError(err, "the second call within the cache TTL must not hit the provider again")
}

func (s *RuntimeSuite) TestErrorsFallsBackToGroupingWhenUnsupported() {
	s.provider.errorsErr = port.ErrUnsupported
	since := time.Now().Add(-time.Hour)
	s.provider.logs = domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{
		{Timestamp: since.Add(time.Minute), Severity: domain.LogError, Message: "database timeout"},
		{Timestamp: since.Add(2 * time.Minute), Severity: domain.LogError, Message: "out of memory"},
	}}

	groups, err := s.svc.Errors(s.ctx, s.env.ID, since)
	s.Require().NoError(err)
	s.Len(groups, 2)
}

func (s *RuntimeSuite) TestErrorsPropagatesNonUnsupportedProviderError() {
	s.provider.errorsErr = context.DeadlineExceeded
	_, err := s.svc.Errors(s.ctx, s.env.ID, time.Now().Add(-time.Hour))
	s.Require().Error(err)
}

func (s *RuntimeSuite) TestErrorsCachesAProviderFailureButNotARunOutDeadline() {
	tests := []struct {
		name        string
		firstErr    error
		secondFails bool
	}{
		{name: "deadline", firstErr: context.DeadlineExceeded, secondFails: false},
		{name: "cancelled", firstErr: context.Canceled, secondFails: false},
		{name: "provider failure", firstErr: errors.New("upstream 503"), secondFails: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			env := s.envs.seed(domain.ComponentEnvironment{
				RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentProduction,
				Status: domain.LinkConfirmed, Provider: domain.CloudVercel, AccountID: s.env.AccountID, Resource: s.env.Resource,
			})
			since := time.Now().Add(-time.Hour)
			s.provider.errorsErr = tt.firstErr
			_, err := s.svc.Errors(s.ctx, env.ID, since)
			s.Require().ErrorIs(err, tt.firstErr)

			s.provider.errorsErr = nil
			s.provider.errorGroups = []domain.RuntimeErrorGroup{{Fingerprint: "f1"}}
			groups, err := s.svc.Errors(s.ctx, env.ID, since)
			if tt.secondFails {
				s.Error(err)
				return
			}
			s.Require().NoError(err)
			s.Len(groups, 1)
		})
	}
}

func (s *RuntimeSuite) TestErrorsSupportedIsFalseForAPerBranchPreview() {
	preview := s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentPreview,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel, AccountID: s.env.AccountID, Resource: s.env.Resource,
	})
	s.False(s.svc.ErrorsSupported(preview))
	s.True(s.svc.ErrorsSupported(s.env))
}

func (s *RuntimeSuite) TestDeploymentsDefaultsLimit() {
	s.provider.deployments = []domain.CloudDeployment{{ID: "d1"}, {ID: "d2"}}
	got, err := s.svc.Deployments(s.ctx, s.env.ID, 0)
	s.Require().NoError(err)
	s.Len(got, 2)
}

func (s *RuntimeSuite) TestUnboundLogsAndErrorsReturnNotConnected() {
	unbound := s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentStaging})

	_, err := s.svc.Logs(s.ctx, unbound.ID, domain.RuntimeLogQuery{})
	s.ErrorIs(err, cloud.ErrNotConnected)

	_, err = s.svc.Errors(s.ctx, unbound.ID, time.Now())
	s.ErrorIs(err, cloud.ErrNotConnected)

	_, err = s.svc.Deployments(s.ctx, unbound.ID, 10)
	s.ErrorIs(err, cloud.ErrNotConnected)
}

func (s *RuntimeSuite) analyticsService(p port.CloudProvider) *cloud.Service {
	return cloud.NewService(cloud.Deps{
		Accounts:     s.accounts,
		Environments: s.envs,
		Providers:    []port.CloudProvider{p},
		Components:   newFakeComponents(),
		Scans:        newFakeScans(),
		Repos:        newFakeRepos(domain.Repository{ID: s.repoID, Name: "acme"}),
	})
}

func (s *RuntimeSuite) TestAnalyticsSaysWhichFeaturesTheProviderMeasures() {
	off := domain.CloudAnalytics{WebAnalytics: domain.AnalyticsNotEnabled, SpeedInsights: domain.AnalyticsEnabled}
	svc := s.analyticsService(&analyticsProvider{fakeProvider: s.provider, state: off})

	got, ok := svc.Analytics(s.ctx, s.env.ID)

	s.True(ok)
	s.Equal(off, got)
}

func (s *RuntimeSuite) TestAnalyticsIsAbsentRatherThanAFailureWhenItCannotBeRead() {
	s.False(s.analyticsStatusOf(s.svc), "a provider without the capability")

	failing := s.analyticsService(&analyticsProvider{fakeProvider: s.provider, err: errors.New("vercel is down")})
	_, ok := failing.Analytics(s.ctx, s.env.ID)
	s.False(ok)

	_, ok = failing.Analytics(s.ctx, uuid.New())
	s.False(ok, "an environment that does not exist")
}

func (s *RuntimeSuite) analyticsStatusOf(svc *cloud.Service) bool {
	_, ok := svc.Analytics(s.ctx, s.env.ID)
	return ok
}
