package cloud_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ResourcesSuite struct {
	suite.Suite
	ctx      context.Context
	accounts *fakeAccounts
	provider *fakeProvider
	svc      *cloud.Service
}

func TestResourcesSuite(t *testing.T) {
	suite.Run(t, new(ResourcesSuite))
}

func (s *ResourcesSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.provider = &fakeProvider{kind: domain.CloudVercel, resources: []domain.CloudResource{
		{Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}},
	}}
	s.svc = cloud.NewService(cloud.Deps{
		Accounts:     s.accounts,
		Environments: newFakeEnvironments(),
		Providers:    []port.CloudProvider{s.provider},
		Components:   newFakeComponents(),
		Scans:        newFakeScans(),
		Repos:        newFakeRepos(),
	})
}

func (s *ResourcesSuite) TestListResourcesCachesWithinTTL() {
	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)

	first, err := s.svc.ListResources(s.ctx, acct.ID, false)
	s.Require().NoError(err)
	s.Len(first, 1)

	s.provider.resources = nil
	s.provider.resourcesErr = context.Canceled

	second, err := s.svc.ListResources(s.ctx, acct.ID, false)
	s.Require().NoError(err, "a cache hit must not call the provider again")
	s.Len(second, 1)
}

func (s *ResourcesSuite) TestListResourcesRefreshBypassesCache() {
	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)

	_, err = s.svc.ListResources(s.ctx, acct.ID, false)
	s.Require().NoError(err)

	s.provider.resources = []domain.CloudResource{
		{Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}},
		{Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_2"}},
	}
	refreshed, err := s.svc.ListResources(s.ctx, acct.ID, true)
	s.Require().NoError(err)
	s.Len(refreshed, 2)
}

func (s *ResourcesSuite) TestListResourcesCacheExpiresAfterTTL() {
	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)

	start := time.Now()
	s.svc.SetClock(func() time.Time { return start })
	_, err = s.svc.ListResources(s.ctx, acct.ID, false)
	s.Require().NoError(err)

	s.svc.SetClock(func() time.Time { return start.Add(61 * time.Second) })
	s.provider.resources = []domain.CloudResource{
		{Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}},
		{Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_2"}},
	}
	after, err := s.svc.ListResources(s.ctx, acct.ID, false)
	s.Require().NoError(err)
	s.Len(after, 2, "the cache must be treated as expired once its TTL has passed")
}
