package opsai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ServiceSuite struct {
	suite.Suite
	ctx       context.Context
	jobs      *memJobs
	sessions  *fakeSessions
	roles     *fakeRoles
	agents    *fakeAgents
	signals   *fakeSignals
	envs      *fakeEnvironments
	links     *fakeLinks
	tasks     *fakeTasks
	svc       *Service
	repo      domain.Repository
	api       domain.Component
	web       domain.Component
	releaseID uuid.UUID
}

func TestServiceSuite(t *testing.T) { suite.Run(t, new(ServiceSuite)) }

func (s *ServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.jobs = newMemJobs()
	s.sessions = &fakeSessions{}
	s.releaseID = uuid.New()
	s.roles = &fakeRoles{releaseRole: uuid.New(), byArea: map[string]uuid.UUID{domain.RepoKindBackend: s.releaseID}}
	s.agents = &fakeAgents{disabled: map[uuid.UUID]bool{}}
	s.signals = &fakeSignals{}
	s.envs = &fakeEnvironments{}
	s.links = &fakeLinks{}
	s.tasks = &fakeTasks{}
	s.repo = domain.Repository{ID: uuid.New(), Name: "acme-shop", Kind: domain.RepoKindBackend}
	s.api = domain.Component{ID: uuid.New(), RepositoryID: s.repo.ID, Path: "services/api", Status: domain.ComponentStatusActive,
		Role: domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceExact),
		Stack: domain.Detected(domain.ComponentStack{
			Languages:  []domain.StackItem{{Name: "Go"}},
			Frameworks: []domain.StackItem{{Name: "chi"}},
			Libraries:  []domain.StackItem{{Name: "github.com/prometheus/client_golang"}},
		}, domain.ConfidenceExact)}
	s.web = domain.Component{ID: uuid.New(), RepositoryID: s.repo.ID, Path: "web", Status: domain.ComponentStatusActive,
		Role: domain.Detected(domain.ComponentRoleFrontend, domain.ConfidenceExact)}
	s.svc = s.newService(WithSynchronousJobs())
}

func (s *ServiceSuite) newService(opts ...Option) *Service {
	svc, err := NewService(Deps{
		Jobs:         s.jobs,
		Sessions:     s.sessions,
		Roles:        s.roles,
		Agents:       s.agents,
		Components:   &fakeComponents{comps: []domain.Component{s.api, s.web}},
		Repositories: &fakeRepos{repos: []domain.Repository{s.repo}},
		Signals:      s.signals,
		Environments: s.envs,
		Links:        s.links,
		Tasks:        s.tasks,
	}, opts...)
	s.Require().NoError(err)
	return svc
}

func (s *ServiceSuite) TestSuggestErrorsRunsATurnWithTheReleaseEngineerAndRecordsWhatItsToolsStored() {
	stored := domain.AIJobItem{Kind: domain.AIJobItemErrorDefinition, ID: uuid.New(), Name: "Timeout", Rationale: "downstream"}
	var turnJob domain.AIJob
	s.sessions.turn = func(ctx context.Context, sid uuid.UUID) (domain.AgentResponse, error) {
		toolCtx := registry.ContextWithSessionID(ctx, sid)
		job, ok := s.svc.SessionJob(toolCtx)
		s.Require().True(ok)
		turnJob = job
		s.svc.Record(toolCtx, stored)
		return domain.AgentResponse{Message: domain.Message{Content: "  Timeout — dependency, alert  "}}, nil
	}

	job, err := s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String(), ComponentID: s.api.ID.String()})
	s.Require().NoError(err)

	s.Equal(domain.AIJobDone, job.Status)
	s.Equal(domain.AIJobDefineErrors, job.Kind)
	s.Equal(domain.EnvironmentProduction, job.Environment)
	s.Equal("Timeout — dependency, alert", job.ResultSummary)
	s.Equal([]domain.AIJobItem{stored}, job.Created)
	s.Equal(job.ID, turnJob.ID)
	s.Require().Len(s.sessions.created, 1)
	s.Equal(s.releaseID, *s.sessions.created[0].AgentID)
	s.Equal(s.repo.ID, *s.sessions.created[0].ProjectID)
	s.Require().Len(s.sessions.sent, 1)
	s.False(*s.sessions.sent[0].Orchestrate)
	s.Contains(s.sessions.sent[0].Content, "list_error_groups")
	s.Contains(s.sessions.sent[0].Content, "services/api")
}

func (s *ServiceSuite) TestCodeModeAsksForTheCodeCatalog() {
	job, err := s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String(), Mode: ModeCode})
	s.Require().NoError(err)
	s.Equal(domain.AIJobErrorCatalog, job.Kind)
	s.Nil(job.ComponentID)
	s.Contains(s.sessions.sent[0].Content, `source: "code"`)
	s.Contains(s.sessions.sent[0].Content, "web (frontend)")
}

func (s *ServiceSuite) TestAFailedOrQuestioningTurnFailsTheJob() {
	s.sessions.turn = func(context.Context, uuid.UUID) (domain.AgentResponse, error) {
		return domain.AgentResponse{}, errors.New("cli exited 1")
	}
	job, err := s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.Require().NoError(err)
	s.Equal(domain.AIJobFailed, job.Status)
	s.Equal("cli exited 1", job.Error)

	s.sessions.turn = func(context.Context, uuid.UUID) (domain.AgentResponse, error) {
		return domain.AgentResponse{Clarification: &domain.ClarificationRequest{}}, nil
	}
	job, err = s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.Require().NoError(err)
	s.Equal(domain.AIJobFailed, job.Status)
	s.Equal(errStoppedOnQuestion.Error(), job.Error)
}

func (s *ServiceSuite) TestARunningJobIsAnsweredInsteadOfStartingAnother() {
	running, _, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobMapSignals, RepositoryID: s.repo.ID, ComponentID: &s.api.ID, Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	job, err := s.svc.SuggestSignals(s.ctx, s.api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal(running.ID, job.ID)
	s.Empty(s.sessions.sent)
}

func (s *ServiceSuite) TestMapSignalsNamesTheUnboundGoldenSignals() {
	s.signals.signals = []domain.ComponentSignal{
		{Signal: "http.error_ratio", Golden: true, Status: domain.SignalStateUnbound},
		{Signal: "http.latency", Golden: true, Status: domain.SignalStateBound},
		{Signal: "queue.backlog", Status: domain.SignalStateSuggested},
	}
	job, err := s.svc.SuggestSignals(s.ctx, s.api.ID, domain.EnvironmentStaging)
	s.Require().NoError(err)
	s.Equal(domain.AIJobMapSignals, job.Kind)
	msg := s.sessions.sent[0].Content
	s.Contains(msg, "Golden signals with no confirmed binding: http.error_ratio.")
	s.Contains(msg, "Other signals with no confirmed binding: queue.backlog.")
	s.NotContains(msg, "http.latency")
}

func (s *ServiceSuite) TestJobsNeedAnEnabledReleaseEngineerAndValidInput() {
	_, err := s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: "nope", Environment: "moon", Mode: "x"})
	var fe FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(FieldErrors{"repository_id": datasource.FieldInvalid, "environment": datasource.FieldInvalid, "mode": datasource.FieldInvalidOption}, fe)

	_, err = s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: uuid.NewString()})
	s.Require().ErrorAs(err, &fe)
	s.Equal(datasource.FieldInvalid, fe["repository_id"])

	s.agents.disabled[s.releaseID] = true
	_, err = s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.ErrorIs(err, ErrNoReleaseEngineer)

	noChat, err := NewService(Deps{Jobs: s.jobs, Roles: s.roles, Components: &fakeComponents{}, Repositories: &fakeRepos{}})
	s.Require().NoError(err)
	_, err = noChat.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.ErrorIs(err, ErrUnavailable)
}

func (s *ServiceSuite) TestStartFailsJobsAPreviousProcessLeftRunning() {
	left, _, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobDefineErrors, RepositoryID: s.repo.ID, Owner: "an earlier process"})
	s.Require().NoError(err)
	own, _, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobErrorCatalog, RepositoryID: s.repo.ID, Owner: s.svc.owner})
	s.Require().NoError(err)
	s.svc.Start(s.ctx)
	got, err := s.svc.Job(s.ctx, left.ID)
	s.Require().NoError(err)
	s.Equal(domain.AIJobFailed, got.Status)
	s.Equal(interruptedReason, got.Error)
	got, err = s.svc.Job(s.ctx, own.ID)
	s.Require().NoError(err)
	s.Equal(domain.AIJobRunning, got.Status, "this process's own job is not interrupted")
}

func (s *ServiceSuite) TestStartLeavesAnotherProcesssRecentJobsWithAGrace() {
	recent, _, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobDefineErrors, RepositoryID: s.repo.ID, Owner: "another pod"})
	s.Require().NoError(err)
	svc := s.newService(WithInterruptedAfter(time.Hour))
	svc.Start(s.ctx)
	got, err := svc.Job(s.ctx, recent.ID)
	s.Require().NoError(err)
	s.Equal(domain.AIJobRunning, got.Status)
}

func (s *ServiceSuite) TestEachJobKindRunsUnderItsOwnNarrowToolCeiling() {
	_, err := s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.Require().NoError(err)
	_, err = s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String(), Mode: ModeCode})
	s.Require().NoError(err)
	_, err = s.svc.SuggestSignals(s.ctx, s.api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)

	s.Require().Len(s.sessions.sent, 3)
	for i, kind := range []domain.AIJobKind{domain.AIJobDefineErrors, domain.AIJobErrorCatalog, domain.AIJobMapSignals} {
		req := s.sessions.sent[i]
		s.Equal(JobTools(kind), req.ToolCeiling)
		s.True(req.NoQuotaPark, "a job's turn is never parked to resume after it failed")
		for _, forbidden := range []string{"run_terminal", "fetch_url", "merge_task_pull_request", "deploy_release",
			"rollback_task_release", "save_memory", "create_skill", "update_deploy_target", "write_file"} {
			s.NotContains(req.ToolCeiling, forbidden, kind)
		}
	}
	s.Contains(s.sessions.sent[0].ToolCeiling, "list_error_groups")
	s.NotContains(s.sessions.sent[0].ToolCeiling, "propose_signal_binding")
	s.Contains(s.sessions.sent[2].ToolCeiling, "propose_signal_binding")
	s.NotContains(s.sessions.sent[2].ToolCeiling, "propose_error_definitions")
}

func (s *ServiceSuite) TestRecordAfterTheJobEndedIsNoChange() {
	var sid uuid.UUID
	s.sessions.turn = func(_ context.Context, id uuid.UUID) (domain.AgentResponse, error) {
		sid = id
		return domain.AgentResponse{Message: domain.Message{Content: "done"}}, nil
	}
	job, err := s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.Require().NoError(err)
	s.Require().Equal(domain.AIJobDone, job.Status)

	s.svc.Record(registry.ContextWithSessionID(s.ctx, sid), domain.AIJobItem{Kind: domain.AIJobItemErrorDefinition, ID: uuid.New()})
	got, err := s.svc.Job(s.ctx, job.ID)
	s.Require().NoError(err)
	s.Empty(got.Created, "a person chatting in the job's session afterwards adds nothing to it")
}

func (s *ServiceSuite) TestRecordKeepsEachItemOnce() {
	item := domain.AIJobItem{Kind: domain.AIJobItemSignalBinding, ID: uuid.New(), Name: "http.latency"}
	s.sessions.turn = func(ctx context.Context, sid uuid.UUID) (domain.AgentResponse, error) {
		toolCtx := registry.ContextWithSessionID(ctx, sid)
		s.svc.Record(toolCtx, item)
		s.svc.Record(toolCtx, item)
		return domain.AgentResponse{Message: domain.Message{Content: "done"}}, nil
	}
	job, err := s.svc.SuggestSignals(s.ctx, s.api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal([]domain.AIJobItem{item}, job.Created)
}

func (s *ServiceSuite) TestAJobPastItsTimeLimitIsStoppedAndFailed() {
	svc := s.newService(WithSynchronousJobs(), WithJobLimits(50*time.Millisecond, 0))
	stopped := make(chan struct{})
	s.sessions.turn = func(context.Context, uuid.UUID) (domain.AgentResponse, error) {
		for {
			s.sessions.mu.Lock()
			n := len(s.sessions.cancelled)
			s.sessions.mu.Unlock()
			if n > 0 {
				close(stopped)
				return domain.AgentResponse{}, domain.ErrRunCancelled
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	job, err := svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.Require().NoError(err)
	<-stopped
	s.Equal(domain.AIJobFailed, job.Status)
	s.Equal(errTimedOut.Error(), job.Error)
}

func (s *ServiceSuite) TestAStaleRunningJobIsFailedSoANewOneStarts() {
	stuck, _, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobDefineErrors, RepositoryID: s.repo.ID, Environment: domain.EnvironmentProduction, Owner: s.svc.owner})
	s.Require().NoError(err)
	s.jobs.mu.Lock()
	j := s.jobs.jobs[stuck.ID]
	long := time.Now().Add(-2 * jobTimeout)
	j.CreatedAt, j.StartedAt = long, &long
	s.jobs.jobs[stuck.ID] = j
	s.jobs.mu.Unlock()

	job, err := s.svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.Require().NoError(err)
	s.NotEqual(stuck.ID, job.ID)
	s.Equal(domain.AIJobDone, job.Status)
	got, err := s.svc.Job(s.ctx, stuck.ID)
	s.Require().NoError(err)
	s.Equal(domain.AIJobFailed, got.Status)
	s.Equal(staleReason, got.Error)
}

func (s *ServiceSuite) TestAJobThatWaitsPastTheQueueLimitFails() {
	svc := s.newService(WithJobLimits(0, 50*time.Millisecond))
	release := make(chan struct{})
	s.sessions.turn = func(context.Context, uuid.UUID) (domain.AgentResponse, error) {
		<-release
		return domain.AgentResponse{Message: domain.Message{Content: "done"}}, nil
	}
	for _, kind := range []string{ModeErrors, ModeCode} {
		_, err := svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String(), Mode: kind})
		s.Require().NoError(err)
	}
	s.Eventually(func() bool {
		s.sessions.mu.Lock()
		defer s.sessions.mu.Unlock()
		return len(s.sessions.sent) == defaultConcurrency
	}, time.Second, 5*time.Millisecond)
	queued, err := svc.SuggestSignals(s.ctx, s.api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Eventually(func() bool {
		got, err := svc.Job(s.ctx, queued.ID)
		return err == nil && got.Status == domain.AIJobFailed && got.Error == errQueuedTooLong.Error()
	}, time.Second, 5*time.Millisecond)
	close(release)
	svc.Wait()
}

func (s *ServiceSuite) TestRecordOutsideAJobIsNoChange() {
	s.svc.Record(registry.ContextWithSessionID(s.ctx, uuid.New()), domain.AIJobItem{ID: uuid.New()})
	s.svc.Record(s.ctx, domain.AIJobItem{ID: uuid.New()})
	s.Empty(s.jobs.jobs)
}

func (s *ServiceSuite) TestAsynchronousJobsFinishInTheBackground() {
	svc := s.newService()
	job, err := svc.SuggestErrors(s.ctx, SuggestErrorsRequest{RepositoryID: s.repo.ID.String()})
	s.Require().NoError(err)
	s.Equal(domain.AIJobRunning, job.Status)
	svc.Wait()
	got, err := svc.Job(s.ctx, job.ID)
	s.Require().NoError(err)
	s.Equal(domain.AIJobDone, got.Status)
}

func (s *ServiceSuite) TestEnableOpensABriefedTaskForTheReleaseEngineer() {
	acct := uuid.New()
	s.envs.envs = []domain.ComponentEnvironment{{ComponentID: s.api.ID, Environment: domain.EnvironmentProduction, Provider: domain.CloudGCP,
		Status: domain.LinkConfirmed, AccountID: &acct, Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "api"}}}
	s.signals.signals = []domain.ComponentSignal{
		{Signal: "http.throughput", Golden: true, Status: domain.SignalStateBound},
		{Signal: "http.error_ratio", Golden: true, Status: domain.SignalStateUnbound},
	}

	task, err := s.svc.Enable(s.ctx, s.api.ID, EnableRequest{Goals: []string{"traces", "metrics", "traces"}, Target: "own_cloud"})
	s.Require().NoError(err)
	s.Equal("TC-7", task.Key)
	s.Equal(s.repo.ID, s.tasks.repo)
	s.Require().Len(s.tasks.created, 1)
	req := s.tasks.created[0]
	s.Equal(s.releaseID, *req.AssigneeAgentID)
	s.Equal(s.api.ID, *req.ComponentID)
	s.Equal(domain.TaskTypeTechnical, req.TaskType)
	s.Equal(domain.TaskColumnTodo, req.Column)
	s.Equal("Enable observability: api (production)", req.Title)
	for _, want := range []string{"traces, metrics", "(gcp)", "chi · Go", "http.throughput", "http.error_ratio", "prometheus", "create_board_task", "otel-instrumentation"} {
		s.Contains(req.Description, want)
	}
}

func (s *ServiceSuite) TestEnableAgainAnswersTheOpenTaskForTheComponentEnvironment() {
	first, err := s.svc.Enable(s.ctx, s.api.ID, EnableRequest{Goals: []string{"metrics"}, Target: "own_cloud"})
	s.Require().NoError(err)
	again, err := s.svc.Enable(s.ctx, s.api.ID, EnableRequest{Goals: []string{"traces"}, Target: "own_cloud"})
	s.Require().NoError(err)
	s.Equal(first.ID, again.ID)
	s.Len(s.tasks.created, 1)

	_, err = s.svc.Enable(s.ctx, s.api.ID, EnableRequest{Environment: "staging", Goals: []string{"traces"}, Target: "own_cloud"})
	s.Require().NoError(err)
	s.Len(s.tasks.created, 2, "another environment is another task")

	s.tasks.board[0].Column = domain.TaskColumnDone
	_, err = s.svc.Enable(s.ctx, s.api.ID, EnableRequest{Goals: []string{"traces"}, Target: "own_cloud"})
	s.Require().NoError(err)
	s.Len(s.tasks.created, 3, "a finished one is not reopened")
}

func (s *ServiceSuite) TestEnableValidatesTheRequest() {
	tests := []struct {
		name string
		req  EnableRequest
		want FieldErrors
	}{
		{name: "nothing", req: EnableRequest{}, want: FieldErrors{"goals": datasource.FieldRequired, "target": datasource.FieldRequired}},
		{name: "unknown goal", req: EnableRequest{Goals: []string{"magic"}, Target: "self_hosted"}, want: FieldErrors{"goals": datasource.FieldInvalidOption}},
		{name: "otlp without endpoint", req: EnableRequest{Goals: []string{"logs"}, Target: "otlp_endpoint"}, want: FieldErrors{"otlp_endpoint": datasource.FieldRequired}},
		{name: "endpoint without otlp", req: EnableRequest{Goals: []string{"logs"}, Target: "own_cloud", OTLPEndpoint: "https://x"}, want: FieldErrors{"otlp_endpoint": datasource.FieldInvalidOption}},
		{name: "bad endpoint", req: EnableRequest{Goals: []string{"logs"}, Target: "otlp_endpoint", OTLPEndpoint: "ftp://x"}, want: FieldErrors{"otlp_endpoint": datasource.FieldInvalid}},
		{name: "bad environment", req: EnableRequest{Environment: "moon", Goals: []string{"logs"}, Target: "own_cloud"}, want: FieldErrors{"environment": datasource.FieldInvalid}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.svc.Enable(s.ctx, s.api.ID, tt.req)
			var fe FieldErrors
			s.Require().ErrorAs(err, &fe)
			s.Equal(tt.want, fe)
		})
	}
	s.Empty(s.tasks.created)
}

func (s *ServiceSuite) TestEnableWithoutAReleaseEngineerForTheAreaIsRefused() {
	_, err := s.svc.Enable(s.ctx, s.web.ID, EnableRequest{Goals: []string{"rum"}, Target: "self_hosted"})
	s.ErrorIs(err, ErrNoReleaseEngineer)
}

func (s *ServiceSuite) vercelEnv(c domain.Component) domain.ComponentEnvironment {
	account := uuid.New()
	return domain.ComponentEnvironment{
		ID: uuid.New(), RepositoryID: s.repo.ID, ComponentID: c.ID, Environment: domain.EnvironmentProduction,
		Provider: domain.CloudVercel, AccountID: &account, Status: domain.LinkConfirmed,
		Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"},
	}
}

func (s *ServiceSuite) TestReadinessSaysWhenAVercelProjectIsNotMeasured() {
	env := s.vercelEnv(s.web)
	s.envs.envs = []domain.ComponentEnvironment{env}
	analytics := &fakeAnalytics{ok: true, state: domain.CloudAnalytics{WebAnalytics: domain.AnalyticsNotEnabled, SpeedInsights: domain.AnalyticsEnabled}}
	deps := Deps{
		Jobs: s.jobs, Sessions: s.sessions, Roles: s.roles, Agents: s.agents,
		Components: &fakeComponents{comps: []domain.Component{s.api, s.web}}, Repositories: &fakeRepos{repos: []domain.Repository{s.repo}},
		Signals: s.signals, Environments: s.envs, Links: s.links, Tasks: s.tasks, Analytics: analytics,
	}
	svc, err := NewService(deps, WithSynchronousJobs())
	s.Require().NoError(err)

	r, err := svc.Readiness(s.ctx, s.web.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal(domain.CloudVercel, r.Provider)
	s.Require().NotNil(r.Analytics)
	s.Equal(domain.AnalyticsNotEnabled, r.Analytics.WebAnalytics)
	s.Equal([]uuid.UUID{env.ID}, analytics.asked)
	s.Contains(r.Suggestions, domain.ReadinessSuggestion{Goal: domain.GoalRUM, Code: "analytics_not_enabled"})

	analytics.state = domain.CloudAnalytics{WebAnalytics: domain.AnalyticsEnabled, SpeedInsights: domain.AnalyticsEnabled}
	r, err = svc.Readiness(s.ctx, s.web.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.NotContains(r.Suggestions, domain.ReadinessSuggestion{Goal: domain.GoalRUM, Code: "analytics_not_enabled"})

	analytics.ok = false
	r, err = svc.Readiness(s.ctx, s.web.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Nil(r.Analytics, "a provider that cannot be asked says nothing, and nothing is wrong with that")
}

func (s *ServiceSuite) TestReadinessAsksNoOneAboutAnotherProvider() {
	env := s.vercelEnv(s.api)
	env.Provider = domain.CloudGCP
	s.envs.envs = []domain.ComponentEnvironment{env}
	analytics := &fakeAnalytics{ok: true}
	svc, err := NewService(Deps{
		Jobs: s.jobs, Roles: s.roles, Components: &fakeComponents{comps: []domain.Component{s.api}}, Repositories: &fakeRepos{repos: []domain.Repository{s.repo}},
		Environments: s.envs, Analytics: analytics,
	}, WithSynchronousJobs())
	s.Require().NoError(err)
	r, err := svc.Readiness(s.ctx, s.api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Nil(r.Analytics)
	s.Empty(analytics.asked)
}

func (s *ServiceSuite) TestReadinessReportsSignalsInstrumentationAndGaps() {
	sentry := uuid.New()
	s.links.links = []domain.ComponentLink{{FromComponentID: s.api.ID, ToResourceID: &sentry, Status: domain.LinkConfirmed}}
	s.links.resources = []domain.SystemResource{{ID: sentry, Kind: domain.ResourceObservability, Vendor: "Sentry"}}
	s.signals.signals = []domain.ComponentSignal{
		{Signal: "http.error_ratio", Golden: true, Status: domain.SignalStateBound},
		{Signal: "http.latency", Golden: true, Status: domain.SignalStateUnbound},
		{Signal: "logs.lines", Status: domain.SignalStateUnbound},
		{Signal: "queue.backlog", Status: domain.SignalStateSuggested},
	}

	r, err := s.svc.Readiness(s.ctx, s.api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal(domain.ComponentRoleBackend, r.Role)
	s.True(r.Instrumentation[domain.InstrumentationSentry])
	s.True(r.Instrumentation[domain.InstrumentationPrometheus])
	s.False(r.Instrumentation[domain.InstrumentationOTel])
	s.Len(r.Signals, 4)
	s.Equal([]domain.ReadinessSuggestion{
		{Goal: domain.GoalMetrics, Code: "golden_partly_unbound"},
		{Goal: domain.GoalTraces, Code: "no_tracing_sdk"},
		{Goal: domain.GoalLogs, Code: "no_log_source"},
		{Goal: domain.GoalQueues, Code: "queues_unbound"},
	}, r.Suggestions)

	r, err = s.svc.Readiness(s.ctx, s.web.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Contains(r.Suggestions, domain.ReadinessSuggestion{Goal: domain.GoalRUM, Code: "no_rum"})

	_, err = s.svc.Readiness(s.ctx, s.api.ID, "moon")
	s.Require().Error(err)
}
