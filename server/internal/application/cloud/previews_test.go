package cloud_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type previewCall struct{ branch, sha string }

// fakePreviewProvider is a Vercel-like fakeProvider: it previews per branch
// and has no error surface.
type fakePreviewProvider struct {
	*fakeProvider

	mu           sync.Mutex
	preview      domain.CloudDeployment
	previewFound bool
	previewErr   error
	previewCalls []previewCall
	access       domain.PreviewAccess
	accessErr    error
}

var (
	_ port.CloudPreviewer    = (*fakePreviewProvider)(nil)
	_ port.CloudErrorSurface = (*fakePreviewProvider)(nil)
)

func (f *fakePreviewProvider) Preview(_ context.Context, _ domain.CloudCredential, _ domain.CloudResourceRef, branch, sha string) (domain.CloudDeployment, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previewCalls = append(f.previewCalls, previewCall{branch, sha})
	return f.preview, f.previewFound, f.previewErr
}

func (f *fakePreviewProvider) PreviewAccess(context.Context, domain.CloudCredential, domain.CloudResourceRef) (domain.PreviewAccess, error) {
	return f.access, f.accessErr
}

func (f *fakePreviewProvider) ErrorsSupported(domain.DeployEnvironment) bool { return false }

func (f *fakePreviewProvider) calls() []previewCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]previewCall(nil), f.previewCalls...)
}

type fakeTaskReader struct {
	tasks map[uuid.UUID]domain.BoardTask
}

func (f *fakeTaskReader) GetTask(_ context.Context, _, taskID uuid.UUID) (domain.BoardTask, error) {
	t, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return t, nil
}

type fakePRHeads struct{ branch, sha string }

func (f *fakePRHeads) PullRequestHead(context.Context, uuid.UUID, uuid.UUID) (string, string, error) {
	return f.branch, f.sha, nil
}

type PreviewSuite struct {
	suite.Suite
	ctx        context.Context
	accounts   *fakeAccounts
	envs       *fakeEnvironments
	components *fakeComponents
	provider   *fakePreviewProvider
	tasks      *fakeTaskReader
	svc        *cloud.Service

	repoID  uuid.UUID
	acct    domain.CloudAccount
	comp    domain.Component
	task    domain.BoardTask
	preview domain.ComponentEnvironment
}

func TestPreviewSuite(t *testing.T) {
	suite.Run(t, new(PreviewSuite))
}

func (s *PreviewSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.envs = newFakeEnvironments()
	s.components = newFakeComponents()
	s.provider = &fakePreviewProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}}
	s.repoID = uuid.New()

	s.svc = cloud.NewService(cloud.Deps{
		Accounts:     s.accounts,
		Environments: s.envs,
		Providers:    []port.CloudProvider{s.provider},
		Components:   s.components,
		Scans:        newFakeScans(),
		Repos:        newFakeRepos(domain.Repository{ID: s.repoID, Name: "acme"}),
	})

	acct, err := s.accounts.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	s.Require().NoError(err)
	s.acct = acct
	s.comp = s.components.seed(domain.Component{RepositoryID: s.repoID, Path: "apps/web"})
	s.preview = s.seedEnv(domain.EnvironmentPreview)

	s.task = domain.BoardTask{ID: uuid.New(), Key: "T-12"}
	s.tasks = &fakeTaskReader{tasks: map[uuid.UUID]domain.BoardTask{s.task.ID: s.task}}
	s.svc.SetTaskPreviewSources(s.tasks, nil)
}

func (s *PreviewSuite) seedEnv(env domain.DeployEnvironment) domain.ComponentEnvironment {
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}
	return s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: s.comp.ID, Environment: env,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel, AccountID: &s.acct.ID, Resource: &ref,
	})
}

func (s *PreviewSuite) TestFoundPreviewPrefersPRHeadAndCarriesAccess() {
	s.svc.SetTaskPreviewSources(s.tasks, &fakePRHeads{branch: "feature/login", sha: "abc1234def"})
	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	ready := created.Add(time.Minute)
	s.provider.previewFound = true
	s.provider.preview = domain.CloudDeployment{
		ID: "dpl_1", Status: domain.CloudDeployReady, URL: "https://web-abc.vercel.app",
		BranchURL: "https://web-git-feature-login-acme.vercel.app", PRNumber: 7, CommitSHA: "abc1234def",
		CreatedAt: created, ReadyAt: &ready, InspectURL: "https://vercel.com/acme/web/dpl_1",
	}
	s.provider.access = domain.PreviewAccess{Protected: true, Mode: domain.PreviewAccessVercelAuth, BypassConfigured: true, BypassSecret: "s3cret"}

	out, err := s.svc.TaskPreviews(s.ctx, s.repoID, s.task.ID)
	s.Require().NoError(err)

	s.Equal([]previewCall{{"feature/login", "abc1234def"}}, s.provider.calls())
	s.Equal("feature/login", out.Branch)
	s.Equal("abc1234def", out.HeadSHA)
	s.Require().Len(out.Previews, 1)
	p := out.Previews[0]
	s.Equal(s.comp.ID, p.ComponentID)
	s.Equal("web", p.ComponentName)
	s.Equal(s.preview.ID, p.EnvironmentID)
	s.Equal(domain.CloudVercel, p.Provider)
	s.Equal(domain.TaskPreviewReady, p.Status)
	s.Equal("https://web-git-feature-login-acme.vercel.app", p.BranchURL)
	s.Equal(7, p.PRNumber)
	s.Equal("2026-09-01T10:00:00Z", p.CreatedAt)
	s.Equal("2026-09-01T10:01:00Z", p.ReadyAt)
	s.True(p.Protected)
	s.True(p.BypassConfigured)
	s.Equal("s3cret", p.BypassSecret)

	raw, err := json.Marshal(out.Previews)
	s.Require().NoError(err)
	s.NotContains(string(raw), "s3cret", "the bypass secret must never be serialized")
	var decoded []map[string]any
	s.Require().NoError(json.Unmarshal(raw, &decoded))
	keys := make([]string, 0, len(decoded[0]))
	for k := range decoded[0] {
		keys = append(keys, k)
	}
	s.ElementsMatch([]string{
		"component_id", "component_name", "environment_id", "provider", "status", "url", "branch_url",
		"pr_number", "commit_sha", "created_at", "ready_at", "inspect_url", "protected", "bypass_configured",
	}, keys)
}

func (s *PreviewSuite) TestNoDeploymentYetIsStatusNoneWithEmptyFields() {
	s.provider.access = domain.PreviewAccess{Protected: true, BypassConfigured: true, BypassSecret: "s3cret"}

	out, err := s.svc.TaskPreviews(s.ctx, s.repoID, s.task.ID)
	s.Require().NoError(err)

	s.Equal([]previewCall{{domain.TaskBranchName(s.task), ""}}, s.provider.calls(), "without a PR head the task branch is looked up alone")
	s.Require().Len(out.Previews, 1)
	p := out.Previews[0]
	s.Equal(domain.TaskPreviewNone, p.Status)
	s.Equal(s.preview.ID, p.EnvironmentID)
	s.Empty(p.URL)
	s.Empty(p.CreatedAt)
	s.Empty(p.BypassSecret)
	s.False(p.Protected)
}

func (s *PreviewSuite) TestNoPerBranchEnvironmentIsAnEmptyList() {
	s.Require().NoError(s.envs.DeleteEnvironment(s.ctx, s.preview.ID))
	s.seedEnv(domain.EnvironmentProduction)

	out, err := s.svc.TaskPreviews(s.ctx, s.repoID, s.task.ID)
	s.Require().NoError(err)
	s.NotNil(out.Previews)
	s.Empty(out.Previews)
	s.Empty(s.provider.calls())
}

func (s *PreviewSuite) TestUnknownTask() {
	_, err := s.svc.TaskPreviews(s.ctx, s.repoID, uuid.New())
	s.ErrorIs(err, domain.ErrBoardTaskNotFound)
}

func (s *PreviewSuite) TestDeploymentsAskForTheEnvironmentsOwnTarget() {
	production := s.seedEnv(domain.EnvironmentProduction)

	_, err := s.svc.Deployments(s.ctx, s.preview.ID, 5)
	s.Require().NoError(err)
	_, err = s.svc.Deployments(s.ctx, production.ID, 5)
	s.Require().NoError(err)

	s.Equal([]domain.DeployEnvironment{domain.EnvironmentPreview, domain.EnvironmentProduction}, s.provider.deploymentsEnvs)
}

func (s *PreviewSuite) TestOverviewOfPerBranchEnvironment() {
	s.provider.detail = domain.CloudResourceDetail{
		CloudResource: domain.CloudResource{URL: "https://acme.com", Domains: []string{"acme.com"}},
		Status:        domain.CloudStatusHealthy,
	}
	s.provider.deployments = []domain.CloudDeployment{{ID: "dpl_new", Status: domain.CloudDeployBuilding}, {ID: "dpl_old", Status: domain.CloudDeployReady}}
	s.provider.errorGroups = []domain.RuntimeErrorGroup{{Fingerprint: "f1", Count: 3}}
	s.provider.access = domain.PreviewAccess{Protected: true, Mode: domain.PreviewAccessPassword, BypassSecret: "s3cret", BypassConfigured: true}

	out, err := s.svc.Overview(s.ctx, s.preview.ID)
	s.Require().NoError(err)

	s.False(out.ErrorsSupported)
	s.NotNil(out.ErrorsLast)
	s.Empty(out.ErrorsLast)
	s.Require().NotNil(out.PreviewAccess)
	s.Equal(domain.PreviewAccessPassword, out.PreviewAccess.Mode)
	s.True(out.PreviewAccess.BypassConfigured)
	s.Require().NotNil(out.Detail)
	s.Empty(out.Detail.URL, "the project's production URL is not the preview environment's")
	s.Equal(domain.CloudStatusDeploying, out.Detail.Status)
	s.Require().NotNil(out.Detail.LatestDeployment)
	s.Equal("dpl_new", out.Detail.LatestDeployment.ID)

	raw, err := json.Marshal(out)
	s.Require().NoError(err)
	s.NotContains(string(raw), "s3cret")
	s.Contains(string(raw), `"per_branch":true`)
	s.Contains(string(raw), `"errors_supported":false`)
}

func (s *PreviewSuite) TestOverviewErrorsSupportedWhenProviderDoesNotSayOtherwise() {
	plain := &fakeProvider{kind: domain.CloudGCP, errorGroups: []domain.RuntimeErrorGroup{{Fingerprint: "f1", Count: 1}}}
	svc := cloud.NewService(cloud.Deps{
		Accounts: s.accounts, Environments: s.envs, Providers: []port.CloudProvider{plain},
		Components: s.components, Scans: newFakeScans(), Repos: newFakeRepos(),
	})
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "svc"}
	env := s.envs.seed(domain.ComponentEnvironment{
		RepositoryID: s.repoID, ComponentID: uuid.New(), Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Provider: domain.CloudGCP, AccountID: &s.acct.ID, Resource: &ref,
	})

	out, err := svc.Overview(s.ctx, env.ID)
	s.Require().NoError(err)
	s.True(out.ErrorsSupported)
	s.Nil(out.PreviewAccess)
	s.Len(out.ErrorsLast, 1)
}

func (s *PreviewSuite) TestLogsOfPerBranchEnvironmentReadTheNewestReadyPreview() {
	s.provider.deployments = []domain.CloudDeployment{{ID: "dpl_building", Status: domain.CloudDeployBuilding}, {ID: "dpl_ready", Status: domain.CloudDeployReady}}

	_, err := s.svc.Logs(s.ctx, s.preview.ID, domain.RuntimeLogQuery{})
	s.Require().NoError(err)

	s.Require().Len(s.provider.logsRefs, 1)
	s.Equal("dpl_ready", s.provider.logsRefs[0].Extra[domain.CloudRefDeploymentID])
	s.Nil(s.preview.Resource.Extra, "the stored ref is not mutated")
}

func (s *PreviewSuite) TestLogsOfPerBranchEnvironmentWithNoReadyPreviewIsEmpty() {
	s.provider.deployments = []domain.CloudDeployment{{ID: "dpl_building", Status: domain.CloudDeployBuilding}}

	page, err := s.svc.Logs(s.ctx, s.preview.ID, domain.RuntimeLogQuery{})
	s.Require().NoError(err)
	s.Empty(page.Entries)
	s.Empty(s.provider.logsRefs)
}

func (s *PreviewSuite) TestHealthOfPerBranchEnvironmentIsTheNewestPreviewBuild() {
	for _, tc := range []struct {
		deployments []domain.CloudDeployment
		want        domain.CloudResourceStatus
	}{
		{[]domain.CloudDeployment{{ID: "d", Status: domain.CloudDeployReady, CreatedAt: time.Now()}}, domain.CloudStatusHealthy},
		{[]domain.CloudDeployment{{ID: "d", Status: domain.CloudDeployBuilding, CreatedAt: time.Now()}}, domain.CloudStatusDeploying},
		{[]domain.CloudDeployment{{ID: "d", Status: domain.CloudDeployError, CreatedAt: time.Now()}}, domain.CloudStatusFailed},
		{nil, domain.CloudStatusUnknown},
	} {
		s.SetupTest()
		s.provider.detail = domain.CloudResourceDetail{Status: domain.CloudStatusHealthy}
		s.provider.deployments = tc.deployments
		s.svc.SetHealthIntervals(time.Hour, time.Millisecond, time.Millisecond)

		sweepCtx, cancel := context.WithCancel(s.ctx)
		s.svc.Start(sweepCtx)
		s.Require().Eventually(func() bool {
			got, err := s.envs.GetEnvironment(s.ctx, s.preview.ID)
			return err == nil && got.Health != nil
		}, time.Second, 5*time.Millisecond)
		cancel()

		got, err := s.envs.GetEnvironment(s.ctx, s.preview.ID)
		s.Require().NoError(err)
		s.Equal(tc.want, got.Health.Status)
		s.Equal(0, got.Health.ErrorCount24h)
		s.Equal([]domain.DeployEnvironment{domain.EnvironmentPreview}, s.provider.deploymentsEnvs)
	}
}
