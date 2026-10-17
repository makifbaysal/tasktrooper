package datasource

import (
	"fmt"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	builtinRate     = "__rate"
	builtinInterval = "__interval"
	builtinQuantile = "__quantile"

	// DefaultQuantile is what $__quantile expands to when the caller names
	// none.
	DefaultQuantile = 0.95
)

var builtinPlaceholders = map[string]bool{builtinRate: true, builtinInterval: true, builtinQuantile: true}

// minRateWindow keeps two samples inside a rate() window at a 60 s scrape
// interval (Grafana Cloud's default); a shorter window returns no data there.
const minRateWindow = 2 * time.Minute

// RateWindow is what $__rate expands to for a step.
func RateWindow(step time.Duration) time.Duration {
	return max(2*step, minRateWindow)
}

// RenderOptions are what the built-ins expand to.
type RenderOptions struct {
	Step time.Duration
	// Quantile is $__quantile, strictly between 0 and 1; zero means
	// DefaultQuantile.
	Quantile float64
}

// dialect is one query language's placeholder rules. A param value is escaped
// for the one place the language lets a recipe put it, so render is safe only
// for a query check admitted.
type dialect interface {
	check(subject, query string, declared map[string]bool) []string
	render(query string, params map[string]string, step time.Duration, quantile float64) (string, error)
	validMetricName(name string) bool
}

// dialectFor: every kind without a language of its own speaks PromQL.
func dialectFor(kind domain.DataSourceKind) dialect {
	switch kind {
	case domain.DataSourceKindCloudWatch:
		return cloudWatchDialect{}
	case domain.DataSourceKindKafka:
		return kafkaDialect
	case domain.DataSourceKindRabbitMQ:
		return rabbitMQDialect
	case domain.DataSourceKindNATS:
		return natsDialect
	case domain.DataSourceKindLoki:
		return logQLDialect{}
	case domain.DataSourceKindTempo:
		return traceQLDialect{}
	case domain.DataSourceKindElasticsearch:
		return elasticsearchDialect{}
	case domain.DataSourceKindGooglePlayVitals:
		return playVitalsDialect
	case domain.DataSourceKindAppStoreMetrics:
		return appStoreMetricsDialect
	case domain.DataSourceKindSentry:
		return sentryDialect
	case domain.DataSourceKindPostHog:
		return postHogDialect
	case domain.DataSourceKindGA4:
		return ga4Dialect
	case domain.DataSourceKindPlausible:
		return plausibleDialect
	case domain.DataSourceKindDatadog:
		return datadogDialect{}
	case domain.DataSourceKindNewRelic:
		return nrqlDialect{}
	}
	return promQLDialect{}
}

// RenderQuery expands a PromQL recipe query (the prometheus and
// gcp_monitoring kinds) at one step with the default quantile.
func RenderQuery(query string, params map[string]string, step time.Duration) (string, error) {
	return RenderKindQuery(domain.DataSourceKindPrometheus, query, params, RenderOptions{Step: step})
}

// RenderKindQuery expands a recipe query in the query language of kind. A
// param the query references without a value is ErrInvalidInput.
func RenderKindQuery(kind domain.DataSourceKind, query string, params map[string]string, opts RenderOptions) (string, error) {
	q := opts.Quantile
	if q == 0 {
		q = DefaultQuantile
	}
	if !(q > 0 && q < 1) {
		return "", fmt.Errorf("%w: quantile %v is not between 0 and 1", ErrInvalidInput, opts.Quantile)
	}
	return dialectFor(kind).render(query, params, opts.Step, q)
}

func expand(query string, params map[string]string, builtin func(name string) (string, bool), escape func(string) string) (string, error) {
	var missing []string
	out := placeholderRegex.ReplaceAllStringFunc(query, func(m string) string {
		name := m[1:]
		if v, ok := builtin(name); ok {
			return v
		}
		v, ok := params[name]
		if !ok {
			missing = append(missing, name)
			return m
		}
		return escape(v)
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%w: no value for %s", ErrInvalidInput, strings.Join(missing, ", "))
	}
	return out, nil
}
