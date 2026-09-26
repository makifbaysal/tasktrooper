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

type EnvironmentsSuite struct {
	suite.Suite
	ctx        context.Context
	accounts   *fakeAccounts
	envs       *fakeEnvironments
	components *fakeComponents
	deploys    *fakeDeployTargets
	vercel     *fakeProvider
	delivery   *fakeDeliveryRefresher
	svc        *cloud.Service

	repoID uuid.UUID
	comp   domain.Component
}

func TestEnvironmentsSuite(t *testing.T) {
	suite.Run(t, new(EnvironmentsSuite))
}

func (s *EnvironmentsSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.envs = newFakeEnvironments()
	s.components = newFakeComponents()
	s.deploys = newFakeDeployTargets()
	s.vercel = &fakeProvider{kind: domain.CloudVercel}
	s.delivery = &fakeDeliveryRefresher{}

	s.repoID = uuid.New()
	s.comp = s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "."})

	s.svc = cloud.NewService(cloud.Deps{
		Accounts:      s.accounts,
		Environments:  s.envs,
		Providers:     []port.CloudProvider{s.vercel},
		Components:    s.components,
		Scans:         newFakeScans(),
		Repos:         newFakeRepos(domain.Repository{ID: s.repoID, Name: "acme"}),
		DeployTargets: s.deploys,
	})
	s.svc.SetDeliveryRefresher(s.delivery)
}

func (s *EnvironmentsSuite) createAccount() domain.CloudAccount {
	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)
	return acct
}

func (s *EnvironmentsSuite) TestBindEnvironmentRejectsUnknownEnv() {
	_, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, "canary", domain.SaveEnvironmentRequest{URL: "https://x.example.com"})
	s.ErrorIs(err, cloud.ErrInvalidInput)
}

func (s *EnvironmentsSuite) TestBindEnvironmentRejectsInactiveComponent() {
	dismissed := s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "b", Status: domain.ComponentStatusDismissed})
	_, err := s.svc.BindEnvironment(s.ctx, dismissed.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{URL: "https://x.example.com"})
	s.ErrorIs(err, cloud.ErrInvalidInput)
}

func (s *EnvironmentsSuite) TestBindEnvironmentCustomRequiresURL() {
	_, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{})
	s.ErrorIs(err, cloud.ErrInvalidInput)
}

func (s *EnvironmentsSuite) TestBindEnvironmentCustomSucceeds() {
	e, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentStaging, domain.SaveEnvironmentRequest{URL: "https://stage.example.com"})
	s.Require().NoError(err)
	s.Equal(domain.LinkConfirmed, e.Status)
	s.Equal(domain.LinkSourceUser, e.Source)
	s.Equal(domain.ConfidenceExact, e.Confidence)
	s.Nil(e.AccountID)
	s.Equal("https://stage.example.com", e.URL)
}

func (s *EnvironmentsSuite) TestBindEnvironmentWithAccountRequiresResource() {
	acct := s.createAccount()
	_, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{AccountID: &acct.ID})
	s.ErrorIs(err, cloud.ErrInvalidInput)
}

func (s *EnvironmentsSuite) TestBindEnvironmentWithAccountDefaultsURLFromListing() {
	acct := s.createAccount()
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}, URL: "https://from-listing.example.com"},
	}
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}

	e, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{AccountID: &acct.ID, Resource: &ref})
	s.Require().NoError(err)
	s.Equal("https://from-listing.example.com", e.URL)
	s.Equal(domain.CloudVercel, e.Provider)
}

func (s *EnvironmentsSuite) TestBindEnvironmentWithAccountExplicitURLWins() {
	acct := s.createAccount()
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}, URL: "https://from-listing.example.com"},
	}
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}

	e, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{AccountID: &acct.ID, Resource: &ref, URL: "https://explicit.example.com"})
	s.Require().NoError(err)
	s.Equal("https://explicit.example.com", e.URL)
}

func (s *EnvironmentsSuite) TestPatchEnvironmentDismiss() {
	seeded := s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction, Status: domain.LinkSuggested})
	dismissed := domain.LinkDismissed
	e, err := s.svc.PatchEnvironment(s.ctx, seeded.ID, cloud.EnvironmentPatch{Status: &dismissed})
	s.Require().NoError(err)
	s.Equal(domain.LinkDismissed, e.Status)
	s.False(e.AutoConfirmed)
}

func (s *EnvironmentsSuite) TestPatchEnvironmentPicksCandidate() {
	acct := s.createAccount()
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_2", Name: "two"}, URL: "https://two.example.com"},
	}
	candidates := []domain.CloudResource{
		{AccountID: acct.ID, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1", Name: "one"}},
		{AccountID: acct.ID, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_2", Name: "two"}},
	}
	seeded := s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkSuggested, Source: domain.LinkSourceScan, Candidates: candidates,
	})

	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_2", Name: "two"}
	e, err := s.svc.PatchEnvironment(s.ctx, seeded.ID, cloud.EnvironmentPatch{AccountID: &acct.ID, Resource: &ref})
	s.Require().NoError(err)
	s.Equal(domain.LinkConfirmed, e.Status)
	s.Equal("https://two.example.com", e.URL)
	s.Empty(e.Candidates)
	s.False(e.AutoConfirmed)
	s.Equal(domain.LinkSourceScan, e.Source, "picking a candidate is not the same as a human-created custom binding")
}

func (s *EnvironmentsSuite) TestDeleteEnvironmentUserRowIsHardDeleted() {
	seeded := s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction, Source: domain.LinkSourceUser, URL: "https://x.example.com"})
	s.Require().NoError(s.svc.DeleteEnvironment(s.ctx, seeded.ID))

	_, err := s.envs.GetEnvironment(s.ctx, seeded.ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *EnvironmentsSuite) TestDeleteEnvironmentScanRowIsDismissed() {
	seeded := s.envs.seed(domain.ComponentEnvironment{RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction, Source: domain.LinkSourceScan, Status: domain.LinkConfirmed})
	s.Require().NoError(s.svc.DeleteEnvironment(s.ctx, seeded.ID))

	after, err := s.envs.GetEnvironment(s.ctx, seeded.ID)
	s.Require().NoError(err)
	s.Equal(domain.LinkDismissed, after.Status)
}

func (s *EnvironmentsSuite) TestListEnvironmentsNeverReturnsNil() {
	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.NotNil(envs)
}

func (s *EnvironmentsSuite) TestBindEnvironmentProductionVercelAlignsDelivery() {
	acct := s.createAccount()
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}

	_, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{AccountID: &acct.ID, Resource: &ref})
	s.Require().NoError(err)

	s.Require().Equal(1, s.delivery.alignCallCount())
	call := s.delivery.lastAlignCall()
	s.Equal(s.comp.ID, call.componentID)
	s.Equal(domain.CloudVercel, call.provider)
}

func (s *EnvironmentsSuite) TestBindEnvironmentStagingVercelDoesNotAlignDelivery() {
	acct := s.createAccount()
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}

	_, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentStaging, domain.SaveEnvironmentRequest{AccountID: &acct.ID, Resource: &ref})
	s.Require().NoError(err)

	s.Equal(0, s.delivery.alignCallCount())
}

func (s *EnvironmentsSuite) TestBindEnvironmentProductionCustomURLDoesNotAlignDelivery() {
	_, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentProduction, domain.SaveEnvironmentRequest{URL: "https://prod.example.com"})
	s.Require().NoError(err)

	s.Equal(0, s.delivery.alignCallCount())
}

func (s *EnvironmentsSuite) TestPatchEnvironmentConfirmingSuggestedProductionVercelAlignsDelivery() {
	acct := s.createAccount()
	seeded := s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Provider: domain.CloudVercel, AccountID: &acct.ID, Status: domain.LinkSuggested, Source: domain.LinkSourceScan,
	})

	confirmed := domain.LinkConfirmed
	_, err := s.svc.PatchEnvironment(s.ctx, seeded.ID, cloud.EnvironmentPatch{Status: &confirmed})
	s.Require().NoError(err)

	s.Require().Equal(1, s.delivery.alignCallCount())
	call := s.delivery.lastAlignCall()
	s.Equal(s.comp.ID, call.componentID)
	s.Equal(domain.CloudVercel, call.provider)
}

func (s *EnvironmentsSuite) TestPatchEnvironmentDismissingDoesNotAlignDelivery() {
	seeded := s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Provider: domain.CloudVercel, Status: domain.LinkSuggested, Source: domain.LinkSourceScan,
	})

	dismissed := domain.LinkDismissed
	_, err := s.svc.PatchEnvironment(s.ctx, seeded.ID, cloud.EnvironmentPatch{Status: &dismissed})
	s.Require().NoError(err)

	s.Equal(0, s.delivery.alignCallCount())
}
