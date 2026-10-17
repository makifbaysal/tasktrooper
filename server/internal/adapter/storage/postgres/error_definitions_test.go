package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ErrorDefinitionStoreSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	pg        *database.Embedded
	pool      *pgxpool.Pool
	defs      *postgres.ErrorDefinitionStore
	jobs      *postgres.AIJobStore
	bindings  *postgres.SignalBindingStore
	rules     *postgres.AlertRuleStore
	sources   *postgres.DataSourceStore
	models    *postgres.ProjectModelStore
	repos     *postgres.RepositoryStore
	tasks     *postgres.BoardTaskStore
	repo      domain.Repository
	component domain.Component
}

func TestErrorDefinitionStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ErrorDefinitionStoreSuite))
}

func (s *ErrorDefinitionStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.defs = postgres.NewErrorDefinitionStore(db)
	s.jobs = postgres.NewAIJobStore(db)
	s.bindings = postgres.NewSignalBindingStore(db)
	s.rules = postgres.NewAlertRuleStore(db)
	s.sources = postgres.NewDataSourceStore(db)
	s.models = postgres.NewProjectModelStore(db)
	s.repos = postgres.NewRepositoryStore(db)
	s.tasks = postgres.NewBoardTaskStore(db)
}

func (s *ErrorDefinitionStoreSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ErrorDefinitionStoreSuite) SetupTest() {
	suffix := uuid.NewString()
	var err error
	s.repo, err = s.repos.Create(s.ctx, "errdef-repo-"+suffix, "", "/tmp/errdef-repo-"+suffix, "", "")
	s.Require().NoError(err)
	s.component, err = s.models.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repo.ID, Path: "api"})
	s.Require().NoError(err)
}

func (s *ErrorDefinitionStoreSuite) definition(name string, comp *uuid.UUID, status domain.ErrorDefinitionStatus, action domain.ErrorAction) domain.ErrorDefinition {
	d, err := s.defs.CreateErrorDefinition(s.ctx, domain.ErrorDefinition{
		RepositoryID: s.repo.ID, ComponentID: comp, Name: name,
		Match:    domain.ErrorMatch{MessageRegex: "timeout", Attributes: map[string]string{"tenant": "acme"}},
		Category: domain.ErrorCategoryDependency, Severity: domain.IncidentSeverityHigh, Action: action,
		AlertThreshold: &domain.ErrorAlertThreshold{Count: 5, WindowSeconds: 600},
		Source:         domain.ErrorDefinitionAI, Status: status, Rationale: "why", Evidence: []string{"a.go:1"},
	})
	s.Require().NoError(err)
	return d
}

func (s *ErrorDefinitionStoreSuite) task() uuid.UUID {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repo.ID, TaskNumber: num, Title: "errdef-test", TaskType: "task",
		Column: domain.TaskColumnTodo, Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	s.Require().NoError(err)
	return task.ID
}

func (s *ErrorDefinitionStoreSuite) TestRoundTripsEveryField() {
	d := s.definition("Timeout", &s.component.ID, domain.ErrorDefinitionSuggested, domain.ErrorActionAlert)
	got, err := s.defs.GetErrorDefinition(s.ctx, d.ID)
	s.Require().NoError(err)
	s.Equal("acme", got.Match.Attributes["tenant"])
	s.Equal(5, got.AlertThreshold.Count)
	s.Equal([]string{"a.go:1"}, got.Evidence)
	s.Equal(s.component.ID, *got.ComponentID)
	s.Nil(got.TaskID)

	_, err = s.defs.GetErrorDefinition(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *ErrorDefinitionStoreSuite) TestListFiltersByRepositoryComponentStatusAndAction() {
	own := s.definition("Own", &s.component.ID, domain.ErrorDefinitionActive, domain.ErrorActionTask)
	wide := s.definition("Wide", nil, domain.ErrorDefinitionActive, domain.ErrorActionIgnore)
	s.definition("Suggested", nil, domain.ErrorDefinitionSuggested, domain.ErrorActionTask)
	other, err := s.models.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repo.ID, Path: "web"})
	s.Require().NoError(err)
	s.definition("Other", &other.ID, domain.ErrorDefinitionActive, domain.ErrorActionTask)

	got, err := s.defs.ListErrorDefinitions(s.ctx, domain.ErrorDefinitionFilter{
		RepositoryIDs: []uuid.UUID{s.repo.ID}, ComponentID: &s.component.ID,
		Statuses: []domain.ErrorDefinitionStatus{domain.ErrorDefinitionActive},
	})
	s.Require().NoError(err)
	s.ElementsMatch([]uuid.UUID{own.ID, wide.ID}, idsOf(got))

	got, err = s.defs.ListErrorDefinitions(s.ctx, domain.ErrorDefinitionFilter{
		RepositoryIDs: []uuid.UUID{s.repo.ID}, Actions: []domain.ErrorAction{domain.ErrorActionIgnore},
	})
	s.Require().NoError(err)
	s.Equal([]uuid.UUID{wide.ID}, idsOf(got))
}

func idsOf(defs []domain.ErrorDefinition) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.ID)
	}
	return out
}

func (s *ErrorDefinitionStoreSuite) TestUpdateKeepsFiringOnlyWhileTheActionStays() {
	d := s.definition("Timeout", nil, domain.ErrorDefinitionActive, domain.ErrorActionAlert)
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.Require().NoError(s.defs.SetErrorDefinitionFiring(s.ctx, d.ID, &now))

	d.Name = "Renamed"
	got, err := s.defs.UpdateErrorDefinition(s.ctx, d)
	s.Require().NoError(err)
	s.Equal("Renamed", got.Name)
	s.Require().NotNil(got.FiringSince)
	s.Equal(domain.ErrorDefinitionAI, got.Source)

	d.Action = domain.ErrorActionIncident
	got, err = s.defs.UpdateErrorDefinition(s.ctx, d)
	s.Require().NoError(err)
	s.Nil(got.FiringSince)

	got, err = s.defs.SetErrorDefinitionStatus(s.ctx, d.ID, domain.ErrorDefinitionDismissed)
	s.Require().NoError(err)
	s.Equal(domain.ErrorDefinitionDismissed, got.Status)

	s.Require().NoError(s.defs.DeleteErrorDefinition(s.ctx, d.ID))
	s.ErrorIs(s.defs.DeleteErrorDefinition(s.ctx, d.ID), port.ErrNotFound)
}

func (s *ErrorDefinitionStoreSuite) TestAGroupLinkIsClaimedOnceAndReleasedWithoutATask() {
	d := s.definition("Nil", nil, domain.ErrorDefinitionActive, domain.ErrorActionTask)
	link := domain.ErrorGroupLink{DefinitionID: d.ID, Fingerprint: "fp", ComponentID: s.component.ID}
	longAgo := time.Now().Add(-time.Hour)
	ok, err := s.defs.ClaimErrorGroupLink(s.ctx, link, longAgo)
	s.Require().NoError(err)
	s.True(ok)
	ok, err = s.defs.ClaimErrorGroupLink(s.ctx, link, longAgo)
	s.Require().NoError(err)
	s.False(ok)
	s.Require().NoError(s.defs.ReleaseErrorGroupLink(s.ctx, d.ID, "fp"))
	ok, err = s.defs.ClaimErrorGroupLink(s.ctx, link, longAgo)
	s.Require().NoError(err)
	s.True(ok)
}

func (s *ErrorDefinitionStoreSuite) TestAClaimThatNeverLinkedATaskIsReclaimedOnceStaleButALinkedOneNever() {
	d := s.definition("Crash", nil, domain.ErrorDefinitionActive, domain.ErrorActionTask)
	link := domain.ErrorGroupLink{DefinitionID: d.ID, Fingerprint: "fp-stale", ComponentID: s.component.ID}
	ok, err := s.defs.ClaimErrorGroupLink(s.ctx, link, time.Now().Add(-time.Hour))
	s.Require().NoError(err)
	s.Require().True(ok)

	ok, err = s.defs.ClaimErrorGroupLink(s.ctx, link, time.Now().Add(-time.Hour))
	s.Require().NoError(err)
	s.False(ok, "a fresh claim is someone else's task in the making")
	ok, err = s.defs.ClaimErrorGroupLink(s.ctx, link, time.Now().Add(time.Minute))
	s.Require().NoError(err)
	s.True(ok, "a claim older than the cutoff that opened no task is taken over")

	other := domain.ErrorGroupLink{DefinitionID: d.ID, Fingerprint: "fp-linked", ComponentID: s.component.ID}
	ok, err = s.defs.ClaimErrorGroupLink(s.ctx, other, time.Now())
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Require().NoError(s.defs.SetErrorGroupLinkTask(s.ctx, d.ID, "fp-linked", s.task()))
	ok, err = s.defs.ClaimErrorGroupLink(s.ctx, other, time.Now().Add(time.Hour))
	s.Require().NoError(err)
	s.False(ok, "a group that has its task is never claimed again")
}

func (s *ErrorDefinitionStoreSuite) TestTwoEqualSuggestionsCannotBothBeStored() {
	d := domain.ErrorDefinition{RepositoryID: s.repo.ID, ComponentID: &s.component.ID, Name: "Dup",
		Match: domain.ErrorMatch{ExceptionType: "DupError"}, Category: domain.ErrorCategoryBug, Severity: domain.IncidentSeverityLow,
		Action: domain.ErrorActionNone, Source: domain.ErrorDefinitionAI, Status: domain.ErrorDefinitionSuggested, Evidence: []string{}}
	_, err := s.defs.CreateErrorDefinition(s.ctx, d)
	s.Require().NoError(err)
	_, err = s.defs.CreateErrorDefinition(s.ctx, d)
	s.ErrorIs(err, port.ErrErrorDefinitionExists)

	d.Status, d.Source = domain.ErrorDefinitionActive, domain.ErrorDefinitionHuman
	_, err = s.defs.CreateErrorDefinition(s.ctx, d)
	s.NoError(err, "a person's own definition is not a suggestion")
}

func (s *ErrorDefinitionStoreSuite) TestAIJobsLifecycle() {
	job, created, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobMapSignals, RepositoryID: s.repo.ID,
		ComponentID: &s.component.ID, Environment: domain.EnvironmentProduction, Owner: "this process"})
	s.Require().NoError(err)
	s.True(created)
	s.Equal(domain.AIJobRunning, job.Status)
	s.Equal("this process", job.Owner)
	s.Nil(job.StartedAt)
	s.Equal([]domain.AIJobItem{}, job.Created)

	same, created, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobMapSignals, RepositoryID: s.repo.ID,
		ComponentID: &s.component.ID, Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.False(created, "one job of a kind runs per repository, component and environment")
	s.Equal(job.ID, same.ID)
	wide, created, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobMapSignals, RepositoryID: s.repo.ID, Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.True(created, "the repository-wide job is another one")
	_, err = s.jobs.FinishAIJob(s.ctx, wide.ID, domain.AIJobDone, "", "")
	s.Require().NoError(err)

	started, err := s.jobs.StartAIJob(s.ctx, job.ID)
	s.Require().NoError(err)
	s.True(started)
	started, err = s.jobs.StartAIJob(s.ctx, job.ID)
	s.Require().NoError(err)
	s.False(started)

	session := uuid.New()
	s.Require().NoError(s.jobs.SetAIJobSession(s.ctx, job.ID, session))
	items := []domain.AIJobItem{
		{Kind: domain.AIJobItemSignalBinding, ID: uuid.New(), Name: "http.latency", Rationale: "histogram"},
		{Kind: domain.AIJobItemAlertRule, ID: uuid.New(), Name: "Slow"},
	}
	for _, it := range items {
		s.Require().NoError(s.jobs.AppendAIJobItem(s.ctx, job.ID, it))
	}
	s.Require().NoError(s.jobs.AppendAIJobItem(s.ctx, job.ID, domain.AIJobItem{Kind: items[0].Kind, ID: items[0].ID, Name: "again"}))
	bySession, err := s.jobs.GetAIJobBySession(s.ctx, session)
	s.Require().NoError(err)
	s.Equal(items, bySession.Created, "an item already recorded is not added twice")
	s.NotNil(bySession.StartedAt)

	done, err := s.jobs.FinishAIJob(s.ctx, job.ID, domain.AIJobDone, "two proposals", "")
	s.Require().NoError(err)
	s.Equal(domain.AIJobDone, done.Status)
	s.NotNil(done.FinishedAt)
	_, err = s.jobs.FinishAIJob(s.ctx, job.ID, domain.AIJobFailed, "", "late")
	s.ErrorIs(err, port.ErrNotFound, "a job that ended keeps its outcome")
	s.Require().NoError(s.jobs.AppendAIJobItem(s.ctx, job.ID, domain.AIJobItem{Kind: domain.AIJobItemAlertRule, ID: uuid.New()}))
	got, err := s.jobs.GetAIJob(s.ctx, job.ID)
	s.Require().NoError(err)
	s.Equal(domain.AIJobDone, got.Status)
	s.Len(got.Created, 2, "nothing is added to a job that ended")

	left, _, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobDefineErrors, RepositoryID: s.repo.ID, Environment: domain.EnvironmentProduction, Owner: "old"})
	s.Require().NoError(err)
	mine, _, err := s.jobs.CreateAIJob(s.ctx, domain.AIJob{Kind: domain.AIJobErrorCatalog, RepositoryID: s.repo.ID, Environment: domain.EnvironmentProduction, Owner: "new"})
	s.Require().NoError(err)
	n, err := s.jobs.FailRunningAIJobs(s.ctx, "new", time.Now().Add(time.Minute), "restart")
	s.Require().NoError(err)
	s.GreaterOrEqual(n, int64(1))
	got, err = s.jobs.GetAIJob(s.ctx, left.ID)
	s.Require().NoError(err)
	s.Equal(domain.AIJobFailed, got.Status)
	s.Equal("restart", got.Error)
	got, err = s.jobs.GetAIJob(s.ctx, mine.ID)
	s.Require().NoError(err)
	s.Equal(domain.AIJobRunning, got.Status, "the booting process's own jobs stay")

	n, err = s.jobs.FailStaleAIJobs(s.ctx, time.Now().Add(-time.Hour), time.Now().Add(time.Minute), "stale")
	s.Require().NoError(err)
	s.Equal(int64(1), n, "a job queued since before the cutoff that never started is stale")
	got, err = s.jobs.GetAIJob(s.ctx, mine.ID)
	s.Require().NoError(err)
	s.Equal("stale", got.Error)
}

func (s *ErrorDefinitionStoreSuite) TestAnAISuggestionIsStoredOnceAndNeverOverADismissedTarget() {
	ds, err := s.sources.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "prom", Config: map[string]string{"url": "http://localhost:9090"},
	}, nil)
	s.Require().NoError(err)
	b := domain.SignalBinding{ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.latency",
		DataSourceID: ds.ID, Query: "histogram_quantile($__quantile, rate(x[$__rate]))", Source: domain.SignalBindingAI}

	first, err := s.bindings.AddSignalBindingSuggestion(s.ctx, b)
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingSuggested, first.Status)
	s.Equal(domain.SignalBindingAI, first.Source)

	again, err := s.bindings.AddSignalBindingSuggestion(s.ctx, b)
	s.Require().NoError(err)
	s.Equal(first.ID, again.ID)

	_, err = s.bindings.DismissSignalBinding(s.ctx, first.ID)
	s.Require().NoError(err)
	dismissed, err := s.bindings.AddSignalBindingSuggestion(s.ctx, b)
	s.Require().NoError(err)
	s.Equal(first.ID, dismissed.ID)
	s.Equal(domain.SignalBindingDismissed, dismissed.Status)

	all, err := s.bindings.ListSignalBindings(s.ctx, s.component.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Len(all, 1)
}

func (s *ErrorDefinitionStoreSuite) TestAlertRulesKeepWhoCreatedThem() {
	spec := domain.AlertRuleSpec{
		Name: "Slow", ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.latency",
		Condition: domain.AlertCondition{Op: domain.AlertOpAbove, Threshold: 1, WindowSeconds: 300},
		Severity:  domain.IncidentSeverityMedium, Channels: []domain.NotificationChannelKind{},
	}
	human, err := s.rules.CreateAlertRule(s.ctx, domain.AlertRule{AlertRuleSpec: spec, State: domain.AlertStateOK, StateSince: time.Now()})
	s.Require().NoError(err)
	s.Equal(domain.AlertRuleByHuman, human.CreatedBy)

	ai, err := s.rules.CreateAlertRule(s.ctx, domain.AlertRule{AlertRuleSpec: spec, State: domain.AlertStateOK, StateSince: time.Now(), CreatedBy: domain.AlertRuleByAI})
	s.Require().NoError(err)
	ai.Enabled = true
	ai.CreatedBy = domain.AlertRuleByHuman
	updated, err := s.rules.UpdateAlertRule(s.ctx, ai)
	s.Require().NoError(err)
	s.Equal(domain.AlertRuleByAI, updated.CreatedBy)
}
