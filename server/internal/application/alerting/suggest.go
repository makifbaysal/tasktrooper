package alerting

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	historySpan = 14 * 24 * time.Hour
	historyStep = 10 * time.Minute
	// minHistory is two hours of samples: less is not a baseline.
	minHistory = 12

	thresholdDigits = 3
)

// history is the deterministic summary of a signal's last 14 days a
// template's threshold is computed from; ok is false without enough of it.
type history struct {
	ok     bool
	median float64
	p99    float64
}

type template struct {
	signal       string
	name         string
	op           domain.AlertOp
	forSeconds   int64
	window       int64
	severity     domain.IncidentSeverity
	openIncident bool
	threshold    func(h history) float64
	needsHistory bool
}

var crashFree = template{
	signal: "mobile.crash_free_users", name: "Crash-free users below 99.5%", op: domain.AlertOpBelow,
	forSeconds: 1800, window: 900, severity: domain.IncidentSeverityHigh, openIncident: true,
	threshold: func(history) float64 { return 99.5 },
}

// templates are the starting rules per role. The worker's "backlog above
// max(1000, p99×1.3) and rising for 10m" is one rising rule: rising already
// requires the value to be above its threshold.
var templates = map[domain.ComponentRole][]template{
	domain.ComponentRoleBackend: {
		{
			signal: "http.error_ratio", name: "High error ratio", op: domain.AlertOpAbove,
			forSeconds: 300, window: 300, severity: domain.IncidentSeverityHigh, openIncident: true, needsHistory: true,
			threshold: func(h history) float64 {
				if !h.ok {
					return 2
				}
				return math.Max(2, h.p99*1.3)
			},
		},
		{
			signal: "http.latency", name: "Slow responses (p95)", op: domain.AlertOpAbove,
			forSeconds: 600, window: 300, severity: domain.IncidentSeverityMedium, needsHistory: true,
			threshold: func(h history) float64 {
				if !h.ok || h.median <= 0 {
					return 1000
				}
				return 2 * h.median
			},
		},
	},
	domain.ComponentRoleWorker: {
		{
			signal: "queue.backlog", name: "Backlog growing", op: domain.AlertOpRising,
			forSeconds: 600, window: 600, severity: domain.IncidentSeverityHigh, openIncident: true, needsHistory: true,
			threshold: func(h history) float64 {
				if !h.ok {
					return 1000
				}
				return math.Max(1000, h.p99*1.3)
			},
		},
	},
	domain.ComponentRoleMobile:  {crashFree},
	domain.ComponentRoleDesktop: {crashFree},
	domain.ComponentRoleFrontend: {
		{
			signal: "web.lcp", name: "Slow page loads (LCP p75)", op: domain.AlertOpAbove,
			forSeconds: 900, window: 300, severity: domain.IncidentSeverityMedium,
			threshold: func(history) float64 { return 2500 },
		},
	},
}

// SuggestRequest is the body of POST /v1/alert-rules/suggest.
type SuggestRequest struct {
	ComponentID string `json:"component_id"`
	Environment string `json:"environment"`
}

// Suggest answers the role's starting rules for the signals the component
// environment has bound and no rule tests the same way yet, thresholds from
// the last 14 days of each signal where a template uses them (a fixed
// fallback without enough history). Nothing is saved.
func (s *Service) Suggest(ctx context.Context, req SuggestRequest) ([]domain.AlertRuleSpec, error) {
	fe := FieldErrors{}
	componentID, err := uuid.Parse(req.ComponentID)
	if err != nil {
		fe["component_id"] = datasource.FieldInvalid
	}
	env := domain.DeployEnvironment(req.Environment)
	if env == "" {
		env = domain.EnvironmentProduction
	}
	if !env.Valid() {
		fe["environment"] = datasource.FieldInvalid
	}
	var comp domain.Component
	if err == nil {
		comp, err = s.components.GetComponent(ctx, componentID)
		switch {
		case errors.Is(err, port.ErrNotFound):
			fe["component_id"] = datasource.FieldInvalid
		case err != nil:
			return nil, err
		}
	}
	if len(fe) > 0 {
		return nil, fe
	}

	bound, err := s.boundSignals(ctx, componentID, env)
	if err != nil {
		return nil, err
	}
	existing, err := s.rules.ListAlertRules(ctx, domain.AlertRuleFilter{ComponentID: &componentID, Environment: env})
	if err != nil {
		return nil, err
	}
	out := []domain.AlertRuleSpec{}
	for _, t := range templates[comp.Role.Get()] {
		if !bound[t.signal] || slices.ContainsFunc(existing, func(r domain.AlertRule) bool {
			return r.Signal == t.signal && r.Condition.Op == t.op
		}) {
			continue
		}
		spec := domain.AlertRuleSpec{
			Name:        t.name,
			ComponentID: componentID,
			Environment: env,
			Signal:      t.signal,
			Condition: domain.AlertCondition{
				Op: t.op, ForSeconds: t.forSeconds, WindowSeconds: t.window,
			},
			Severity:     t.severity,
			Channels:     s.suggestedChannels(),
			OpenIncident: t.openIncident,
			Enabled:      true,
		}
		var h history
		if t.needsHistory {
			h = s.history(ctx, spec)
		}
		spec.Condition.Threshold = roundSignificant(t.threshold(h), thresholdDigits)
		out = append(out, spec)
	}
	return out, nil
}

// suggestedChannels is the desktop notification where this server can
// show one, else none: a suggestion must save as it is.
func (s *Service) suggestedChannels() []domain.NotificationChannelKind {
	if s.channelAvailable(domain.NotificationChannelDesktop) {
		return []domain.NotificationChannelKind{domain.NotificationChannelDesktop}
	}
	return []domain.NotificationChannelKind{}
}

func (s *Service) boundSignals(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (map[string]bool, error) {
	cs, err := s.signals.List(ctx, componentID, env)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, sig := range cs.Signals {
		if sig.Status == domain.SignalStateBound {
			out[sig.Signal] = true
		}
	}
	return out, nil
}

func (s *Service) history(ctx context.Context, spec domain.AlertRuleSpec) history {
	to := s.now().Truncate(time.Minute)
	samples, _, err := s.read(ctx, spec, to.Add(-historySpan), to, historyStep)
	if err != nil {
		log.Debug().Err(err).Str("signal", spec.Signal).Msg("alerting: no history for a suggested threshold")
		return history{}
	}
	return summarizeHistory(samples)
}

// summarizeHistory: nearest-rank percentiles over the samples' values.
func summarizeHistory(samples []sample) history {
	if len(samples) < minHistory {
		return history{}
	}
	values := make([]float64, len(samples))
	for i, smp := range samples {
		values[i] = smp.value
	}
	slices.Sort(values)
	return history{ok: true, median: percentile(values, 0.5), p99: percentile(values, 0.99)}
}

func percentile(sorted []float64, q float64) float64 {
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[min(max(rank, 0), len(sorted)-1)]
}

func roundSignificant(x float64, digits int) float64 {
	if x == 0 || math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	magnitude := math.Pow(10, float64(digits)-math.Ceil(math.Log10(math.Abs(x))))
	return math.Round(x*magnitude) / magnitude
}
