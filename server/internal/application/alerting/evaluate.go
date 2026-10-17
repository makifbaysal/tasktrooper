package alerting

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Start runs Sweep now and then a few times per interval until Stop, so a
// rule waits at most a quarter interval past its due time. A Start after
// Stop starts it again.
func (s *Service) Start(ctx context.Context) {
	s.runMu.Lock()
	if s.cancel != nil {
		s.runMu.Unlock()
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.runMu.Unlock()
	every := max(s.interval/4, time.Second)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.Sweep(ctx)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Sweep(ctx)
			}
		}
	}()
	log.Info().Dur("interval", s.interval).Msg("alert rule evaluator started")
}

// Stop cancels the loop, cutting a sweep in progress short (what it has not
// written is judged again at the next start), and waits for it.
func (s *Service) Stop() {
	s.runMu.Lock()
	cancel := s.cancel
	s.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	s.runMu.Lock()
	s.cancel = nil
	s.runMu.Unlock()
}

// Sweep evaluates every enabled rule whose interval has elapsed since its
// last evaluation (never evaluated first, at most max_rules_per_sweep),
// a few at once, resolves the incidents of rules that no longer exist and
// drops events older than 30 days.
func (s *Service) Sweep(ctx context.Context) {
	now := s.now()
	due, err := s.rules.DueAlertRules(ctx, now.Add(-s.interval), s.maxPerSweep)
	if err != nil {
		log.Warn().Err(err).Msg("alerting: listing due rules failed")
		return
	}
	sem := make(chan struct{}, max(s.concurrency, 1))
	var wg sync.WaitGroup
sweep:
	for _, rule := range due {
		select {
		case <-ctx.Done():
			break sweep
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(rule domain.AlertRule) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Str("rule_id", rule.ID.String()).Msg("alerting: evaluating a rule panicked")
				}
			}()
			if err := s.evaluate(ctx, rule); err != nil && ctx.Err() == nil {
				log.Warn().Err(err).Str("rule_id", rule.ID.String()).Msg("alerting: evaluating a rule failed")
			}
		}(rule)
	}
	wg.Wait()
	s.deliverSettled(ctx, now)
	s.resolveOrphans(ctx, now)
	s.prune(ctx, now)
}

// deliverSettled tells the final state of the rules whose flapping held
// their notifications back, once each has stayed put.
func (s *Service) deliverSettled(ctx context.Context, now time.Time) {
	if s.notifier == nil || ctx.Err() != nil {
		return
	}
	for _, h := range s.cooldown.Settled(s.cooldownKey(ctx, ""), now) {
		if n, ok := h.Payload.(heldNotification); ok {
			s.notifier.NotifyVia(ctx, n.notification, n.channels)
		}
	}
}

// heldNotification is a rule's notification held back while it flapped.
type heldNotification struct {
	notification domain.Notification
	channels     []domain.NotificationChannelKind
}

func (s *Service) cooldownKey(ctx context.Context, ruleID string) string {
	return s.scopeOf(ctx) + "\x00" + ruleID
}

// prune runs every sweep rather than on a timer of its own: a deployment that
// calls Sweep once per database must prune each, and the indexed delete
// costs nothing when there is nothing to drop.
func (s *Service) prune(ctx context.Context, now time.Time) {
	if _, err := s.rules.PruneAlertEvents(ctx, now.Add(-eventRetention)); err != nil {
		log.Warn().Err(err).Msg("alerting: pruning old events failed")
	}
}

// resolveOrphans resolves the live alert_rule incidents whose rule is gone:
// deleting a component or a data source deletes its rules in the database,
// with no evaluation left to resolve what a firing one opened. At most every
// orphanSweepEvery.
func (s *Service) resolveOrphans(ctx context.Context, now time.Time) {
	closer, ok := s.incidents.(IncidentCloser)
	if !ok || ctx.Err() != nil {
		return
	}
	scope := s.scopeOf(ctx)
	s.orphanMu.Lock()
	if last, ok := s.lastOrphanSweep[scope]; ok && now.Sub(last) < orphanSweepEvery {
		s.orphanMu.Unlock()
		return
	}
	s.lastOrphanSweep[scope] = now
	s.orphanMu.Unlock()

	live, err := closer.List(ctx, domain.IncidentFilter{
		Source: domain.IncidentSourceAlertRule, Statuses: domain.LiveIncidentStatuses, Limit: orphanSweepLimit,
	})
	if err != nil {
		log.Warn().Err(err).Msg("alerting: listing alert rule incidents failed")
		return
	}
	for _, in := range live {
		ruleID, ok := domain.AlertRuleOfFingerprint(in.Fingerprint)
		if !ok || in.Source != domain.IncidentSourceAlertRule {
			continue
		}
		_, err := s.rules.GetAlertRule(ctx, ruleID)
		switch {
		case err == nil:
			continue
		case !errors.Is(err, port.ErrNotFound):
			log.Warn().Err(err).Str("rule_id", ruleID.String()).Msg("alerting: reading an incident's rule failed")
			continue
		}
		if _, err := closer.Resolve(ctx, in.ID, orphanResolveNote); err != nil {
			log.Warn().Err(err).Str("incident_id", in.ID.String()).Msg("alerting: resolving an orphaned rule's incident failed")
		}
	}
}

// orphanResolveNote is the incident timeline's entry for it.
const orphanResolveNote = "the alert rule that opened it no longer exists"

// noData is a read that answered nothing to judge, for a reason that will
// not change by itself: nothing bound, a query the source refuses, a kind
// that cannot answer it, a source that is gone. Anything else — the source
// down or slow, a store or decryption failure, a panic — says nothing about
// the value, so the rule keeps its state.
func noData(err error) bool {
	return errors.Is(err, signals.ErrUnbound) ||
		errors.Is(err, signals.ErrInvalidInput) ||
		errors.Is(err, datasource.ErrInvalidInput) ||
		errors.Is(err, ErrInvalidInput) ||
		errors.Is(err, port.ErrDataSourceQuery) ||
		errors.Is(err, datasource.ErrUnsupported) ||
		errors.Is(err, port.ErrNotFound)
}

// evaluate reads a rule's latest window and moves its state. The read runs
// unlocked; the write re-reads the rule under its lock and is dropped when
// the rule was edited meanwhile, so an edit is never overwritten by an
// evaluation of what the rule used to say. Only a read that cannot work (see
// noData) is no data; any other failure leaves the rule where it was rather
// than resolving it, so one failed query never closes an incident a minute
// before reopening another — for up to maxReadErrors evaluations in a row,
// counted on the rule with the last failure, after which it reads as no data.
// A pending rule whose run of failures ends starts its for over: the time it
// went unread is not time the condition was seen to hold. A firing or a
// recovery that does not reach prodops leaves the rule where it was too, to
// be tried again next time.
func (s *Service) evaluate(ctx context.Context, snapshot domain.AlertRule) error {
	now := s.now()
	window := snapshot.Condition.Window()
	samples, step, err := s.safeRead(ctx, snapshot.AlertRuleSpec, now.Add(-max(2*window, minReadLookback)), now, window)
	keep := false
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return ctx.Err()
	case noData(err):
		log.Debug().Err(err).Str("rule_id", snapshot.ID.String()).Msg("alerting: no samples")
	case errors.Is(err, datasource.ErrUpstream), errors.Is(err, context.DeadlineExceeded):
		log.Debug().Err(err).Str("rule_id", snapshot.ID.String()).Msg("alerting: the source did not answer; keeping the state")
		keep = true
	default:
		log.Warn().Err(err).Str("rule_id", snapshot.ID.String()).Msg("alerting: reading the rule failed; keeping the state")
		keep = true
	}
	var readErrors domain.AlertReadErrors
	if keep {
		readErrors = domain.AlertReadErrors{ErrorCount: snapshot.ErrorCount + 1, LastError: domain.TruncateHead(err.Error(), maxLastErrorLen)}
		if readErrors.ErrorCount > maxReadErrors {
			keep = false
			samples = nil
		}
	}
	cur, prev := pick(samples, now, step)
	v := judge(snapshot.Condition, cur, prev)

	l := s.lock(snapshot.ID)
	l.Lock()
	defer l.Unlock()
	rule, err := s.rules.GetAlertRule(ctx, snapshot.ID)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !rule.UpdatedAt.Equal(snapshot.UpdatedAt) || !rule.Enabled {
		return nil
	}
	if keep {
		_, _, err = s.rules.RecordAlertEvaluation(ctx, rule.ID, rule.UpdatedAt,
			domain.AlertEvaluation{State: rule.State, StateSince: rule.StateSince, Value: rule.LastValue, At: now, Streak: rule.AlertStreak, Errors: readErrors}, nil)
		return err
	}
	st := standingOf(rule)
	if rule.ErrorCount > 0 && rule.State == domain.AlertStatePending {
		st.since = now
	}
	next := advance(st, rule.Condition, v, now)
	var event *domain.AlertEvent
	if next.state != rule.State {
		incidentID, err := s.announce(ctx, rule, next.state, v.value)
		if err != nil {
			log.Warn().Err(err).Str("rule_id", rule.ID.String()).Str("to", string(next.state)).
				Msg("alerting: the incident was not updated; trying again next evaluation")
			next = holdBack(rule, next, now)
		}
		if next.state != rule.State {
			event = &domain.AlertEvent{At: now, FromState: rule.State, ToState: next.state, Value: v.value, IncidentID: incidentID}
		}
	}
	_, _, err = s.rules.RecordAlertEvaluation(ctx, rule.ID, rule.UpdatedAt,
		domain.AlertEvaluation{State: next.state, StateSince: next.since, Value: v.value, At: now, Streak: next.stored(), Errors: readErrors}, event)
	return err
}

// safeRead is read with a panic turned into an error the rule keeps its
// state on, so the evaluation is still recorded and the rule waits its
// interval instead of being read again every sweep.
func (s *Service) safeRead(ctx context.Context, spec domain.AlertRuleSpec, from, to time.Time, step time.Duration) (samples []sample, effective time.Duration, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("alerting: reading a rule panicked")
			samples, effective, err = nil, 0, errReadPanicked
		}
	}()
	return s.read(ctx, spec, from, to, step)
}

// holdBack is where a rule stays when its firing or recovery could not be
// made: firing until the recovery goes through (with the streak that led
// out, so the next evaluation agreeing tries again), pending (its for already
// served) until the firing does.
func holdBack(rule domain.AlertRule, wanted standing, now time.Time) standing {
	switch {
	case rule.State == domain.AlertStateFiring:
		return standing{state: rule.State, since: rule.StateSince, streak: wanted.streak}
	case wanted.state == domain.AlertStateFiring && rule.State == domain.AlertStatePending:
		return standing{state: rule.State, since: rule.StateSince}
	case wanted.state == domain.AlertStateFiring:
		return standing{state: domain.AlertStatePending, since: now.Add(-rule.Condition.For())}
	}
	return wanted
}

// announce makes a state change known: entering firing opens (or folds
// into) the rule's incident or notifies; leaving it resolves.
func (s *Service) announce(ctx context.Context, rule domain.AlertRule, to domain.AlertState, value *float64) (*uuid.UUID, error) {
	switch {
	case to == domain.AlertStateFiring:
		return s.fire(ctx, rule, value)
	case rule.State == domain.AlertStateFiring:
		return s.resolve(ctx, rule, value)
	}
	return nil, nil
}

func (s *Service) fire(ctx context.Context, rule domain.AlertRule, value *float64) (*uuid.UUID, error) {
	if rule.OpenIncident && s.incidents != nil {
		in, err := s.incidentInput(ctx, rule, value)
		if err != nil {
			return nil, err
		}
		incident, err := s.incidents.Ingest(ctx, in)
		if err != nil {
			return nil, err
		}
		return &incident.ID, nil
	}
	s.notify(ctx, rule, domain.NotificationAlertFiring,
		fmt.Sprintf("%s · %s", strings.ToUpper(incidentEnv(rule.Environment)), rule.Name),
		fmt.Sprintf("%s (%s)", formatValue(value, s.unit(rule.AlertRuleSpec)), s.describe(rule.AlertRuleSpec)))
	return nil, nil
}

// resolve ingests the recovery of the rule's incident, or notifies. The
// incident it answers is nil when none was open.
func (s *Service) resolve(ctx context.Context, rule domain.AlertRule, value *float64) (*uuid.UUID, error) {
	if rule.OpenIncident && s.incidents != nil {
		in, err := s.incidentInput(ctx, rule, value)
		if err != nil {
			return nil, err
		}
		in.Resolved = true
		incident, err := s.incidents.Ingest(ctx, in)
		if err != nil {
			return nil, err
		}
		if incident.ID == uuid.Nil {
			return nil, nil
		}
		return &incident.ID, nil
	}
	s.notify(ctx, rule, domain.NotificationAlertResolved, "Recovered · "+rule.Name,
		fmt.Sprintf("%s · %s", strings.ToUpper(incidentEnv(rule.Environment)), formatValue(value, s.unit(rule.AlertRuleSpec))))
	return nil, nil
}

func (s *Service) incidentInput(ctx context.Context, rule domain.AlertRule, value *float64) (domain.IncidentInput, error) {
	comp, err := s.components.GetComponent(ctx, rule.ComponentID)
	if err != nil {
		return domain.IncidentInput{}, err
	}
	unit := s.unit(rule.AlertRuleSpec)
	payload := map[string]any{
		"rule_id":      rule.ID.String(),
		"rule_name":    rule.Name,
		"component_id": rule.ComponentID.String(),
		"environment":  string(rule.Environment),
		"signal":       rule.Signal,
		"query":        rule.Query,
		"condition":    rule.Condition,
		"value":        value,
	}
	if rule.DataSourceID != nil {
		payload["data_source_id"] = rule.DataSourceID.String()
	}
	return domain.IncidentInput{
		RepositoryID: comp.RepositoryID,
		Env:          incidentEnv(rule.Environment),
		Source:       domain.IncidentSourceAlertRule,
		Fingerprint:  domain.AlertRuleFingerprint(rule.ID),
		Title:        fmt.Sprintf("%s: %s", rule.Name, formatValue(value, unit)),
		Detail:       fmt.Sprintf("%s; value %s", s.describe(rule.AlertRuleSpec), formatValue(value, unit)),
		Severity:     rule.Severity,
		Payload:      payload,
	}, nil
}

// notify holds back a repeat of the kind last told for the rule inside 30
// minutes, as prodops does for an alert: a firing after a told recovery is
// told. A rule changing state more than four times in that window is
// flapping; it is told nothing more until it has stayed put for fifteen
// minutes, and then where it ended (deliverSettled). The hysteresis in
// advance keeps a condition near its threshold from flapping that fast.
func (s *Service) notify(ctx context.Context, rule domain.AlertRule, kind domain.NotificationKind, title, body string) {
	if s.notifier == nil || len(rule.Channels) == 0 {
		return
	}
	n := domain.Notification{
		Kind:     kind,
		Title:    title,
		Body:     body,
		Severity: rule.Severity,
		Link:     domain.AlertRuleLink(rule.ComponentID, rule.ID),
		Source:   domain.NotificationSourceAlert,
		SourceID: rule.ID.String(),
	}
	held := heldNotification{notification: n, channels: rule.Channels}
	if !s.cooldown.AllowTransitionHolding(s.cooldownKey(ctx, rule.ID.String()), string(kind), s.now(), held) {
		return
	}
	s.notifier.NotifyVia(ctx, n, rule.Channels)
}

func (s *Service) appendEvent(ctx context.Context, e domain.AlertEvent) {
	if err := s.rules.AppendAlertEvent(ctx, e); err != nil {
		log.Warn().Err(err).Str("rule_id", e.RuleID.String()).Msg("alerting: recording an event failed")
	}
}

// describe is the condition as data: "http.error_ratio gt 2% for 5m0s
// (window 5m0s)".
func (s *Service) describe(spec domain.AlertRuleSpec) string {
	subject := spec.Signal
	if subject == "" {
		subject = "query"
	}
	c := spec.Condition
	threshold := c.Threshold
	out := fmt.Sprintf("%s %s %s", subject, c.Op, formatValue(&threshold, s.unit(spec)))
	if c.Op == domain.AlertOpNoData {
		out = fmt.Sprintf("%s %s", subject, c.Op)
	}
	if c.ForSeconds > 0 {
		out += " for " + c.For().String()
	}
	return out + " (window " + c.Window().String() + ")"
}

// incidentEnv maps a deploy environment onto the label incidents carry: the
// legacy prod/stage/local where one exists, the environment's own name for
// preview, which has none.
func incidentEnv(env domain.DeployEnvironment) string {
	switch env {
	case domain.EnvironmentProduction:
		return domain.DeployEnvProd
	case domain.EnvironmentStaging:
		return domain.DeployEnvStage
	case domain.EnvironmentDevelopment:
		return domain.DeployEnvLocal
	}
	return string(env)
}

func formatValue(v *float64, unit string) string {
	if v == nil {
		return "no data"
	}
	x := *v
	var s string
	switch a := math.Abs(x); {
	case a >= 100:
		s = strconv.FormatFloat(math.Round(x), 'f', -1, 64)
	case a >= 1:
		s = strconv.FormatFloat(math.Round(x*100)/100, 'f', -1, 64)
	default:
		s = strconv.FormatFloat(x, 'g', 3, 64)
	}
	switch unit {
	case "":
		return s
	case "%":
		return s + "%"
	}
	return s + " " + unit
}
