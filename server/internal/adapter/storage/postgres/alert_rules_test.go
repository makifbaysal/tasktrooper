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

type AlertRuleStoreSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	pg        *database.Embedded
	pool      *pgxpool.Pool
	store     *postgres.AlertRuleStore
	sources   *postgres.DataSourceStore
	models    *postgres.ProjectModelStore
	repos     *postgres.RepositoryStore
	component domain.Component
	source    domain.DataSource
}

func TestAlertRuleStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(AlertRuleStoreSuite))
}

func (s *AlertRuleStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewAlertRuleStore(db)
	s.sources = postgres.NewDataSourceStore(db)
	s.models = postgres.NewProjectModelStore(db)
	s.repos = postgres.NewRepositoryStore(db)
}

func (s *AlertRuleStoreSuite) TearDownSuite() {
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

func (s *AlertRuleStoreSuite) SetupTest() {
	suffix := uuid.NewString()
	repo, err := s.repos.Create(s.ctx, "alerts-repo-"+suffix, "", "/tmp/alerts-repo-"+suffix, "", "")
	s.Require().NoError(err)
	s.component, err = s.models.SaveComponent(s.ctx, domain.Component{RepositoryID: repo.ID, Path: "api"})
	s.Require().NoError(err)
	s.source, err = s.sources.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "prom", Config: map[string]string{"url": "http://localhost:9090"},
	}, nil)
	s.Require().NoError(err)
}

func (s *AlertRuleStoreSuite) rule(name string, env domain.DeployEnvironment) domain.AlertRule {
	return domain.AlertRule{
		AlertRuleSpec: domain.AlertRuleSpec{
			Name: name, ComponentID: s.component.ID, Environment: env, Signal: "http.error_ratio",
			Condition: domain.AlertCondition{Op: domain.AlertOpAbove, Threshold: 2.5, ForSeconds: 300, WindowSeconds: 300},
			Severity:  domain.IncidentSeverityHigh,
			Channels:  []domain.NotificationChannelKind{domain.NotificationChannelDesktop, domain.NotificationChannelSlack},
			Enabled:   true,
		},
		State:      domain.AlertStateOK,
		StateSince: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}
}

func (s *AlertRuleStoreSuite) create(r domain.AlertRule) domain.AlertRule {
	saved, err := s.store.CreateAlertRule(s.ctx, r)
	s.Require().NoError(err)
	return saved
}

func (s *AlertRuleStoreSuite) mine(list []domain.AlertRule) []string {
	var out []string
	for _, r := range list {
		if r.ComponentID == s.component.ID {
			out = append(out, r.Name)
		}
	}
	return out
}

func (s *AlertRuleStoreSuite) TestRulesRoundTripAndListByComponentAndEnvironment() {
	saved := s.create(s.rule("b errors", domain.EnvironmentProduction))
	s.create(s.rule("A latency", domain.EnvironmentProduction))
	s.create(s.rule("staging errors", domain.EnvironmentStaging))

	got, err := s.store.GetAlertRule(s.ctx, saved.ID)
	s.Require().NoError(err)
	s.Equal(saved.AlertRuleSpec, got.AlertRuleSpec)
	s.Equal(domain.AlertCondition{Op: domain.AlertOpAbove, Threshold: 2.5, ForSeconds: 300, WindowSeconds: 300}, got.Condition)
	s.Equal([]domain.NotificationChannelKind{domain.NotificationChannelDesktop, domain.NotificationChannelSlack}, got.Channels)
	s.Nil(got.DataSourceID)
	s.Nil(got.LastValue)
	s.Nil(got.LastEvaluatedAt)
	s.True(got.StateSince.Equal(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)))
	s.False(got.CreatedAt.IsZero())

	prod, err := s.store.ListAlertRules(s.ctx, domain.AlertRuleFilter{ComponentID: &s.component.ID, Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Equal([]string{"A latency", "b errors"}, s.mine(prod))
	all, err := s.store.ListAlertRules(s.ctx, domain.AlertRuleFilter{ComponentID: &s.component.ID})
	s.Require().NoError(err)
	s.Len(all, 3)

	_, err = s.store.GetAlertRule(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *AlertRuleStoreSuite) TestARawRuleNeedsItsSourceAndNoSignal() {
	raw := s.rule("raw", domain.EnvironmentProduction)
	raw.Signal, raw.Query, raw.DataSourceID = "", `up{job="api"}`, &s.source.ID
	saved := s.create(raw)
	s.Require().NotNil(saved.DataSourceID)
	s.Equal(s.source.ID, *saved.DataSourceID)

	both := s.rule("both", domain.EnvironmentProduction)
	both.Query, both.DataSourceID = `up`, &s.source.ID
	_, err := s.store.CreateAlertRule(s.ctx, both)
	s.Error(err)
}

func (s *AlertRuleStoreSuite) TestUpdateReplacesTheSpecAndMovesUpdatedAt() {
	saved := s.create(s.rule("errors", domain.EnvironmentProduction))
	value := 4.2
	evaluated := time.Date(2026, 10, 17, 12, 5, 0, 0, time.UTC)
	saved.Name, saved.Enabled, saved.Channels = "renamed", false, []domain.NotificationChannelKind{}
	saved.State, saved.LastValue, saved.LastEvaluatedAt = domain.AlertStatePending, &value, &evaluated

	updated, err := s.store.UpdateAlertRule(s.ctx, saved)
	s.Require().NoError(err)
	s.Equal("renamed", updated.Name)
	s.False(updated.Enabled)
	s.Equal([]domain.NotificationChannelKind{}, updated.Channels)
	s.Equal(domain.AlertStatePending, updated.State)
	s.Equal(4.2, *updated.LastValue)
	s.True(updated.UpdatedAt.After(saved.UpdatedAt))

	saved.ID = uuid.New()
	_, err = s.store.UpdateAlertRule(s.ctx, saved)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *AlertRuleStoreSuite) TestDueRulesAreEnabledAndLongestWaitingFirst() {
	evaluated := s.create(s.rule("evaluated", domain.EnvironmentProduction))
	never := s.create(s.rule("never", domain.EnvironmentProduction))
	off := s.rule("off", domain.EnvironmentProduction)
	off.Enabled = false
	s.create(off)
	cutoff := time.Now().UTC().Add(time.Minute)
	_, ok, err := s.store.RecordAlertEvaluation(s.ctx, evaluated.ID, evaluated.UpdatedAt,
		domain.AlertEvaluation{State: domain.AlertStateOK, StateSince: cutoff, At: cutoff}, nil)
	s.Require().NoError(err)
	s.Require().True(ok)

	due, err := s.store.DueAlertRules(s.ctx, cutoff, 1000)
	s.Require().NoError(err)
	names := s.mine(due)
	s.Equal([]string{"never", "evaluated"}, names)
	s.Equal(never.ID, s.firstMine(due))

	due, err = s.store.DueAlertRules(s.ctx, cutoff.Add(-time.Second), 1000)
	s.Require().NoError(err)
	s.Equal([]string{"never"}, s.mine(due))
}

func (s *AlertRuleStoreSuite) firstMine(list []domain.AlertRule) uuid.UUID {
	for _, r := range list {
		if r.ComponentID == s.component.ID {
			return r.ID
		}
	}
	return uuid.Nil
}

func (s *AlertRuleStoreSuite) TestAnEvaluationIsRecordedOnlyAgainstTheSpecItRead() {
	saved := s.create(s.rule("errors", domain.EnvironmentProduction))
	at := time.Date(2026, 10, 17, 12, 1, 0, 0, time.UTC)
	value := 3.5
	incident := uuid.New()

	rule, ok, err := s.store.RecordAlertEvaluation(s.ctx, saved.ID, saved.UpdatedAt,
		domain.AlertEvaluation{State: domain.AlertStateFiring, StateSince: at, Value: &value, At: at},
		&domain.AlertEvent{At: at, FromState: domain.AlertStateOK, ToState: domain.AlertStateFiring, Value: &value, IncidentID: &incident})
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(domain.AlertStateFiring, rule.State)
	s.True(rule.StateSince.Equal(at))
	s.True(rule.LastEvaluatedAt.Equal(at))
	s.True(rule.UpdatedAt.Equal(saved.UpdatedAt))

	edited, err := s.store.UpdateAlertRule(s.ctx, rule)
	s.Require().NoError(err)
	_, ok, err = s.store.RecordAlertEvaluation(s.ctx, saved.ID, saved.UpdatedAt,
		domain.AlertEvaluation{State: domain.AlertStateOK, StateSince: at, At: at},
		&domain.AlertEvent{At: at, FromState: domain.AlertStateFiring, ToState: domain.AlertStateOK})
	s.Require().NoError(err)
	s.False(ok)
	still, err := s.store.GetAlertRule(s.ctx, saved.ID)
	s.Require().NoError(err)
	s.Equal(domain.AlertStateFiring, still.State)
	s.True(still.UpdatedAt.Equal(edited.UpdatedAt))

	events, err := s.store.ListAlertEvents(s.ctx, saved.ID, 10)
	s.Require().NoError(err)
	s.Require().Len(events, 1)
	s.Equal(domain.AlertStateFiring, events[0].ToState)
	s.Equal(incident, *events[0].IncidentID)
	s.Equal(3.5, *events[0].Value)
}

func (s *AlertRuleStoreSuite) TestEventsAreNewestFirstAndPrunedByAge() {
	saved := s.create(s.rule("errors", domain.EnvironmentProduction))
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, to := range []domain.AlertState{domain.AlertStatePending, domain.AlertStateFiring, domain.AlertStateOK} {
		s.Require().NoError(s.store.AppendAlertEvent(s.ctx, domain.AlertEvent{
			RuleID: saved.ID, At: base.Add(time.Duration(i) * 24 * time.Hour), FromState: domain.AlertStateOK, ToState: to,
		}))
	}

	events, err := s.store.ListAlertEvents(s.ctx, saved.ID, 2)
	s.Require().NoError(err)
	s.Require().Len(events, 2)
	s.Equal(domain.AlertStateOK, events[0].ToState)
	s.Equal(domain.AlertStateFiring, events[1].ToState)
	s.Nil(events[0].IncidentID)

	pruned, err := s.store.PruneAlertEvents(s.ctx, base.Add(36*time.Hour))
	s.Require().NoError(err)
	s.GreaterOrEqual(pruned, int64(2))
	events, err = s.store.ListAlertEvents(s.ctx, saved.ID, 10)
	s.Require().NoError(err)
	s.Len(events, 1)
}

func (s *AlertRuleStoreSuite) TestDeletingTheRuleOrItsComponentDeletesItsEvents() {
	saved := s.create(s.rule("errors", domain.EnvironmentProduction))
	s.Require().NoError(s.store.AppendAlertEvent(s.ctx, domain.AlertEvent{
		RuleID: saved.ID, At: time.Now().UTC(), FromState: domain.AlertStateOK, ToState: domain.AlertStatePending,
	}))
	s.Require().NoError(s.store.DeleteAlertRule(s.ctx, saved.ID))
	s.ErrorIs(s.store.DeleteAlertRule(s.ctx, saved.ID), port.ErrNotFound)
	var n int
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT count(*) FROM alert_events WHERE rule_id = $1`, saved.ID).Scan(&n))
	s.Zero(n)

	other := s.create(s.rule("other", domain.EnvironmentProduction))
	_, err := s.pool.Exec(s.ctx, `DELETE FROM project_components WHERE id = $1`, s.component.ID)
	s.Require().NoError(err)
	_, err = s.store.GetAlertRule(s.ctx, other.ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *AlertRuleStoreSuite) TestTheStreakOfAFiringRuleIsKept() {
	saved := s.create(s.rule("errors", domain.EnvironmentProduction))
	at := time.Date(2026, 10, 17, 12, 1, 0, 0, time.UTC)
	clearSince := at.Add(-time.Minute)

	rule, ok, err := s.store.RecordAlertEvaluation(s.ctx, saved.ID, saved.UpdatedAt, domain.AlertEvaluation{
		State: domain.AlertStateFiring, StateSince: at, At: at,
		Streak: domain.AlertStreak{ClearCount: 2, ClearSince: &clearSince, NoDataCount: 1},
	}, nil)
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(2, rule.ClearCount)
	s.Require().NotNil(rule.ClearSince)
	s.True(rule.ClearSince.Equal(clearSince))
	s.Equal(1, rule.NoDataCount)

	rule.AlertStreak = domain.AlertStreak{}
	edited, err := s.store.UpdateAlertRule(s.ctx, rule)
	s.Require().NoError(err)
	s.Equal(domain.AlertStreak{}, edited.AlertStreak)
}
