package opsai

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// instrumentationNeedles: a library or framework whose lowercased name holds
// one of these, or a linked observability resource of that vendor, counts.
var instrumentationNeedles = map[string][]string{
	domain.InstrumentationOTel:        {"opentelemetry", "otel"},
	domain.InstrumentationSentry:      {"sentry"},
	domain.InstrumentationDatadog:     {"dd-trace", "ddtrace", "datadog", "dd-sdk"},
	domain.InstrumentationPrometheus:  {"prometheus", "prom-client", "prom_client"},
	domain.InstrumentationPostHog:     {"posthog"},
	domain.InstrumentationCrashlytics: {"crashlytics"},
	domain.InstrumentationNewRelic:    {"newrelic", "new-relic"},
	domain.InstrumentationWebVitals:   {"web-vitals", "web_vitals"},
}

const (
	logsSignal   = "logs.lines"
	queuesPrefix = "queue."
)

// Readiness reads which signals of the component environment are bound,
// which instrumentation its code already carries, and what is missing.
func (s *Service) Readiness(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ObservabilityReadiness, error) {
	if !env.Valid() {
		return domain.ObservabilityReadiness{}, FieldErrors{"env": datasource.FieldInvalid}
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return domain.ObservabilityReadiness{}, err
	}
	out := domain.ObservabilityReadiness{
		ComponentID:     comp.ID,
		Environment:     env,
		Role:            comp.Role.Get(),
		Signals:         []domain.ReadinessSignal{},
		Instrumentation: s.instrumentation(ctx, comp),
		Suggestions:     []domain.ReadinessSuggestion{},
	}
	bound := s.boundEnvironment(ctx, comp, env)
	if bound != nil {
		out.Provider = bound.Provider
		out.Analytics = s.analyticsOf(ctx, *bound)
	}
	if s.signals != nil {
		list, err := s.signals.List(ctx, comp.ID, env)
		if err != nil {
			return domain.ObservabilityReadiness{}, err
		}
		for _, sig := range list.Signals {
			out.Signals = append(out.Signals, domain.ReadinessSignal{Signal: sig.Signal, Status: sig.Status, Golden: sig.Golden})
		}
	}
	out.Suggestions = suggestions(out)
	return out, nil
}

func (s *Service) instrumentation(ctx context.Context, comp domain.Component) map[string]bool {
	out := make(map[string]bool, len(instrumentationNeedles))
	for key := range instrumentationNeedles {
		out[key] = false
	}
	stack := comp.Stack.Get()
	var names []string
	for _, group := range [][]domain.StackItem{stack.Libraries, stack.Frameworks} {
		for _, it := range group {
			names = append(names, strings.ToLower(it.Name))
		}
	}
	names = append(names, s.observabilityVendors(ctx, comp)...)
	for key, needles := range instrumentationNeedles {
		for _, n := range names {
			for _, needle := range needles {
				if strings.Contains(n, needle) {
					out[key] = true
				}
			}
		}
	}
	return out
}

// observabilityVendors are the vendors of the observability resources the
// discovery scan linked the component to (sentry, datadog, posthog, …).
func (s *Service) observabilityVendors(ctx context.Context, comp domain.Component) []string {
	if s.links == nil {
		return nil
	}
	links, err := s.links.ListLinks(ctx, comp.RepositoryID)
	if err != nil {
		log.Debug().Err(err).Str("component", comp.ID.String()).Msg("opsai: reading links failed")
		return nil
	}
	var ids []uuid.UUID
	for _, l := range links {
		if l.FromComponentID == comp.ID && l.ToResourceID != nil && l.Status != domain.LinkDismissed && !l.Missing {
			ids = append(ids, *l.ToResourceID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	resources, err := s.links.ListResources(ctx, ids)
	if err != nil {
		log.Debug().Err(err).Str("component", comp.ID.String()).Msg("opsai: reading resources failed")
		return nil
	}
	var out []string
	for _, r := range resources {
		if r.Kind == domain.ResourceObservability && r.Vendor != "" {
			out = append(out, strings.ToLower(r.Vendor))
		}
	}
	return out
}

// boundEnvironment is the environment's confirmed binding to a cloud, nil
// without one.
func (s *Service) boundEnvironment(ctx context.Context, comp domain.Component, env domain.DeployEnvironment) *domain.ComponentEnvironment {
	if s.environments == nil {
		return nil
	}
	envs, err := s.environments.ListEnvironments(ctx, comp.RepositoryID)
	if err != nil {
		return nil
	}
	for _, e := range envs {
		if e.ComponentID == comp.ID && e.Environment == env && e.Status == domain.LinkConfirmed && e.Bound() {
			return &e
		}
	}
	return nil
}

// analyticsOf is whether the provider measures the project, for a provider
// that has to be told to (Vercel); nil otherwise.
func (s *Service) analyticsOf(ctx context.Context, env domain.ComponentEnvironment) *domain.CloudAnalytics {
	if s.analytics == nil || env.Provider != domain.CloudVercel {
		return nil
	}
	a, ok := s.analytics.Analytics(ctx, env.ID)
	if !ok {
		return nil
	}
	return &a
}

func suggestions(r domain.ObservabilityReadiness) []domain.ReadinessSuggestion {
	out := []domain.ReadinessSuggestion{}
	add := func(goal domain.ObservabilityGoal, code string) {
		out = append(out, domain.ReadinessSuggestion{Goal: goal, Code: code})
	}
	golden, goldenBound := 0, 0
	logsUnbound := false
	queuesUnbound := false
	for _, sig := range r.Signals {
		if sig.Golden {
			golden++
			if sig.Status == domain.SignalStateBound {
				goldenBound++
			}
		}
		if sig.Signal == logsSignal && sig.Status != domain.SignalStateBound {
			logsUnbound = true
		}
		if strings.HasPrefix(sig.Signal, queuesPrefix) && sig.Status != domain.SignalStateBound {
			queuesUnbound = true
		}
	}
	if a := r.Analytics; a != nil && (a.WebAnalytics == domain.AnalyticsNotEnabled || a.SpeedInsights == domain.AnalyticsNotEnabled) {
		add(domain.GoalRUM, "analytics_not_enabled")
	}
	switch {
	case golden > 0 && goldenBound == 0:
		add(domain.GoalMetrics, "golden_unbound")
	case goldenBound < golden:
		add(domain.GoalMetrics, "golden_partly_unbound")
	}
	has := func(keys ...string) bool {
		return slices.ContainsFunc(keys, func(k string) bool { return r.Instrumentation[k] })
	}
	server := r.Role == domain.ComponentRoleBackend || r.Role == domain.ComponentRoleWorker
	if server && !has(domain.InstrumentationOTel, domain.InstrumentationDatadog, domain.InstrumentationNewRelic) {
		add(domain.GoalTraces, "no_tracing_sdk")
	}
	if server && logsUnbound && r.Provider == "" {
		add(domain.GoalLogs, "no_log_source")
	}
	if r.Role == domain.ComponentRoleFrontend &&
		!has(domain.InstrumentationWebVitals, domain.InstrumentationSentry, domain.InstrumentationPostHog, domain.InstrumentationDatadog) {
		add(domain.GoalRUM, "no_rum")
	}
	if (r.Role == domain.ComponentRoleMobile || r.Role == domain.ComponentRoleDesktop) &&
		!has(domain.InstrumentationSentry, domain.InstrumentationCrashlytics, domain.InstrumentationDatadog) {
		add(domain.GoalCrash, "no_crash_sdk")
	}
	if queuesUnbound {
		add(domain.GoalQueues, "queues_unbound")
	}
	return out
}
