package cloud_test

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *EnvironmentsSuite) TestBindNonProductionVercelEnvironmentKeepsURLEmpty() {
	acct := s.createAccount()
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}, URL: "https://acme.com"},
	}
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}

	for _, env := range []domain.DeployEnvironment{domain.EnvironmentPreview, domain.EnvironmentStaging} {
		e, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, env, domain.SaveEnvironmentRequest{AccountID: &acct.ID, Resource: &ref})
		s.Require().NoError(err)
		s.Empty(e.URL, "%s must not borrow the project's production URL", env)
		s.Equal(env == domain.EnvironmentPreview, e.PerBranch())
	}

	e, err := s.svc.BindEnvironment(s.ctx, s.comp.ID, domain.EnvironmentStaging, domain.SaveEnvironmentRequest{AccountID: &acct.ID, Resource: &ref, URL: "https://staging.acme.com"})
	s.Require().NoError(err)
	s.Equal("https://staging.acme.com", e.URL, "a URL the caller passed is kept")
}

func (s *EnvironmentsSuite) TestPatchCandidateOnPreviewKeepsURLEmpty() {
	acct := s.createAccount()
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}, URL: "https://acme.com"},
	}
	seeded := s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: domain.EnvironmentPreview,
		Provider: domain.CloudVercel, Status: domain.LinkSuggested, URL: "https://acme.com",
	})
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}

	e, err := s.svc.PatchEnvironment(s.ctx, seeded.ID, cloud.EnvironmentPatch{AccountID: &acct.ID, Resource: &ref})
	s.Require().NoError(err)
	s.Empty(e.URL)
}

func (s *MatchSuite) TestVercelPreviewSignalDoesNotTakeTheProductionURL() {
	acct := s.createAccount(domain.CloudVercel)
	s.vercel.resources = []domain.CloudResource{
		{AccountID: acct.ID, Provider: domain.CloudVercel, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1", Name: "acme-app"}, URL: "https://acme.vercel.app"},
	}

	err := s.svc.MatchScan(s.ctx, s.repoID, s.scanResult(domain.DeploySignal{
		ComponentPath: ".", Provider: "vercel", Environment: domain.EnvironmentPreview, Ref: map[string]string{"project_id": "prj_1"},
	}))
	s.Require().NoError(err)

	envs, err := s.svc.ListEnvironments(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(envs, 1)
	s.Equal(domain.LinkConfirmed, envs[0].Status)
	s.Empty(envs[0].URL)
	s.True(envs[0].PerBranch())
}
