package alerting_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/alerting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const (
	errorRatioQuery = `error_ratio{job="api"}`
	latencyQuery    = `histogram_quantile($__quantile, sum by (le) (rate(latency_bucket{job="api"}[$__rate])))`
	backlogQuery    = `backlog{group="jobs"}`
	rawQuery        = `up{job="api"}`
)

type promKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
}

type AlertingSuite struct {
	suite.Suite
	ctx       context.Context
	now       time.Time
	rules     *fakeRules
	dsStore   *mocks.DataSourceStore
	querier   *mocks.MetricsQuerier
	bindings  *mocks.SignalBindingStore
	comps     *mocks.ComponentStore
	incidents *mocks.IncidentStore
	source    domain.DataSource
	api       domain.Component
	bound     []domain.SignalBinding
	known     map[uuid.UUID]domain.Component

	mu       sync.Mutex
	value    func(query string, at time.Time) *float64
	seen     []domain.SeriesQuery
	inFlight atomic.Int32
	peak     atomic.Int32
	slow     time.Duration
	fail     error
	step     time.Duration
	bindErr  error
	live     []domain.Incident
	sent     map[domain.NotificationChannelKind][]domain.Notification

	svc *alerting.Service
}

func TestAlertingSuite(t *testing.T) { suite.Run(t, new(AlertingSuite)) }

func (s *AlertingSuite) SetupTest() {
	s.ctx = context.Background()
	s.now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s.value = func(string, time.Time) *float64 { return nil }
	s.seen, s.slow, s.bound, s.fail, s.step, s.bindErr, s.live = nil, 0, nil, nil, 0, nil, nil
	s.inFlight.Store(0)
	s.peak.Store(0)
	s.sent = map[domain.NotificationChannelKind][]domain.Notification{}
	s.build()
}

func (s *AlertingSuite) build(opts ...alerting.Option) {
	t := s.T()
	s.rules = newFakeRules()
	s.dsStore = mocks.NewDataSourceStore(t)
	recipes := mocks.NewObservabilityRecipeStore(t)
	kind := mocks.NewDataSourceKindAdapter(t)
	s.querier = mocks.NewMetricsQuerier(t)
	s.bindings = mocks.NewSignalBindingStore(t)
	s.comps = mocks.NewComponentStore(t)
	s.incidents = mocks.NewIncidentStore(t)

	kind.On("Kind").Return(domain.DataSourceKindPrometheus).Maybe()
	kind.On("Schema").Return(domain.DataSourceSchema{Label: "Prometheus"}).Maybe()
	kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	recipes.On("ListUserRecipes", mock.Anything).Return([]domain.Recipe{}, nil).Maybe()

	s.source = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindPrometheus, Name: "prom"}
	s.dsStore.On("ListDataSources", mock.Anything).Return([]domain.DataSource{s.source}, nil).Maybe()
	s.dsStore.On("GetDataSource", mock.Anything, s.source.ID).Return(s.source, nil).Maybe()
	s.dsStore.On("GetDataSource", mock.Anything, mock.Anything).Return(domain.DataSource{}, port.ErrNotFound).Maybe()
	s.dsStore.On("DataSourceSecrets", mock.Anything, s.source.ID).Return(map[string]string{}, nil).Maybe()
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(s.answer).Maybe()

	s.known = map[uuid.UUID]domain.Component{}
	s.comps.On("GetComponent", mock.Anything, mock.Anything).Return(func(_ context.Context, id uuid.UUID) (domain.Component, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if c, ok := s.known[id]; ok {
			return c, nil
		}
		return domain.Component{}, port.ErrNotFound
	}).Maybe()
	s.api = s.component(domain.ComponentRoleBackend)
	s.bindings.On("ListSignalBindings", mock.Anything, mock.Anything, mock.Anything).Return(
		func(_ context.Context, componentID uuid.UUID, env domain.DeployEnvironment) ([]domain.SignalBinding, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.bindErr != nil {
				return nil, s.bindErr
			}
			out := []domain.SignalBinding{}
			for _, b := range s.bound {
				if b.ComponentID == componentID && b.Environment == env {
					out = append(out, b)
				}
			}
			return out, nil
		}).Maybe()

	s.incidents.On("AppendEvent", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	s.incidents.On("List", mock.Anything, mock.Anything).Return(func(_ context.Context, f domain.IncidentFilter) ([]domain.Incident, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		var out []domain.Incident
		for _, in := range s.live {
			if f.Source == "" || in.Source == f.Source {
				out = append(out, in)
			}
		}
		return out, nil
	}).Maybe()
	s.incidents.On("History", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	s.incidents.On("UpdateRemedy", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(func(_ context.Context, id uuid.UUID, _, _, _ string, _ int) (domain.Incident, error) {
			return domain.Incident{ID: id, Severity: domain.IncidentSeverityMedium}, nil
		}).Maybe()

	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store: s.dsStore, Recipes: recipes,
		Kinds:     []port.DataSourceKindAdapter{promKind{kind, s.querier}},
		URLPolicy: &policy,
	})
	s.Require().NoError(err)
	sigs, err := signals.NewService(signals.Deps{
		Bindings: s.bindings, Sources: sources, Components: s.comps, Environments: mocks.NewEnvironmentStore(t),
	}, signals.WithClock(s.clock))
	s.Require().NoError(err)

	notifier := notify.NewService()
	for _, k := range []domain.NotificationChannelKind{domain.NotificationChannelDesktop, domain.NotificationChannelSlack} {
		notifier.Register(s.channel(k))
	}
	base := []alerting.Option{alerting.WithClock(s.clock), alerting.WithReadTimeout(2 * time.Second)}
	s.svc, err = alerting.NewService(alerting.Deps{
		Rules:      s.rules,
		Signals:    sigs,
		Sources:    sources,
		Components: s.comps,
		Incidents:  prodops.NewService(prodops.Deps{Incidents: s.incidents}),
		Notifier:   notifier,
	}, append(base, opts...)...)
	s.Require().NoError(err)
}

func (s *AlertingSuite) clock() time.Time { return s.now }

func (s *AlertingSuite) channel(kind domain.NotificationChannelKind) *mocks.NotificationChannel {
	ch := mocks.NewNotificationChannel(s.T())
	ch.On("Kind").Return(kind).Maybe()
	ch.On("Send", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.sent[kind] = append(s.sent[kind], args.Get(1).(domain.Notification))
	}).Return(nil).Maybe()
	return ch
}

// answer is the source: a point per step of the range (per s.step when the
// source keeps a coarser period, as CloudWatch does), valued by s.value.
func (s *AlertingSuite) answer(_ context.Context, _ domain.DataSource, _ map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	n := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	for {
		p := s.peak.Load()
		if n <= p || s.peak.CompareAndSwap(p, n) {
			break
		}
	}
	s.mu.Lock()
	s.seen = append(s.seen, q)
	value, slow, fail, step := s.value, s.slow, s.fail, s.step
	s.mu.Unlock()
	if fail != nil {
		return domain.SeriesResult{}, fail
	}
	if slow > 0 {
		time.Sleep(slow)
	}
	every := max(q.Step, step)
	points := []domain.SeriesPoint{}
	for at := q.From.Truncate(every); !at.After(q.To); at = at.Add(every) {
		if v := value(q.Query, at); v != nil && !at.Before(q.From) {
			points = append(points, domain.SeriesPoint{Time: at, Value: v})
		}
	}
	return domain.SeriesResult{Series: []domain.Series{{Name: "", Points: points}}, Step: step}, nil
}

func (s *AlertingSuite) constant(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = func(string, time.Time) *float64 { return &v }
}

func (s *AlertingSuite) queries() []domain.SeriesQuery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.SeriesQuery(nil), s.seen...)
}

func (s *AlertingSuite) notifications(kind domain.NotificationChannelKind) []domain.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.Notification(nil), s.sent[kind]...)
}

func (s *AlertingSuite) component(role domain.ComponentRole) domain.Component {
	c := domain.Component{
		ID: uuid.New(), RepositoryID: uuid.New(), Path: string(role),
		Name: domain.Detected(string(role), domain.ConfidenceHigh), Role: domain.Detected(role, domain.ConfidenceHigh),
		Status: domain.ComponentStatusActive,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.known[c.ID] = c
	return c
}

func (s *AlertingSuite) bind(c domain.Component, signal, query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bound = append(s.bound, domain.SignalBinding{
		ID: uuid.New(), ComponentID: c.ID, Environment: domain.EnvironmentProduction, Signal: signal,
		DataSourceID: s.source.ID, Query: query, Params: map[string]string{},
		Status: domain.SignalBindingConfirmed, Source: domain.SignalBindingHuman,
	})
}

func ptr[T any](v T) *T { return &v }

func (s *AlertingSuite) ruleRequest(signal string, op domain.AlertOp, threshold float64, forSeconds int64) alerting.RuleRequest {
	return alerting.RuleRequest{
		Name:        ptr("Error ratio"),
		ComponentID: ptr(s.api.ID.String()),
		Signal:      ptr(signal),
		Condition: &alerting.ConditionRequest{
			Op: ptr(string(op)), Threshold: ptr(threshold), ForSeconds: ptr(forSeconds),
		},
	}
}

func (s *AlertingSuite) create(req alerting.RuleRequest) domain.AlertRule {
	rule, err := s.svc.Create(s.ctx, req)
	s.Require().NoError(err)
	return rule
}

func (s *AlertingSuite) sweepAfter(d time.Duration) {
	s.now = s.now.Add(d)
	s.svc.Sweep(s.ctx)
}

func (s *AlertingSuite) incidentRule() domain.AlertRule {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 120)
	req.OpenIncident = ptr(true)
	req.Severity = ptr("high")
	return s.create(req)
}

func (s *AlertingSuite) expectIngest(rule domain.AlertRule, incidentID uuid.UUID) *domain.IncidentInput {
	var got domain.IncidentInput
	s.incidents.On("Upsert", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		got = args.Get(1).(domain.IncidentInput)
	}).Return(domain.Incident{ID: incidentID, RepositoryID: s.api.RepositoryID, Env: "prod", Severity: rule.Severity}, true, nil).Once()
	return &got
}

func (s *AlertingSuite) expectRecovery(incidentID uuid.UUID, rule domain.AlertRule) {
	live := domain.Incident{ID: incidentID, RepositoryID: s.api.RepositoryID, Env: "prod", Severity: rule.Severity}
	s.incidents.On("FindLive", mock.Anything, s.api.RepositoryID, "prod", domain.AlertRuleFingerprint(rule.ID)).Return(live, nil).Once()
	s.incidents.On("UpdateStatus", mock.Anything, incidentID, domain.IncidentStatusResolved).Return(live, nil).Once()
}

func (s *AlertingSuite) TestCreateFillsTheDefaults() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	rule := s.create(s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0))

	s.NotEqual(uuid.Nil, rule.ID)
	s.Equal(domain.EnvironmentProduction, rule.Environment)
	s.Equal(domain.IncidentSeverityMedium, rule.Severity)
	s.Equal(int64(300), rule.Condition.WindowSeconds)
	s.Equal([]domain.NotificationChannelKind{}, rule.Channels)
	s.True(rule.Enabled)
	s.False(rule.OpenIncident)
	s.Nil(rule.DataSourceID)
	s.Equal(domain.AlertStateOK, rule.State)
	s.Equal(s.now, rule.StateSince)
	s.Nil(rule.LastEvaluatedAt)
}

func (s *AlertingSuite) TestCreateReportsEachProblemAsAFieldError() {
	tests := []struct {
		name  string
		edit  func(r *alerting.RuleRequest)
		field string
		code  string
	}{
		{"no name", func(r *alerting.RuleRequest) { r.Name = ptr(" ") }, "name", datasource.FieldRequired},
		{"a malformed component", func(r *alerting.RuleRequest) { r.ComponentID = ptr("nope") }, "component_id", datasource.FieldInvalid},
		{"an unknown component", func(r *alerting.RuleRequest) { r.ComponentID = ptr(uuid.NewString()) }, "component_id", datasource.FieldInvalid},
		{"an unknown environment", func(r *alerting.RuleRequest) { r.Environment = ptr("prod") }, "environment", datasource.FieldInvalid},
		{"an unknown signal", func(r *alerting.RuleRequest) { r.Signal = ptr("http.nope") }, "signal", datasource.FieldInvalid},
		{"a signal and a query", func(r *alerting.RuleRequest) { r.Query = ptr(rawQuery) }, "query", datasource.FieldInvalidOption},
		{"neither a signal nor a query", func(r *alerting.RuleRequest) { r.Signal = ptr("") }, "signal", datasource.FieldRequired},
		{"a query without a source", func(r *alerting.RuleRequest) { r.Signal, r.Query = nil, ptr(rawQuery) }, "data_source_id", datasource.FieldRequired},
		{"an unknown source", func(r *alerting.RuleRequest) {
			r.Signal, r.Query, r.DataSourceID = nil, ptr(rawQuery), ptr(uuid.NewString())
		}, "data_source_id", datasource.FieldInvalid},
		{"a query naming a param", func(r *alerting.RuleRequest) {
			r.Signal, r.Query, r.DataSourceID = nil, ptr(`up{job="$job"}`), ptr(s.source.ID.String())
		}, "query", datasource.FieldInvalid},
		{"no op", func(r *alerting.RuleRequest) { r.Condition.Op = nil }, "condition.op", datasource.FieldRequired},
		{"an unknown op", func(r *alerting.RuleRequest) { r.Condition.Op = ptr("gte") }, "condition.op", datasource.FieldInvalid},
		{"a window under a minute", func(r *alerting.RuleRequest) { r.Condition.WindowSeconds = ptr(int64(30)) }, "condition.window_seconds", datasource.FieldInvalid},
		{"a window over a day", func(r *alerting.RuleRequest) { r.Condition.WindowSeconds = ptr(int64(2 * 86400)) }, "condition.window_seconds", datasource.FieldTooLong},
		{"a negative for", func(r *alerting.RuleRequest) { r.Condition.ForSeconds = ptr(int64(-1)) }, "condition.for_seconds", datasource.FieldInvalid},
		{"an unknown severity", func(r *alerting.RuleRequest) { r.Severity = ptr("p1") }, "severity", datasource.FieldInvalid},
		{"an unknown channel", func(r *alerting.RuleRequest) { r.Channels = ptr([]string{"desktop", "pager"}) }, "channels", datasource.FieldInvalidOption},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0)
			tt.edit(&req)
			_, err := s.svc.Create(s.ctx, req)
			var fe alerting.FieldErrors
			s.Require().ErrorAs(err, &fe)
			s.Equal(tt.code, fe[tt.field], fe)
			s.ErrorIs(err, alerting.ErrInvalidInput)
		})
	}
}

func (s *AlertingSuite) TestAFiringRuleOpensAnIncidentThroughProdopsAndRecovers() {
	rule := s.incidentRule()
	s.constant(5)

	s.sweepAfter(0)
	s.Equal(domain.AlertStatePending, s.rules.get(rule.ID).State)
	s.sweepAfter(time.Minute)
	s.Equal(domain.AlertStatePending, s.rules.get(rule.ID).State)

	incidentID := uuid.New()
	in := s.expectIngest(rule, incidentID)
	s.sweepAfter(time.Minute)

	got := s.rules.get(rule.ID)
	s.Equal(domain.AlertStateFiring, got.State)
	s.Equal(s.now, got.StateSince)
	s.Require().NotNil(got.LastValue)
	s.Equal(5.0, *got.LastValue)
	s.Equal(s.api.RepositoryID, in.RepositoryID)
	s.Equal("prod", in.Env)
	s.Equal(domain.IncidentSourceAlertRule, in.Source)
	s.Equal("alert:"+rule.ID.String(), in.Fingerprint)
	s.Equal(domain.IncidentSeverityHigh, in.Severity)
	s.Equal("Error ratio: 5%", in.Title)
	s.Contains(in.Detail, "http.error_ratio gt 2% for 2m0s (window 5m0s)")
	s.Equal(rule.ID.String(), in.Payload["rule_id"])
	s.False(in.Resolved)

	s.constant(1)
	for range 5 {
		s.sweepAfter(time.Minute)
	}
	s.Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State, "false for four of the five minutes a recovery needs")
	s.expectRecovery(incidentID, rule)
	s.sweepAfter(time.Minute)

	s.Equal(domain.AlertStateOK, s.rules.get(rule.ID).State)
	s.Equal([]string{"ok>pending", "pending>firing", "firing>ok"}, s.rules.transitions(rule.ID))
	events := s.rules.eventsOf(rule.ID)
	s.Nil(events[0].IncidentID)
	s.Equal(incidentID, *events[1].IncidentID)
	s.Equal(incidentID, *events[2].IncidentID)
	s.Empty(s.notifications(domain.NotificationChannelDesktop))
}

func (s *AlertingSuite) TestAFiringProdopsRefusedIsTriedAgainNextEvaluation() {
	rule := s.incidentRule()
	s.constant(5)
	s.sweepAfter(0)
	s.incidents.On("Upsert", mock.Anything, mock.Anything).Return(domain.Incident{}, false, errors.New("db down")).Once()
	s.sweepAfter(2 * time.Minute)
	s.Equal(domain.AlertStatePending, s.rules.get(rule.ID).State)

	s.expectIngest(rule, uuid.New())
	s.sweepAfter(time.Minute)
	s.Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)
	s.Equal([]string{"ok>pending", "pending>firing"}, s.rules.transitions(rule.ID))
}

func (s *AlertingSuite) TestMissingDataIsNoDataAndOnlyANoDataRuleFiresOnIt() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	threshold := s.create(s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0))
	absent := s.ruleRequest("http.error_ratio", domain.AlertOpNoData, 0, 0)
	absent.Name, absent.Channels = ptr("Silent"), ptr([]string{"desktop"})
	silent := s.create(absent)

	s.sweepAfter(0)

	s.Equal(domain.AlertStateNoData, s.rules.get(threshold.ID).State)
	s.Nil(s.rules.get(threshold.ID).LastValue)
	s.Equal(domain.AlertStateFiring, s.rules.get(silent.ID).State)
	sent := s.notifications(domain.NotificationChannelDesktop)
	s.Require().Len(sent, 1)
	s.Equal(domain.NotificationAlertFiring, sent[0].Kind)
	s.Equal("PROD · Silent", sent[0].Title)
}

func (s *AlertingSuite) TestARuleWithoutAnIncidentNotifiesOnItsChannelsOnly() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0)
	req.Channels = ptr([]string{"slack"})
	req.Severity = ptr("critical")
	rule := s.create(req)

	s.constant(5)
	s.sweepAfter(0)
	s.constant(1)
	for range 6 {
		s.sweepAfter(time.Minute)
	}

	s.Empty(s.notifications(domain.NotificationChannelDesktop))
	sent := s.notifications(domain.NotificationChannelSlack)
	s.Require().Len(sent, 2)
	s.Equal(domain.NotificationAlertFiring, sent[0].Kind)
	s.Equal(domain.NotificationSourceAlert, sent[0].Source)
	s.Equal(rule.ID.String(), sent[0].SourceID)
	s.Equal(domain.IncidentSeverityCritical, sent[0].Severity)
	s.Equal("/operations/entity/"+s.api.ID.String()+"/alerts?rule="+rule.ID.String(), sent[0].Link)
	s.Equal("5% (http.error_ratio gt 2% (window 5m0s))", sent[0].Body)
	s.Equal(domain.NotificationAlertResolved, sent[1].Kind)
	s.Equal("Recovered · Error ratio", sent[1].Title)
}

func (s *AlertingSuite) TestAFlappingConditionStaysFiringAndNotifiesOnce() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0)
	req.Channels = ptr([]string{"desktop"})
	rule := s.create(req)

	for range 3 {
		s.constant(5)
		s.sweepAfter(time.Minute)
		s.constant(1)
		s.sweepAfter(time.Minute)
	}

	s.Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)
	s.Equal([]string{"ok>firing"}, s.rules.transitions(rule.ID))
	sent := s.notifications(domain.NotificationChannelDesktop)
	s.Require().Len(sent, 1)
	s.Equal(domain.NotificationAlertFiring, sent[0].Kind)
}

func (s *AlertingSuite) TestAReFiringAfterAToldRecoveryIsToldInsideTheCooldown() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0)
	req.Channels = ptr([]string{"desktop"})
	s.create(req)

	s.cycle()
	s.cycle()

	s.Equal([]domain.NotificationKind{
		domain.NotificationAlertFiring, domain.NotificationAlertResolved,
		domain.NotificationAlertFiring, domain.NotificationAlertResolved,
	}, s.kindsSent(domain.NotificationChannelDesktop), "seven minutes apart, each opposite of the last one told")
}

// cycle fires the rule, then clears it for the five minutes a recovery needs.
func (s *AlertingSuite) cycle() {
	s.constant(5)
	s.sweepAfter(time.Minute)
	s.constant(1)
	for range 6 {
		s.sweepAfter(time.Minute)
	}
}

func (s *AlertingSuite) kindsSent(channel domain.NotificationChannelKind) []domain.NotificationKind {
	var kinds []domain.NotificationKind
	for _, n := range s.notifications(channel) {
		kinds = append(kinds, n.Kind)
	}
	return kinds
}

func (s *AlertingSuite) TestAFlappingRuleIsToldFourTimesThenWhereItSettles() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0)
	req.Channels = ptr([]string{"desktop"})
	rule := s.create(req)

	s.cycle()
	s.cycle()
	s.constant(5)
	s.sweepAfter(time.Minute)
	s.Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)
	firing, resolved := domain.NotificationAlertFiring, domain.NotificationAlertResolved
	s.Equal([]domain.NotificationKind{firing, resolved, firing, resolved}, s.kindsSent(domain.NotificationChannelDesktop),
		"the fifth change inside half an hour is held")

	for range 14 {
		s.sweepAfter(time.Minute)
	}
	s.Len(s.kindsSent(domain.NotificationChannelDesktop), 4, "fourteen minutes firing is not settled yet")
	s.sweepAfter(time.Minute)
	s.Equal([]domain.NotificationKind{firing, resolved, firing, resolved, firing}, s.kindsSent(domain.NotificationChannelDesktop),
		"once it has stayed firing for fifteen minutes it is told so")
	s.sweepAfter(time.Minute)
	s.Len(s.kindsSent(domain.NotificationChannelDesktop), 5)
}

func (s *AlertingSuite) TestReadFailuresAreCountedAndPastTheLimitReadAsNoData() {
	rule := s.incidentRule()
	s.constant(5)
	s.sweepAfter(0)
	incidentID := uuid.New()
	s.expectIngest(rule, incidentID)
	s.sweepAfter(2 * time.Minute)
	s.Require().Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)

	s.mu.Lock()
	s.fail = errors.New("connection refused")
	s.mu.Unlock()
	for range 10 {
		s.sweepAfter(time.Minute)
	}
	got := s.rules.get(rule.ID)
	s.Equal(domain.AlertStateFiring, got.State, "ten failed reads in a row still hold the state")
	s.Equal(10, got.ErrorCount)
	s.Contains(got.LastError, "connection refused")

	for range 3 {
		s.sweepAfter(time.Minute)
	}
	s.Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State, "past the limit they read as no data, with its grace")
	s.expectRecovery(incidentID, rule)
	s.sweepAfter(time.Minute)
	got = s.rules.get(rule.ID)
	s.Equal(domain.AlertStateNoData, got.State, "a source down for good does not hold the rule firing forever")
	s.Equal(14, got.ErrorCount)

	s.mu.Lock()
	s.fail = nil
	s.mu.Unlock()
	s.sweepAfter(time.Minute)
	got = s.rules.get(rule.ID)
	s.Zero(got.ErrorCount)
	s.Empty(got.LastError)
}

func (s *AlertingSuite) TestAPendingRuleStartsItsForOverAfterARunOfFailedReads() {
	rule := s.incidentRule()
	s.constant(5)
	s.sweepAfter(0)
	s.Require().Equal(domain.AlertStatePending, s.rules.get(rule.ID).State)

	s.mu.Lock()
	s.fail = errors.New("connection refused")
	s.mu.Unlock()
	for range 5 {
		s.sweepAfter(time.Minute)
	}
	s.mu.Lock()
	s.fail = nil
	s.mu.Unlock()
	s.sweepAfter(time.Minute)
	got := s.rules.get(rule.ID)
	s.Equal(domain.AlertStatePending, got.State, "five unread minutes are not five minutes the condition held")
	s.Equal(s.now, got.StateSince)

	s.expectIngest(rule, uuid.New())
	s.sweepAfter(2 * time.Minute)
	s.Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)
}

func (s *AlertingSuite) TestRisingAndChangeRulesCompareAWindowEarlier() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	start := s.now
	s.mu.Lock()
	s.value = func(_ string, at time.Time) *float64 {
		v := 1000 + at.Sub(start).Minutes()*10
		return &v
	}
	s.mu.Unlock()
	rising := s.create(s.ruleRequest("http.error_ratio", domain.AlertOpRising, 500, 0))
	change := s.create(s.ruleRequest("http.error_ratio", domain.AlertOpChangePctAbove, 10, 0))

	s.sweepAfter(0)

	s.Equal(domain.AlertStateFiring, s.rules.get(rising.ID).State)
	s.Equal(1000.0, *s.rules.get(rising.ID).LastValue)
	got := s.rules.get(change.ID)
	s.Equal(domain.AlertStateOK, got.State)
	s.Require().NotNil(got.LastValue)
	s.InDelta(100*50.0/950, *got.LastValue, 1e-9)
}

func (s *AlertingSuite) TestOnlyRulesWhoseIntervalElapsedAreRead() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	s.create(s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0))
	s.constant(1)

	s.sweepAfter(0)
	s.sweepAfter(30 * time.Second)
	s.Len(s.queries(), 1)

	s.sweepAfter(30 * time.Second)
	s.Len(s.queries(), 2)
	q := s.queries()[1]
	s.Equal(errorRatioQuery, q.Query)
	s.Equal(5*time.Minute, q.Step)
	s.Equal(s.now.Add(-15*time.Minute), q.From)
	s.Equal(s.now, q.To)
}

func (s *AlertingSuite) TestASweepReadsAFewRulesAtOnceAndAtMostItsShare() {
	s.build(alerting.WithConcurrency(3), alerting.WithMaxRulesPerSweep(8))
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	for range 10 {
		s.create(s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0))
	}
	s.constant(1)
	s.slow = 20 * time.Millisecond

	s.sweepAfter(0)
	s.Len(s.queries(), 8)
	s.LessOrEqual(s.peak.Load(), int32(3))
	s.Greater(s.peak.Load(), int32(1))

	s.sweepAfter(time.Second)
	s.Len(s.queries(), 10)
}

func (s *AlertingSuite) TestEditingWhatAFiringRuleTestsResolvesItFirst() {
	rule := s.incidentRule()
	s.constant(5)
	s.sweepAfter(0)
	incidentID := uuid.New()
	s.expectIngest(rule, incidentID)
	s.sweepAfter(2 * time.Minute)
	s.Require().Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)

	renamed, err := s.svc.Update(s.ctx, rule.ID, alerting.RuleRequest{Name: ptr("Errors"), Severity: ptr("critical")})
	s.Require().NoError(err)
	s.Equal(domain.AlertStateFiring, renamed.State)
	s.Equal("Errors", renamed.Name)

	s.expectRecovery(incidentID, rule)
	edited, err := s.svc.Update(s.ctx, rule.ID, alerting.RuleRequest{Condition: &alerting.ConditionRequest{Threshold: ptr(10.0)}})
	s.Require().NoError(err)
	s.Equal(domain.AlertStateOK, edited.State)
	s.Equal(10.0, edited.Condition.Threshold)
	s.Equal(int64(120), edited.Condition.ForSeconds)
	s.Nil(edited.LastEvaluatedAt)
	s.Equal([]string{"ok>pending", "pending>firing", "firing>ok"}, s.rules.transitions(rule.ID))
	s.Equal(incidentID, *s.rules.eventsOf(rule.ID)[2].IncidentID)
}

func (s *AlertingSuite) TestAPatchNamingASignalTurnsARawRuleIntoASignalRule() {
	raw := s.create(alerting.RuleRequest{
		Name: ptr("Up"), ComponentID: ptr(s.api.ID.String()),
		DataSourceID: ptr(s.source.ID.String()), Query: ptr(rawQuery),
		Condition: &alerting.ConditionRequest{Op: ptr("lt"), Threshold: ptr(1.0)},
	})
	s.Equal(rawQuery, raw.Query)
	s.Require().NotNil(raw.DataSourceID)

	s.constant(0)
	s.sweepAfter(0)
	s.Equal(rawQuery, s.queries()[0].Query)
	s.Equal(domain.AlertStateFiring, s.rules.get(raw.ID).State)

	edited, err := s.svc.Update(s.ctx, raw.ID, alerting.RuleRequest{Signal: ptr("http.error_ratio")})
	s.Require().NoError(err)
	s.Equal("http.error_ratio", edited.Signal)
	s.Empty(edited.Query)
	s.Nil(edited.DataSourceID)
}

func (s *AlertingSuite) TestDeletingAFiringRuleResolvesItsIncident() {
	rule := s.incidentRule()
	s.constant(5)
	s.sweepAfter(0)
	incidentID := uuid.New()
	s.expectIngest(rule, incidentID)
	s.sweepAfter(2 * time.Minute)

	s.expectRecovery(incidentID, rule)
	s.Require().NoError(s.svc.Delete(s.ctx, rule.ID))
	_, err := s.svc.Get(s.ctx, rule.ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *AlertingSuite) TestEventsAnswerTheNewestFirst() {
	rule := s.incidentRule()
	s.constant(5)
	s.sweepAfter(0)
	s.constant(1)
	s.sweepAfter(time.Minute)

	events, err := s.svc.Events(s.ctx, rule.ID, 0)
	s.Require().NoError(err)
	s.Require().Len(events, 2)
	s.Equal(domain.AlertStateOK, events[0].ToState)
	s.Equal(domain.AlertStatePending, events[1].ToState)

	_, err = s.svc.Events(s.ctx, uuid.New(), 0)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *AlertingSuite) TestBacktestReplaysTheConditionAtTheWindowsResolution() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	outageFrom, outageTo := s.now.Add(-6*time.Hour), s.now.Add(-5*time.Hour)
	s.mu.Lock()
	s.value = func(_ string, at time.Time) *float64 {
		v := 1.0
		if !at.Before(outageFrom) && at.Before(outageTo) {
			v = 5 + at.Sub(outageFrom).Minutes()/10
		}
		return &v
	}
	s.mu.Unlock()
	req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 600)
	req.Name = nil

	out, err := s.svc.Backtest(s.ctx, alerting.BacktestRequest{RuleRequest: req, Days: 1})
	s.Require().NoError(err)

	s.Equal(int64(300), out.StepSeconds)
	s.Require().Len(out.Fires, 1)
	s.Equal(outageFrom.Add(10*time.Minute), out.Fires[0].Start)
	s.Require().NotNil(out.Fires[0].End)
	s.Equal(outageTo.Add(10*time.Minute), *out.Fires[0].End, "false for two steps and its for")
	s.InDelta(5+55.0/10, *out.Fires[0].Peak, 1e-9)
	s.Require().Len(out.Series, 1)
	s.Equal("http.error_ratio", out.Series[0].Name)
	s.Len(out.Series[0].Points, 24*12+1)
	q := s.queries()[0]
	s.Equal(s.now.Add(-24*time.Hour-5*time.Minute), q.From)
}

func (s *AlertingSuite) TestBacktestRefusesWhatItCannotReplay() {
	req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0)
	_, err := s.svc.Backtest(s.ctx, alerting.BacktestRequest{RuleRequest: req, Days: 20})
	var fe alerting.FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(datasource.FieldInvalid, fe["days"])

	_, err = s.svc.Backtest(s.ctx, alerting.BacktestRequest{RuleRequest: req})
	s.ErrorIs(err, signals.ErrUnbound)
}

func (s *AlertingSuite) TestSuggestComputesThresholdsFromTheLastTwoWeeks() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	s.bind(s.api, "http.latency", latencyQuery)
	s.mu.Lock()
	s.value = func(query string, _ time.Time) *float64 {
		v := 2.0
		if strings.Contains(query, "latency_bucket") {
			v = 120.4
		}
		return &v
	}
	s.mu.Unlock()

	out, err := s.svc.Suggest(s.ctx, alerting.SuggestRequest{ComponentID: s.api.ID.String()})
	s.Require().NoError(err)

	s.Require().Len(out, 2)
	ratio, latency := out[0], out[1]
	s.Equal("http.error_ratio", ratio.Signal)
	s.Equal(domain.AlertCondition{Op: domain.AlertOpAbove, Threshold: 2.6, ForSeconds: 300, WindowSeconds: 300}, ratio.Condition)
	s.True(ratio.OpenIncident)
	s.Equal(domain.IncidentSeverityHigh, ratio.Severity)
	s.Equal(domain.EnvironmentProduction, ratio.Environment)
	s.Equal([]domain.NotificationChannelKind{domain.NotificationChannelDesktop}, ratio.Channels)
	s.Equal("http.latency", latency.Signal)
	s.Equal(241.0, latency.Condition.Threshold)
	s.Equal(int64(600), latency.Condition.ForSeconds)
	var sawP95 bool
	for _, q := range s.queries() {
		s.Equal(10*time.Minute, q.Step)
		s.Equal(14*24*time.Hour, q.To.Sub(q.From))
		sawP95 = sawP95 || strings.Contains(q.Query, "histogram_quantile(0.95,")
	}
	s.True(sawP95)
}

func (s *AlertingSuite) TestSuggestFallsBackWithoutHistoryAndSkipsWhatARuleCovers() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	s.bind(s.api, "http.latency", latencyQuery)
	s.create(s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 3, 0))

	out, err := s.svc.Suggest(s.ctx, alerting.SuggestRequest{ComponentID: s.api.ID.String(), Environment: "production"})
	s.Require().NoError(err)
	s.Require().Len(out, 1)
	s.Equal("http.latency", out[0].Signal)
	s.Equal(1000.0, out[0].Condition.Threshold)
}

func (s *AlertingSuite) TestSuggestUsesEachRolesTemplate() {
	tests := []struct {
		role      domain.ComponentRole
		signal    string
		op        domain.AlertOp
		threshold float64
		forSecs   int64
	}{
		{domain.ComponentRoleWorker, "queue.backlog", domain.AlertOpRising, 1000, 600},
		{domain.ComponentRoleMobile, "mobile.crash_free_users", domain.AlertOpBelow, 99.5, 1800},
		{domain.ComponentRoleFrontend, "web.lcp", domain.AlertOpAbove, 2500, 900},
	}
	for _, tt := range tests {
		s.Run(string(tt.role), func() {
			c := s.component(tt.role)
			s.bind(c, tt.signal, backlogQuery)
			out, err := s.svc.Suggest(s.ctx, alerting.SuggestRequest{ComponentID: c.ID.String()})
			s.Require().NoError(err)
			s.Require().Len(out, 1)
			s.Equal(tt.signal, out[0].Signal)
			s.Equal(tt.op, out[0].Condition.Op)
			s.Equal(tt.threshold, out[0].Condition.Threshold)
			s.Equal(tt.forSecs, out[0].Condition.ForSeconds)
		})
	}
}

func (s *AlertingSuite) TestSuggestValidatesItsTarget() {
	_, err := s.svc.Suggest(s.ctx, alerting.SuggestRequest{ComponentID: "nope", Environment: "prod"})
	var fe alerting.FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(alerting.FieldErrors{"component_id": datasource.FieldInvalid, "environment": datasource.FieldInvalid}, fe)
}

func (s *AlertingSuite) TestStartSweepsUntilStop() {
	s.build(alerting.WithInterval(40 * time.Millisecond))
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	s.create(s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0))
	s.constant(1)

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	s.svc.Start(ctx)
	s.Eventually(func() bool { return len(s.queries()) >= 1 }, 2*time.Second, 5*time.Millisecond)
	s.svc.Stop()
	s.Require().NotEmpty(s.rules.pruned)
	s.Equal(s.now.Add(-30*24*time.Hour), s.rules.pruned[0])
}

func (s *AlertingSuite) TestASourceThatDoesNotAnswerKeepsAFiringRuleFiring() {
	rule := s.incidentRule()
	s.constant(5)
	s.sweepAfter(0)
	s.expectIngest(rule, uuid.New())
	s.sweepAfter(2 * time.Minute)
	s.Require().Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)

	s.mu.Lock()
	s.fail = errors.New("connection refused")
	s.mu.Unlock()
	s.sweepAfter(time.Minute)

	got := s.rules.get(rule.ID)
	s.Equal(domain.AlertStateFiring, got.State)
	s.Equal(5.0, *got.LastValue)
	s.Equal(s.now, *got.LastEvaluatedAt)
	s.Equal([]string{"ok>pending", "pending>firing"}, s.rules.transitions(rule.ID))
}

func (s *AlertingSuite) TestASignalWithNothingBoundIsNoData() {
	rule := s.create(s.ruleRequest("http.latency", domain.AlertOpAbove, 500, 0))
	s.constant(900)

	s.sweepAfter(0)

	s.Equal(domain.AlertStateNoData, s.rules.get(rule.ID).State)
	s.Empty(s.queries())
}

func (s *AlertingSuite) firingIncidentRule() (domain.AlertRule, uuid.UUID) {
	rule := s.incidentRule()
	s.constant(5)
	s.sweepAfter(0)
	incidentID := uuid.New()
	s.expectIngest(rule, incidentID)
	s.sweepAfter(2 * time.Minute)
	s.Require().Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)
	return rule, incidentID
}

func (s *AlertingSuite) TestAStoreErrorKeepsAFiringRuleFiringWithNoRecovery() {
	rule, _ := s.firingIncidentRule()
	s.mu.Lock()
	s.bindErr = errors.New("reading signal bindings: connection reset")
	s.mu.Unlock()

	for range 5 {
		s.sweepAfter(time.Minute)
	}

	got := s.rules.get(rule.ID)
	s.Equal(domain.AlertStateFiring, got.State)
	s.Equal(5.0, *got.LastValue)
	s.Equal(s.now, *got.LastEvaluatedAt, "the evaluation is still recorded, so the rule waits its interval")
	s.Equal([]string{"ok>pending", "pending>firing"}, s.rules.transitions(rule.ID))
	s.incidents.AssertNotCalled(s.T(), "FindLive", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func (s *AlertingSuite) TestAPanickingReadKeepsTheState() {
	rule, _ := s.firingIncidentRule()
	s.mu.Lock()
	s.value = func(string, time.Time) *float64 { panic("adapter bug") }
	s.mu.Unlock()

	s.sweepAfter(time.Minute)

	got := s.rules.get(rule.ID)
	s.Equal(domain.AlertStateFiring, got.State)
	s.Equal(s.now, *got.LastEvaluatedAt)
}

func (s *AlertingSuite) TestABindingGoneFromAFiringRuleIsNoDataOnlyPastTheGrace() {
	rule, incidentID := s.firingIncidentRule()
	s.mu.Lock()
	s.bound = nil
	s.mu.Unlock()

	for range 3 {
		s.sweepAfter(time.Minute)
		s.Require().Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)
	}
	s.expectRecovery(incidentID, rule)
	s.sweepAfter(time.Minute)

	s.Equal(domain.AlertStateNoData, s.rules.get(rule.ID).State)
	s.Equal([]string{"ok>pending", "pending>firing", "firing>no_data"}, s.rules.transitions(rule.ID))
}

func (s *AlertingSuite) TestASourceKeepingFiveMinutePeriodsIsStillJudgedEveryMinute() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	signalReq := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0)
	signalReq.Condition.WindowSeconds = ptr(int64(60))
	signalRule := s.create(signalReq)
	rawRule := s.create(alerting.RuleRequest{
		Name: ptr("Up"), ComponentID: ptr(s.api.ID.String()),
		DataSourceID: ptr(s.source.ID.String()), Query: ptr(rawQuery),
		Condition: &alerting.ConditionRequest{Op: ptr("gt"), Threshold: ptr(2.0), WindowSeconds: ptr(int64(60))},
	})
	s.constant(5)
	s.step = 5 * time.Minute

	s.sweepAfter(4 * time.Minute)

	s.Equal(domain.AlertStateFiring, s.rules.get(signalRule.ID).State, "the gap between samples is the period")
	s.Equal(domain.AlertStateFiring, s.rules.get(rawRule.ID).State, "the period the source answered at")
}

func (s *AlertingSuite) TestStartAfterStopStartsAgain() {
	s.build(alerting.WithInterval(40 * time.Millisecond))
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	s.create(s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0))
	s.constant(1)

	s.svc.Start(s.ctx)
	s.Eventually(func() bool { return len(s.queries()) >= 1 }, 2*time.Second, 5*time.Millisecond)
	s.svc.Stop()

	read := len(s.queries())
	s.now = s.now.Add(time.Hour)
	s.svc.Start(s.ctx)
	defer s.svc.Stop()
	s.Eventually(func() bool { return len(s.queries()) > read }, 2*time.Second, 5*time.Millisecond)
}

func (s *AlertingSuite) TestTheIncidentOfARuleThatNoLongerExistsIsResolved() {
	kept := s.create(s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0))
	orphan := domain.Incident{
		ID: uuid.New(), RepositoryID: s.api.RepositoryID, Env: "prod", Source: domain.IncidentSourceAlertRule,
		Fingerprint: domain.AlertRuleFingerprint(uuid.New()), Status: domain.IncidentStatusOpen,
	}
	s.live = []domain.Incident{
		orphan,
		{ID: uuid.New(), Source: domain.IncidentSourceAlertRule, Fingerprint: domain.AlertRuleFingerprint(kept.ID)},
		{ID: uuid.New(), Source: domain.IncidentSourceWebhook, Fingerprint: domain.AlertRuleFingerprint(uuid.New())},
	}
	s.incidents.On("UpdateStatus", mock.Anything, orphan.ID, domain.IncidentStatusResolved).
		Return(domain.Incident{ID: orphan.ID, Status: domain.IncidentStatusResolved}, nil).Once()

	s.sweepAfter(0)
	s.sweepAfter(time.Minute)

	s.incidents.AssertNumberOfCalls(s.T(), "UpdateStatus", 1)
}

func (s *AlertingSuite) TestAChannelThisServerCannotDeliverOnIsRefused() {
	s.bind(s.api, "http.error_ratio", errorRatioQuery)
	req := s.ruleRequest("http.error_ratio", domain.AlertOpAbove, 2, 0)
	req.Channels = ptr([]string{"desktop", "email"})

	_, err := s.svc.Create(s.ctx, req)

	var fe alerting.FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(alerting.FieldUnavailable, fe["channels"])
	available := map[domain.NotificationChannelKind]bool{}
	for _, ch := range s.svc.Channels() {
		available[ch.Kind] = ch.Available
	}
	s.Equal(map[domain.NotificationChannelKind]bool{
		domain.NotificationChannelInApp: false, domain.NotificationChannelDesktop: true, domain.NotificationChannelIOSPush: false,
		domain.NotificationChannelSlack: true, domain.NotificationChannelEmail: false, domain.NotificationChannelWebhook: false,
	}, available)
}

func (s *AlertingSuite) TestARecoveryProdopsRefusedIsTriedAgainAtTheNextFalseEvaluation() {
	rule, incidentID := s.firingIncidentRule()
	s.constant(1)
	for range 5 {
		s.sweepAfter(time.Minute)
	}
	s.incidents.On("FindLive", mock.Anything, s.api.RepositoryID, "prod", domain.AlertRuleFingerprint(rule.ID)).
		Return(domain.Incident{}, errors.New("db down")).Once()
	s.sweepAfter(time.Minute)
	s.Require().Equal(domain.AlertStateFiring, s.rules.get(rule.ID).State)

	s.expectRecovery(incidentID, rule)
	s.sweepAfter(time.Minute)

	s.Equal(domain.AlertStateOK, s.rules.get(rule.ID).State)
	s.Equal(domain.AlertStreak{}, s.rules.get(rule.ID).AlertStreak, "a streak is kept only while firing")
}
