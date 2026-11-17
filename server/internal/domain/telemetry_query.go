package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// DataSourceKindTaskTrooperTelemetry reads TaskTrooper Telemetry through the
// one connection saved in Settings → TaskTrooper Telemetry: a source of this
// kind holds no URL and no key of its own.
const DataSourceKindTaskTrooperTelemetry DataSourceKind = "tasktrooper_telemetry"

// The labels a telemetry source lists values of, for the detector: the
// OpenTelemetry resource attributes telemetry groups everything by.
const (
	TelemetryLabelService     = "service.name"
	TelemetryLabelEnvironment = "deployment.environment"
)

// TelemetryAllEnvironments as a query's env reads every environment.
const TelemetryAllEnvironments = "*"

// Telemetry metrics: requests and errors are per second, error_ratio is
// percent, latency is ms at the query's quantile; retries and dead_letters
// are a consumer process's, per second.
const (
	TelemetryMetricRequests    = "requests"
	TelemetryMetricErrorRatio  = "error_ratio"
	TelemetryMetricErrors      = "errors"
	TelemetryMetricLatency     = "latency"
	TelemetryMetricRetries     = "retries"
	TelemetryMetricDeadLetters = "dead_letters"
)

var telemetryMetrics = []string{
	TelemetryMetricRequests, TelemetryMetricErrorRatio, TelemetryMetricErrors,
	TelemetryMetricLatency, TelemetryMetricRetries, TelemetryMetricDeadLetters,
}

// TelemetryQuery is the query language of the tasktrooper_telemetry kind:
//
//	{"service": "orders-api", "env": "production", "metric": "latency", "quantile": "0.95"}
//
// Service is telemetry's service.name; Env its deployment.environment, where
// "" or "*" reads every environment. Endpoint narrows a series to one
// endpoint id (get-orders-id) or consumer process. Metric (a series; default
// requests) is one of the TelemetryMetric constants; Quantile (latency only,
// default 0.95) is a fraction. A logs, errors, traces or endpoints table read
// uses Service, Env and Endpoint and ignores the rest.
type TelemetryQuery struct {
	Service  string `json:"service"`
	Env      string `json:"env,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Metric   string `json:"metric,omitempty"`
	Quantile string `json:"quantile,omitempty"`
}

func ParseTelemetryQuery(raw string) (TelemetryQuery, error) {
	var q TelemetryQuery
	if err := decodeBrokerQuery(raw, "TaskTrooper Telemetry", &q); err != nil {
		return TelemetryQuery{}, err
	}
	for _, f := range []struct {
		key, v   string
		required bool
	}{{"service", q.Service, true}, {"env", q.Env, false}, {"endpoint", q.Endpoint, false}} {
		if err := checkBrokerName(f.key, f.v, f.required); err != nil {
			return TelemetryQuery{}, err
		}
	}
	if q.Env == TelemetryAllEnvironments {
		q.Env = ""
	}
	m, err := brokerMetric(q.Metric, TelemetryMetricRequests, telemetryMetrics)
	if err != nil {
		return TelemetryQuery{}, err
	}
	q.Metric = m
	if _, err := q.QuantileValue(); err != nil {
		return TelemetryQuery{}, err
	}
	return q, nil
}

// QuantileValue is Quantile as a number, 0.95 when unset.
func (q TelemetryQuery) QuantileValue() (float64, error) {
	if strings.TrimSpace(q.Quantile) == "" {
		return 0.95, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(q.Quantile), 64)
	if err != nil || !(v > 0 && v < 1) {
		return 0, fmt.Errorf("quantile %q is not a fraction between 0 and 1", q.Quantile)
	}
	return v, nil
}

// TelemetryEnvironmentNames are the deployment.environment values that name
// env, most common first: what the detector matches a telemetry environment
// against.
func TelemetryEnvironmentNames(env DeployEnvironment) []string {
	switch env {
	case EnvironmentProduction:
		return []string{"production", "prod", "prd", "live"}
	case EnvironmentStaging:
		return []string{"staging", "stage", "stg", "preprod", "pre-prod", "uat"}
	case EnvironmentPreview:
		return []string{"preview", "pr", "review"}
	case EnvironmentDevelopment:
		return []string{"development", "dev", "local"}
	}
	return nil
}
