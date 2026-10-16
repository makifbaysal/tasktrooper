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
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Start runs Sweep now and then a few times per interval until Stop, so a
// rule waits at most a quarter interval past its due time.
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
}

// Sweep evaluates every enabled rule whose interval has elapsed since its
// last evaluation (never evaluated first, at most max_rules_per_sweep),
// a few at once, and drops events older than 30 days.
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
	s.prune(ctx, now)
}

// prune runs every sweep rather than on a timer of its own: a deployment that
// calls Sweep once per database must prune each, and the indexed delete
// costs nothing when there is nothing to drop.
func (s *Service) prune(ctx context.Context, now time.Time) {
	if _, err := s.rules.PruneAlertEvents(ctx, now.Add(-eventRetention)); err != nil {
		log.Warn().Err(err).Msg("alerting: pruning old events failed")
	}
}

// evaluate reads a rule's latest window and moves its state. The read runs
// unlocked; the write re-reads the rule under its lock and is dropped when
// the rule was edited meanwhile, so an edit is never overwritten by an
// evaluation of what the rule used to say. A source that is down or slow
// leaves the rule where it was rather than resolving it, so one failed query
// never closes an incident a minute before reopening another; a read that
// cannot work (nothing bound, a refused query) is no data. A firing or a
// recovery that does not reach prodops leaves the rule where it was too, to
// be tried again next time.
func (s *Service) evaluate(ctx context.Context, snapshot domain.AlertRule) error {
	now := s.now()
	window := snapshot.Condition.Window()
	samples, err := s.read(ctx, snapshot.AlertRuleSpec, now.Add(-2*window), now, window)
	unreachable := false
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, datasource.ErrUpstream), errors.Is(err, context.DeadlineExceeded):
		log.Debug().Err(err).Str("rule_id", snapshot.ID.String()).Msg("alerting: the source did not answer; keeping the state")
		unreachable = true
	default:
		log.Debug().Err(err).Str("rule_id", snapshot.ID.String()).Msg("alerting: no samples")
	}
	cur, prev := pick(samples, now, window)
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
	if unreachable {
		_, _, err = s.rules.RecordAlertEvaluation(ctx, rule.ID, rule.UpdatedAt,
			domain.AlertEvaluation{State: rule.State, StateSince: rule.StateSince, Value: rule.LastValue, At: now}, nil)
		return err
	}
	state, since := advance(rule.State, rule.StateSince, rule.Condition, v, now)
	var event *domain.AlertEvent
	if state != rule.State {
		incidentID, err := s.announce(ctx, rule, state, v.value)
		if err != nil {
			log.Warn().Err(err).Str("rule_id", rule.ID.String()).Str("to", string(state)).
				Msg("alerting: the incident was not updated; trying again next evaluation")
			state, since = holdBack(rule, state, now)
		}
		if state != rule.State {
			event = &domain.AlertEvent{At: now, FromState: rule.State, ToState: state, Value: v.value, IncidentID: incidentID}
		}
	}
	_, _, err = s.rules.RecordAlertEvaluation(ctx, rule.ID, rule.UpdatedAt,
		domain.AlertEvaluation{State: state, StateSince: since, Value: v.value, At: now}, event)
	return err
}

// holdBack is where a rule stays when its firing or recovery could not be
// made: firing until the recovery goes through, pending (its for already
// served) until the firing does.
func holdBack(rule domain.AlertRule, wanted domain.AlertState, now time.Time) (domain.AlertState, time.Time) {
	switch {
	case rule.State == domain.AlertStateFiring:
		return rule.State, rule.StateSince
	case wanted == domain.AlertStateFiring && rule.State == domain.AlertStatePending:
		return rule.State, rule.StateSince
	case wanted == domain.AlertStateFiring:
		return domain.AlertStatePending, now.Add(-rule.Condition.For())
	}
	return wanted, now
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

// notify keeps to one firing and one recovery notification per rule every
// 30 minutes, as prodops does for an alert, so a flapping rule is heard
// about once.
func (s *Service) notify(ctx context.Context, rule domain.AlertRule, kind domain.NotificationKind, title, body string) {
	if s.notifier == nil || len(rule.Channels) == 0 || !s.cooldown.Allow(string(kind)+"\x00"+rule.ID.String(), s.now()) {
		return
	}
	s.notifier.NotifyVia(ctx, domain.Notification{
		Kind:     kind,
		Title:    title,
		Body:     body,
		Severity: rule.Severity,
		Link:     domain.AlertRuleLink(rule.ComponentID, rule.ID),
		Source:   domain.NotificationSourceAlert,
		SourceID: rule.ID.String(),
	}, rule.Channels)
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
