package domain

import "github.com/google/uuid"

// ObservabilityGoal is one thing "enable observability" can ask for.
type ObservabilityGoal string

const (
	GoalMetrics ObservabilityGoal = "metrics"
	GoalTraces  ObservabilityGoal = "traces"
	GoalLogs    ObservabilityGoal = "logs"
	GoalRUM     ObservabilityGoal = "rum"
	GoalCrash   ObservabilityGoal = "crash"
	GoalQueues  ObservabilityGoal = "queues"
)

func ObservabilityGoals() []ObservabilityGoal {
	return []ObservabilityGoal{GoalMetrics, GoalTraces, GoalLogs, GoalRUM, GoalCrash, GoalQueues}
}

// ObservabilityTarget is where the instrumented component sends telemetry.
type ObservabilityTarget string

const (
	// TargetOwnCloud: the cloud the component already runs on (GCP's
	// Telemetry API, AWS's ADOT collector).
	TargetOwnCloud ObservabilityTarget = "own_cloud"
	// TargetSelfHosted: a Grafana LGTM stack the team runs itself.
	TargetSelfHosted ObservabilityTarget = "self_hosted"
	// TargetOTLPEndpoint: an OTLP endpoint the person names.
	TargetOTLPEndpoint ObservabilityTarget = "otlp_endpoint"
)

func ValidObservabilityTarget(t ObservabilityTarget) bool {
	return t == TargetOwnCloud || t == TargetSelfHosted || t == TargetOTLPEndpoint
}

// Instrumentation keys: what the readiness helper looks for in a
// component's libraries and linked observability resources.
const (
	InstrumentationOTel        = "otel"
	InstrumentationSentry      = "sentry"
	InstrumentationDatadog     = "datadog"
	InstrumentationPrometheus  = "prometheus"
	InstrumentationPostHog     = "posthog"
	InstrumentationCrashlytics = "crashlytics"
	InstrumentationNewRelic    = "newrelic"
	InstrumentationWebVitals   = "web_vitals"
)

type ReadinessSignal struct {
	Signal string             `json:"signal"`
	Status SignalBindingState `json:"status"`
	Golden bool               `json:"golden"`
}

// ReadinessSuggestion is a gap, as a code the UI words: golden_unbound,
// golden_partly_unbound, no_tracing_sdk, no_log_source, no_rum,
// no_crash_sdk, queues_unbound, analytics_not_enabled (a Vercel project
// with Web Analytics or Speed Insights switched off).
type ReadinessSuggestion struct {
	Goal ObservabilityGoal `json:"goal"`
	Code string            `json:"code"`
}

// ObservabilityReadiness is GET /v1/components/:id/observability.
type ObservabilityReadiness struct {
	ComponentID uuid.UUID         `json:"component_id"`
	Environment DeployEnvironment `json:"environment"`
	Role        ComponentRole     `json:"role"`
	Provider    CloudProviderKind `json:"provider"`
	// Analytics is whether a provider that must be told to measure the
	// project (Vercel) is doing so; nil for one that does not say.
	Analytics       *CloudAnalytics       `json:"analytics"`
	Signals         []ReadinessSignal     `json:"signals"`
	Instrumentation map[string]bool       `json:"instrumentation"`
	Suggestions     []ReadinessSuggestion `json:"suggestions"`
}
