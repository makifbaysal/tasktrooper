// Package alerting evaluates alert rules: a condition on one signal of one
// component environment (read through its confirmed binding) or on a raw
// query against a data source. A rule moves ok → pending → firing and back;
// firing either opens an incident through prodops, so its dedupe, remedy,
// policy and notifications apply, or notifies on the rule's own channels.
package alerting

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	DefaultInterval         = time.Minute
	DefaultMaxRulesPerSweep = 200

	defaultConcurrency = 4
	defaultReadTimeout = 20 * time.Second

	DefaultWindow = 5 * time.Minute
	// minReadLookback: an evaluation reads two windows back, and never less
	// than this, so a source keeping one point every few minutes still
	// answers its latest ones.
	minReadLookback = 15 * time.Minute
	minWindow       = time.Minute
	maxWindow       = 24 * time.Hour
	maxFor          = 7 * 24 * time.Hour

	maxNameLen  = 200
	maxQueryLen = 10000

	DefaultEventLimit = 50
	MaxEventLimit     = 500
	eventRetention    = 30 * 24 * time.Hour

	lockStripes = 64

	// orphanSweepEvery paces resolving the incidents of rules deleted with
	// their component or data source.
	orphanSweepEvery = 5 * time.Minute
	orphanSweepLimit = 500

	// maxReadErrors: a rule holds its state through this many evaluations in
	// a row whose read failed; the next one reads as no data, so a source
	// that stays down does not hold a firing rule firing, or a pending one
	// pending, forever.
	maxReadErrors   = 10
	maxLastErrorLen = 300
)

// webVitalsQuantile: Core Web Vitals are judged at the 75th percentile, every
// other quantile signal at its p95.
const (
	webVitalsQuantile = 0.75
	headlineQuantile  = 0.95
	webSignalPrefix   = "web."
)

var channelKinds = []domain.NotificationChannelKind{
	domain.NotificationChannelInApp, domain.NotificationChannelDesktop, domain.NotificationChannelIOSPush,
	domain.NotificationChannelSlack, domain.NotificationChannelEmail, domain.NotificationChannelWebhook,
}

type Components interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
}

// IncidentIngester is satisfied by prodops.Service.
type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

// IncidentCloser is what resolving an orphaned rule's incident needs; an
// IncidentIngester that is one too (prodops.Service) gets the sweep.
type IncidentCloser interface {
	List(ctx context.Context, filter domain.IncidentFilter) ([]domain.Incident, error)
	Resolve(ctx context.Context, incidentID uuid.UUID, note string) (domain.Incident, error)
}

// Notifier is satisfied by notify.Service. Kinds are the channels it can
// deliver on: a rule may name only those.
type Notifier interface {
	NotifyVia(ctx context.Context, n domain.Notification, kinds []domain.NotificationChannelKind)
	Kinds() []domain.NotificationChannelKind
}

type Deps struct {
	Rules      port.AlertRuleStore
	Signals    *signals.Service
	Sources    *datasource.Service
	Components Components
	// Incidents is optional; without it an open_incident rule notifies on
	// its channels instead.
	Incidents IncidentIngester
	// Notifier is optional; without it a rule only records its state.
	Notifier Notifier
}

type Service struct {
	rules      port.AlertRuleStore
	signals    *signals.Service
	sources    *datasource.Service
	components Components
	incidents  IncidentIngester
	notifier   Notifier

	now         func() time.Time
	interval    time.Duration
	maxPerSweep int
	concurrency int
	readTimeout time.Duration

	locks    [lockStripes]sync.Mutex
	cooldown *notify.Cooldown

	scope           func(ctx context.Context) string
	orphanMu        sync.Mutex
	lastOrphanSweep map[string]time.Time

	runMu  sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type Option func(*Service)

func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithInterval is how often each rule is evaluated.
func WithInterval(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.interval = d
		}
	}
}

func WithMaxRulesPerSweep(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.maxPerSweep = n
		}
	}
}

func WithConcurrency(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.concurrency = n
		}
	}
}

// WithReadTimeout bounds the read of one rule's samples.
func WithReadTimeout(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.readTimeout = d
		}
	}
}

func NewService(d Deps, opts ...Option) (*Service, error) {
	if d.Rules == nil || d.Signals == nil || d.Sources == nil || d.Components == nil {
		return nil, errors.New("alerting: rules, signals, sources and components are required")
	}
	s := &Service{
		rules:       d.Rules,
		signals:     d.Signals,
		sources:     d.Sources,
		components:  d.Components,
		incidents:   d.Incidents,
		notifier:    d.Notifier,
		now:         func() time.Time { return time.Now().UTC() },
		interval:    DefaultInterval,
		maxPerSweep: DefaultMaxRulesPerSweep,
		concurrency: defaultConcurrency,
		readTimeout: defaultReadTimeout,
		cooldown:    notify.NewCooldown(notify.DefaultCooldown).WithFlapLimit(notify.DefaultFlapLimit, notify.DefaultFlapSettle),

		lastOrphanSweep: map[string]time.Time{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// SetScope keys the evaluator's in-process state (the orphan sweep's pacing,
// the notification cooldown) by scope(ctx) as well, so a process sweeping
// several tenants keeps each apart. Without it there is one scope. Call it
// before Start.
func (s *Service) SetScope(scope func(ctx context.Context) string) { s.scope = scope }

func (s *Service) scopeOf(ctx context.Context) string {
	if s.scope == nil {
		return ""
	}
	return s.scope(ctx)
}

// lock serializes what changes one rule's state: an evaluation's write and
// a person's edit or delete.
func (s *Service) lock(id uuid.UUID) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write(id[:])
	return &s.locks[h.Sum32()%lockStripes]
}

// ConditionRequest is a condition in a request; an absent field keeps what
// the rule had (or the default on create).
type ConditionRequest struct {
	Op            *string  `json:"op"`
	Threshold     *float64 `json:"threshold"`
	ForSeconds    *int64   `json:"for_seconds"`
	WindowSeconds *int64   `json:"window_seconds"`
}

// RuleRequest is the body of POST and PATCH /v1/alert-rules: an absent field
// keeps what the rule had (or the default on create). Naming a signal and
// neither a query nor a source turns a raw rule into a signal rule, and
// naming a query or a source but no signal the other way round.
type RuleRequest struct {
	Name         *string           `json:"name"`
	ComponentID  *string           `json:"component_id"`
	Environment  *string           `json:"environment"`
	Signal       *string           `json:"signal"`
	DataSourceID *string           `json:"data_source_id"`
	Query        *string           `json:"query"`
	Condition    *ConditionRequest `json:"condition"`
	Severity     *string           `json:"severity"`
	Channels     *[]string         `json:"channels"`
	OpenIncident *bool             `json:"open_incident"`
	Enabled      *bool             `json:"enabled"`
}

func defaultSpec() domain.AlertRuleSpec {
	return domain.AlertRuleSpec{
		Environment: domain.EnvironmentProduction,
		Condition:   domain.AlertCondition{WindowSeconds: int64(DefaultWindow / time.Second)},
		Severity:    domain.IncidentSeverityMedium,
		Channels:    []domain.NotificationChannelKind{},
		Enabled:     true,
	}
}

func (s *Service) List(ctx context.Context, filter domain.AlertRuleFilter) ([]domain.AlertRule, error) {
	if filter.Environment != "" && !filter.Environment.Valid() {
		return nil, FieldErrors{"env": datasource.FieldInvalid}
	}
	return s.rules.ListAlertRules(ctx, filter)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.AlertRule, error) {
	return s.rules.GetAlertRule(ctx, id)
}

func (s *Service) Create(ctx context.Context, req RuleRequest) (domain.AlertRule, error) {
	spec, err := s.specFrom(ctx, defaultSpec(), req, true)
	if err != nil {
		return domain.AlertRule{}, err
	}
	return s.rules.CreateAlertRule(ctx, domain.AlertRule{
		AlertRuleSpec: spec,
		State:         domain.AlertStateOK,
		StateSince:    s.now(),
	})
}

// Update applies a patch. A change to what the rule reads or tests, to
// open_incident or to enabled starts its evaluation over from ok, resolving
// it first when it was firing; a new name, severity or channel list keeps
// its state.
func (s *Service) Update(ctx context.Context, id uuid.UUID, req RuleRequest) (domain.AlertRule, error) {
	l := s.lock(id)
	l.Lock()
	defer l.Unlock()
	rule, err := s.rules.GetAlertRule(ctx, id)
	if err != nil {
		return domain.AlertRule{}, err
	}
	spec, err := s.specFrom(ctx, rule.AlertRuleSpec, req, true)
	if err != nil {
		return domain.AlertRule{}, err
	}
	var event *domain.AlertEvent
	if restarts(rule.AlertRuleSpec, spec) {
		event = s.reset(ctx, rule)
		rule.State, rule.StateSince, rule.LastValue, rule.LastEvaluatedAt = domain.AlertStateOK, s.now(), nil, nil
		rule.AlertStreak = domain.AlertStreak{}
		rule.AlertReadErrors = domain.AlertReadErrors{}
	}
	rule.AlertRuleSpec = spec
	updated, err := s.rules.UpdateAlertRule(ctx, rule)
	if err != nil {
		return domain.AlertRule{}, err
	}
	if event != nil {
		s.appendEvent(ctx, *event)
	}
	return updated, nil
}

// Delete resolves a firing rule before it goes, so its incident does not
// outlive it.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	l := s.lock(id)
	l.Lock()
	defer l.Unlock()
	rule, err := s.rules.GetAlertRule(ctx, id)
	if err != nil {
		return err
	}
	if rule.State == domain.AlertStateFiring {
		s.resolveForEdit(ctx, rule)
	}
	return s.rules.DeleteAlertRule(ctx, id)
}

func (s *Service) Events(ctx context.Context, id uuid.UUID, limit int) ([]domain.AlertEvent, error) {
	if limit <= 0 {
		limit = DefaultEventLimit
	}
	if _, err := s.rules.GetAlertRule(ctx, id); err != nil {
		return nil, err
	}
	return s.rules.ListAlertEvents(ctx, id, min(limit, MaxEventLimit))
}

func restarts(before, after domain.AlertRuleSpec) bool {
	return before.ComponentID != after.ComponentID || before.Environment != after.Environment ||
		before.Signal != after.Signal || !sameSource(before.DataSourceID, after.DataSourceID) ||
		before.Query != after.Query || before.Condition != after.Condition ||
		before.OpenIncident != after.OpenIncident || before.Enabled != after.Enabled
}

func sameSource(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// reset is a rule leaving the state it had for ok outside an evaluation,
// resolved first when it was firing; nil when it was ok already.
func (s *Service) reset(ctx context.Context, rule domain.AlertRule) *domain.AlertEvent {
	if rule.State == domain.AlertStateOK {
		return nil
	}
	var incidentID *uuid.UUID
	if rule.State == domain.AlertStateFiring {
		incidentID = s.resolveForEdit(ctx, rule)
	}
	return &domain.AlertEvent{
		RuleID: rule.ID, At: s.now(), FromState: rule.State, ToState: domain.AlertStateOK, IncidentID: incidentID,
	}
}

// specFrom applies a request onto base and validates the result, reporting
// every problem as a field error.
func (s *Service) specFrom(ctx context.Context, base domain.AlertRuleSpec, req RuleRequest, requireName bool) (domain.AlertRuleSpec, error) {
	spec := base
	spec.Channels = slices.Clone(base.Channels)
	fe := FieldErrors{}
	if req.Name != nil {
		spec.Name = strings.TrimSpace(*req.Name)
	}
	if req.ComponentID != nil {
		raw := strings.TrimSpace(*req.ComponentID)
		id, err := uuid.Parse(raw)
		switch {
		case raw == "":
			spec.ComponentID = uuid.Nil
		case err != nil:
			fe["component_id"] = datasource.FieldInvalid
		default:
			spec.ComponentID = id
		}
	}
	if req.Environment != nil {
		spec.Environment = domain.DeployEnvironment(strings.TrimSpace(*req.Environment))
	}
	namesSignal := req.Signal != nil && strings.TrimSpace(*req.Signal) != ""
	namesRaw := (req.Query != nil && strings.TrimSpace(*req.Query) != "") ||
		(req.DataSourceID != nil && strings.TrimSpace(*req.DataSourceID) != "")
	if namesSignal && req.Query == nil && req.DataSourceID == nil {
		spec.Query, spec.DataSourceID = "", nil
	}
	if namesRaw && req.Signal == nil {
		spec.Signal = ""
	}
	if req.Signal != nil {
		spec.Signal = strings.TrimSpace(*req.Signal)
	}
	if req.Query != nil {
		spec.Query = strings.TrimSpace(*req.Query)
	}
	if req.DataSourceID != nil {
		raw := strings.TrimSpace(*req.DataSourceID)
		id, err := uuid.Parse(raw)
		switch {
		case raw == "":
			spec.DataSourceID = nil
		case err != nil:
			fe["data_source_id"] = datasource.FieldInvalid
		default:
			spec.DataSourceID = &id
		}
	}
	if c := req.Condition; c != nil {
		if c.Op != nil {
			spec.Condition.Op = domain.AlertOp(strings.TrimSpace(*c.Op))
		}
		if c.Threshold != nil {
			spec.Condition.Threshold = *c.Threshold
		}
		if c.ForSeconds != nil {
			spec.Condition.ForSeconds = *c.ForSeconds
		}
		if c.WindowSeconds != nil {
			spec.Condition.WindowSeconds = *c.WindowSeconds
		}
	}
	if req.Severity != nil {
		spec.Severity = domain.IncidentSeverity(strings.TrimSpace(*req.Severity))
	}
	if req.Channels != nil {
		spec.Channels = []domain.NotificationChannelKind{}
		for _, raw := range *req.Channels {
			kind := domain.NotificationChannelKind(strings.TrimSpace(raw))
			switch {
			case !slices.Contains(channelKinds, kind):
				fe["channels"] = datasource.FieldInvalidOption
			case !s.channelAvailable(kind):
				if _, bad := fe["channels"]; !bad {
					fe["channels"] = FieldUnavailable
				}
			case !slices.Contains(spec.Channels, kind):
				spec.Channels = append(spec.Channels, kind)
			}
		}
	}
	if req.OpenIncident != nil {
		spec.OpenIncident = *req.OpenIncident
	}
	if req.Enabled != nil {
		spec.Enabled = *req.Enabled
	}
	if err := s.validate(ctx, &spec, fe, requireName); err != nil {
		return domain.AlertRuleSpec{}, err
	}
	if len(fe) > 0 {
		return domain.AlertRuleSpec{}, fe
	}
	return spec, nil
}

// validate adds spec's problems to fe; an error it returns is one no field
// explains (the store failing).
func (s *Service) validate(ctx context.Context, spec *domain.AlertRuleSpec, fe FieldErrors, requireName bool) error {
	switch {
	case spec.Name == "" && requireName:
		fe["name"] = datasource.FieldRequired
	case len(spec.Name) > maxNameLen:
		fe["name"] = datasource.FieldTooLong
	}
	if _, bad := fe["component_id"]; !bad {
		if spec.ComponentID == uuid.Nil {
			fe["component_id"] = datasource.FieldRequired
		} else if _, err := s.components.GetComponent(ctx, spec.ComponentID); err != nil {
			if !errors.Is(err, port.ErrNotFound) {
				return err
			}
			fe["component_id"] = datasource.FieldInvalid
		}
	}
	if spec.Environment == "" {
		spec.Environment = domain.EnvironmentProduction
	}
	if !spec.Environment.Valid() {
		fe["environment"] = datasource.FieldInvalid
	}
	if err := s.validateTarget(ctx, spec, fe); err != nil {
		return err
	}

	c := &spec.Condition
	switch {
	case c.Op == "":
		fe["condition.op"] = datasource.FieldRequired
	case !domain.ValidAlertOp(c.Op):
		fe["condition.op"] = datasource.FieldInvalid
	}
	if c.WindowSeconds == 0 {
		c.WindowSeconds = int64(DefaultWindow / time.Second)
	}
	switch {
	case c.Window() < minWindow:
		fe["condition.window_seconds"] = datasource.FieldInvalid
	case c.Window() > maxWindow:
		fe["condition.window_seconds"] = datasource.FieldTooLong
	}
	switch {
	case c.ForSeconds < 0:
		fe["condition.for_seconds"] = datasource.FieldInvalid
	case c.For() > maxFor:
		fe["condition.for_seconds"] = datasource.FieldTooLong
	}
	if spec.Severity == "" {
		spec.Severity = domain.IncidentSeverityMedium
	}
	if spec.Severity.Rank() == 0 {
		fe["severity"] = datasource.FieldInvalid
	}
	return nil
}

func (s *Service) validateTarget(ctx context.Context, spec *domain.AlertRuleSpec, fe FieldErrors) error {
	if spec.Signal != "" {
		if spec.Query != "" {
			fe["query"] = datasource.FieldInvalidOption
		}
		if spec.DataSourceID != nil {
			fe["data_source_id"] = datasource.FieldInvalidOption
		}
		sig, ok := s.signals.Signal(spec.Signal)
		switch {
		case !ok:
			fe["signal"] = datasource.FieldInvalid
		case sig.Kind != domain.SignalMetric:
			fe["signal"] = datasource.FieldInvalidOption
		}
		return nil
	}
	if spec.Query == "" && spec.DataSourceID == nil {
		if _, bad := fe["data_source_id"]; !bad {
			fe["signal"] = datasource.FieldRequired
		}
		return nil
	}
	switch {
	case spec.Query == "":
		fe["query"] = datasource.FieldRequired
	case len(spec.Query) > maxQueryLen:
		fe["query"] = datasource.FieldTooLong
	}
	if spec.DataSourceID == nil {
		if _, bad := fe["data_source_id"]; !bad {
			fe["data_source_id"] = datasource.FieldRequired
		}
		return nil
	}
	ds, err := s.sources.Get(ctx, *spec.DataSourceID)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			return err
		}
		fe["data_source_id"] = datasource.FieldInvalid
		return nil
	}
	if !slices.Contains(ds.Capabilities, domain.CapabilityMetrics) {
		fe["data_source_id"] = datasource.FieldInvalidOption
		return nil
	}
	if _, bad := fe["query"]; bad {
		return nil
	}
	step := spec.Condition.Window()
	if step <= 0 {
		step = DefaultWindow
	}
	if _, err := s.sources.RenderRawQuery(ctx, ds.ID, spec.Query, nil, datasource.RenderOptions{Step: step}); err != nil {
		if errors.Is(err, datasource.ErrInvalidInput) {
			fe["query"] = datasource.FieldInvalid
			return nil
		}
		return err
	}
	return nil
}

// quantileOf is what a quantile signal is judged at.
func (s *Service) quantileOf(signal string) float64 {
	sig, ok := s.signals.Signal(signal)
	if !ok || sig.Aggregation != domain.AggregationQuantile {
		return 0
	}
	if strings.HasPrefix(signal, webSignalPrefix) {
		return webVitalsQuantile
	}
	return headlineQuantile
}

// read answers a rule's samples over [from, to] at step, and their effective
// step: its signal through the component environment's confirmed binding, or
// its raw query.
func (s *Service) read(ctx context.Context, spec domain.AlertRuleSpec, from, to time.Time, step time.Duration) ([]sample, time.Duration, error) {
	if spec.Signal != "" {
		series, err := s.signals.ReadSignal(ctx, spec.ComponentID, spec.Environment, spec.Signal, s.quantileOf(spec.Signal), from, to, step)
		if err != nil {
			return nil, 0, err
		}
		samples := samplesOf(series, spec.Condition.Op)
		return samples, effectiveStep(samples, step, 0), nil
	}
	if spec.DataSourceID == nil {
		return nil, 0, fmt.Errorf("the rule names neither a signal nor a data source: %w", ErrInvalidInput)
	}
	rctx, cancel := context.WithTimeout(ctx, s.readTimeout)
	defer cancel()
	query, err := s.sources.RenderRawQuery(rctx, *spec.DataSourceID, spec.Query, nil, datasource.RenderOptions{Step: step})
	if err != nil {
		return nil, 0, err
	}
	res, err := s.sources.Query(rctx, *spec.DataSourceID, domain.SeriesQuery{Query: query, From: from, To: to, Step: step})
	if err != nil {
		return nil, 0, err
	}
	samples := samplesOf(res.Series, spec.Condition.Op)
	return samples, effectiveStep(samples, step, res.Step), nil
}

// Channels is every channel kind a rule may name and whether this server can
// deliver on it.
func (s *Service) Channels() []domain.NotificationChannelStatus {
	out := make([]domain.NotificationChannelStatus, 0, len(channelKinds))
	for _, kind := range channelKinds {
		out = append(out, domain.NotificationChannelStatus{Kind: kind, Available: s.channelAvailable(kind)})
	}
	return out
}

func (s *Service) channelAvailable(kind domain.NotificationChannelKind) bool {
	return s.notifier != nil && slices.Contains(s.notifier.Kinds(), kind)
}

func (s *Service) unit(spec domain.AlertRuleSpec) string {
	if spec.Condition.Op == domain.AlertOpChangePctAbove || spec.Condition.Op == domain.AlertOpChangePctBelow {
		return "%"
	}
	if sig, ok := s.signals.Signal(spec.Signal); ok {
		return sig.Unit
	}
	return ""
}

// resolveForEdit resolves a firing rule a person is changing or deleting;
// the edit goes ahead even when prodops refuses, leaving the incident for a
// person to resolve.
func (s *Service) resolveForEdit(ctx context.Context, rule domain.AlertRule) *uuid.UUID {
	incidentID, err := s.resolve(ctx, rule, rule.LastValue)
	if err != nil {
		log.Warn().Err(err).Str("rule_id", rule.ID.String()).Msg("alerting: resolving an edited rule's incident failed")
	}
	return incidentID
}
