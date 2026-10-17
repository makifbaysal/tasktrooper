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

type FixTaskSuite struct {
	suite.Suite
	ctx      context.Context
	accounts *fakeAccounts
	envs     *fakeEnvironments
	provider *fakeProvider
	tasks    *fakeTasks
	comps    *fakeComponents
	svc      *cloud.Service

	repoID      uuid.UUID
	componentID uuid.UUID
	env         domain.ComponentEnvironment
}

func TestFixTaskSuite(t *testing.T) {
	suite.Run(t, new(FixTaskSuite))
}

func (s *FixTaskSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.envs = newFakeEnvironments()
	s.provider = &fakeProvider{kind: domain.CloudVercel}
	s.tasks = &fakeTasks{}
	s.repoID = uuid.New()
	s.componentID = uuid.New()
	s.comps = newFakeComponents()

	s.svc = cloud.NewService(cloud.Deps{
		Accounts:     s.accounts,
		Environments: s.envs,
		Providers:    []port.CloudProvider{s.provider},
		Components:   s.comps,
		Scans:        newFakeScans(),
		Repos:        newFakeRepos(domain.Repository{ID: s.repoID, Name: "acme"}),
		Tasks:        s.tasks,
	})

	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1", Name: "api"}
	s.env = s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.componentID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel, AccountID: &acct.ID, Resource: &ref,
	})
}

func (s *FixTaskSuite) TestCreateFixTaskOpensABugTaskWithComponentAndSample() {
	s.provider.detail = domain.CloudResourceDetail{ConsoleURL: "https://vercel.com/console/prj_1"}
	s.provider.logs = domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{
		{Timestamp: time.Now(), Severity: domain.LogError, Message: "nil pointer dereference"},
	}}

	group := domain.RuntimeErrorGroup{
		Message:   "nil pointer dereference in handler",
		Count:     7,
		FirstSeen: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		Sample:    "panic: nil pointer dereference\n  at handler.go:42",
	}

	task, err := s.svc.CreateFixTask(s.ctx, s.env.ID, group)
	s.Require().NoError(err)
	s.Equal(1, s.tasks.callCount())

	req, repoID := s.tasks.lastCall()
	s.Equal(s.repoID, repoID)
	s.Equal(domain.TaskTypeBug, req.TaskType)
	s.Equal("Fix: nil pointer dereference in handler", req.Title)
	s.Require().NotNil(req.ComponentID)
	s.Equal(s.componentID, *req.ComponentID)
	s.Contains(req.Description, "production")
	s.Contains(req.Description, "vercel")
	s.Contains(req.Description, "Count: 7")
	s.Contains(req.Description, "panic: nil pointer dereference")
	s.Contains(req.Description, "https://vercel.com/console/prj_1")
	s.Contains(req.Description, "nil pointer dereference")

	s.Equal(domain.TaskTypeBug, task.TaskType)
}

func (s *FixTaskSuite) TestCreateFixTaskTruncatesTitleTo80Chars() {
	longMessage := ""
	for i := 0; i < 200; i++ {
		longMessage += "x"
	}
	group := domain.RuntimeErrorGroup{Message: longMessage, Sample: longMessage}

	_, err := s.svc.CreateFixTask(s.ctx, s.env.ID, group)
	s.Require().NoError(err)
	req, _ := s.tasks.lastCall()
	s.LessOrEqual(len([]rune(req.Title)), len("Fix: ")+80)
}

func (s *FixTaskSuite) TestCreateFixTaskRequiresTaskCreator() {
	svc := cloud.NewService(cloud.Deps{
		Accounts:     s.accounts,
		Environments: s.envs,
		Providers:    []port.CloudProvider{s.provider},
		Components:   newFakeComponents(),
		Scans:        newFakeScans(),
		Repos:        newFakeRepos(domain.Repository{ID: s.repoID, Name: "acme"}),
	})
	_, err := svc.CreateFixTask(s.ctx, s.env.ID, domain.RuntimeErrorGroup{Message: "x"})
	s.Require().Error(err)
}

func (s *FixTaskSuite) TestABoundGroupOpensTheSameTaskInTheComponentsRepository() {
	comp := s.comps.seed(domain.Component{ID: uuid.New(), RepositoryID: s.repoID, Status: domain.ComponentStatusActive})
	group := domain.RuntimeErrorGroup{
		Fingerprint: "101", Message: "NSInvalidArgumentException\nmore", Count: 250, Sample: "unrecognized selector",
		ExternalURL: "https://acme.sentry.io/issues/101/",
	}

	_, err := s.svc.CreateBoundFixTask(s.ctx, comp.ID, cloud.BoundErrorSource{
		Environment: domain.EnvironmentProduction, Name: "Sentry prod", Kind: domain.DataSourceKindSentry,
	}, group)

	s.Require().NoError(err)
	req, repoID := s.tasks.lastCall()
	s.Equal(s.repoID, repoID)
	s.Equal(domain.TaskTypeBug, req.TaskType)
	s.Equal("Fix: NSInvalidArgumentException", req.Title)
	s.Require().NotNil(req.ComponentID)
	s.Equal(comp.ID, *req.ComponentID)
	s.Contains(req.Description, "Environment: production")
	s.Contains(req.Description, "Data source: Sentry prod (sentry)")
	s.Contains(req.Description, "Count: 250")
	s.Contains(req.Description, "unrecognized selector")
	s.Contains(req.Description, "(https://acme.sentry.io/issues/101/)")
}

func (s *FixTaskSuite) TestABoundGroupsLinkMustBeAWebPage() {
	comp := s.comps.seed(domain.Component{ID: uuid.New(), RepositoryID: s.repoID, Status: domain.ComponentStatusActive})

	_, err := s.svc.CreateBoundFixTask(s.ctx, comp.ID, cloud.BoundErrorSource{Environment: domain.EnvironmentProduction, Name: "x", Kind: domain.DataSourceKindSentry},
		domain.RuntimeErrorGroup{Message: "boom", ExternalURL: "javascript:alert(1)"})
	s.Require().NoError(err)
	req, _ := s.tasks.lastCall()
	s.NotContains(req.Description, "javascript")

	_, err = s.svc.CreateBoundFixTask(s.ctx, comp.ID, cloud.BoundErrorSource{Environment: "qa"}, domain.RuntimeErrorGroup{Message: "boom"})
	s.ErrorIs(err, cloud.ErrInvalidInput)
	_, err = s.svc.CreateBoundFixTask(s.ctx, uuid.New(), cloud.BoundErrorSource{Environment: domain.EnvironmentProduction}, domain.RuntimeErrorGroup{Message: "boom"})
	s.ErrorIs(err, port.ErrNotFound)
}
