package datasource_test

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var prometheusOnly = []domain.DataSourceKind{domain.DataSourceKindPrometheus}

// directBroker kinds are offered through their own consumer listing
// (port.QueueDiscoverer), not a detect metric.
var directBroker = map[domain.DataSourceKind]bool{
	domain.DataSourceKindKafka: true, domain.DataSourceKindRabbitMQ: true, domain.DataSourceKindNATS: true,
}

// boundWithoutDetection kinds list nothing to detect from: a store kind's
// recipe is offered from the repository's linked store app, a Sentry one is
// bound by hand to a project.
var boundWithoutDetection = map[domain.DataSourceKind]bool{
	domain.DataSourceKindGooglePlayVitals: true, domain.DataSourceKindAppStoreMetrics: true, domain.DataSourceKindSentry: true,
}

func TestEveryEmbeddedRecipeValidatesAndRenders(t *testing.T) {
	c, err := datasource.LoadCatalog(catalog.ObservabilityFS(), domain.BuiltinDataSourceKinds())
	require.NoError(t, err)

	recipes := c.Recipes()
	ids := make([]string, 0, len(recipes))
	for _, r := range recipes {
		ids = append(ids, r.ID)
		assert.Equal(t, domain.RecipeOriginCatalog, r.Origin)
		if !directBroker[r.SourceKind] && !boundWithoutDetection[r.SourceKind] {
			assert.NotEmpty(t, r.Detect.Metric, r.ID)
		}
		params := map[string]string{}
		for _, p := range r.Params {
			params[p] = `sam"ple`
			if r.SourceKind == domain.DataSourceKindDatadog {
				params[p] = "sample"
			}
		}
		for signal, query := range r.Signals {
			rendered, err := datasource.RenderKindQuery(r.SourceKind, query, params, datasource.RenderOptions{Step: time.Minute, Quantile: 0.99})
			require.NoError(t, err, "%s/%s", r.ID, signal)
			assert.NotContains(t, rendered, "$", "%s/%s", r.ID, signal)
			if r.SourceKind == domain.DataSourceKindCloudWatch {
				_, err := domain.ParseCloudWatchQuery(rendered)
				assert.NoError(t, err, "%s/%s", r.ID, signal)
			}
		}
	}
	assert.Subset(t, ids, []string{
		"otel-http", "rabbitmq-prometheus", "kafka-exporter", "kminion", "cadvisor",
		"gcp-cloud-run", "gcp-gke-workload", "gcp-https-loadbalancer", "gcp-pubsub",
		"aws-alb", "aws-ecs", "aws-lambda", "aws-sqs", "aws-msk",
		"kafka-direct", "rabbitmq-direct", "nats-direct",
		"play-vitals", "app-store-metrics", "sentry-mobile", "sentry-web", "sentry-backend",
		"posthog-web", "posthog-web-vitals", "ga4-web", "plausible-web",
		"datadog-apm", "datadog-system", "datadog-container", "newrelic-apm", "newrelic-browser", "newrelic-mobile",
	})
}

func TestCloudResourceRecipesFillEveryParamTheirRefCarries(t *testing.T) {
	c, err := datasource.LoadCatalog(catalog.ObservabilityFS(), domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	byID := map[string]domain.Recipe{}
	for _, r := range c.Recipes() {
		byID[r.ID] = r
	}

	cases := []struct {
		recipe string
		ref    domain.CloudResourceRef
		want   map[string]string
	}{
		{"gcp-cloud-run", domain.CloudResourceRef{
			Kind: domain.CloudResourceCloudRunService, ID: "projects/p/locations/europe-west3/services/api", Name: "api", Region: "europe-west3",
		}, map[string]string{"service": "api", "location": "europe-west3"}},
		{"gcp-gke-workload", domain.CloudResourceRef{
			Kind: domain.CloudResourceGKEWorkload, ID: "projects/p/locations/europe-west3/clusters/prod", Name: "prod", Region: "europe-west3",
		}, map[string]string{"cluster": "prod", "location": "europe-west3"}},
		{"aws-ecs", domain.CloudResourceRef{
			Kind: domain.CloudResourceECSService, ID: "arn:aws:ecs:eu-central-1:1:service/prod/api", Name: "api", Region: "eu-central-1",
			Extra: map[string]string{"cluster_name": "prod", "cluster_arn": "arn:aws:ecs:eu-central-1:1:cluster/prod"},
		}, map[string]string{"cluster": "prod", "service": "api"}},
		{"aws-lambda", domain.CloudResourceRef{
			Kind: domain.CloudResourceLambdaFunction, ID: "arn:aws:lambda:eu-central-1:1:function:resize", Name: "resize", Region: "eu-central-1",
		}, map[string]string{"function": "resize"}},
	}
	for _, tc := range cases {
		t.Run(tc.recipe, func(t *testing.T) {
			r, ok := byID[tc.recipe]
			require.True(t, ok)
			assert.Equal(t, tc.ref.Kind, r.AppliesTo.ResourceKind)
			assert.Equal(t, tc.want, r.ParamsFromResource(tc.ref))
		})
	}
}

func TestTheSignalCatalogCoversEveryRole(t *testing.T) {
	c, err := datasource.LoadCatalog(catalog.ObservabilityFS(), prometheusOnly)
	require.NoError(t, err)

	byID := map[string]domain.Signal{}
	for _, s := range c.Signals() {
		byID[s.ID] = s
	}
	for _, id := range []string{
		"http.requests", "http.error_ratio", "http.latency", "infra.cpu", "infra.memory", "infra.instances",
		"queue.backlog", "queue.backlog_age", "queue.consume_rate", "queue.publish_rate", "queue.dead_letter",
		"queue.consumers", "process.errors",
		"web.page_views", "web.sessions", "web.lcp", "web.inp", "web.cls", "web.ttfb", "web.js_errors",
		"mobile.crash_free_users", "mobile.crash_rate", "mobile.anr_rate", "mobile.cold_start", "mobile.rollout_fraction",
	} {
		assert.Contains(t, byID, id)
	}
	assert.Equal(t, domain.Signal{
		ID: "http.requests", Kind: domain.SignalMetric, Unit: "req/s",
		Roles: []domain.ComponentRole{domain.ComponentRoleBackend}, Aggregation: domain.AggregationRate,
		Better: domain.SignalHigherIsBetter,
	}, byID["http.requests"])
}

const testSignals = `signals:
  - {id: queue.backlog, kind: metric, unit: messages, roles: [worker], aggregation: gauge}
`

func TestLoadCatalogRefusesABrokenRecipe(t *testing.T) {
	cases := []struct {
		name string
		file string
		body string
		want string
	}{
		{"unknown signal", "recipes/r.yaml", `id: r
source_kind: prometheus
signals: {queue.depth: "x"}`, `signal "queue.depth" is not in the signal catalog`},
		{"undeclared placeholder", "recipes/r.yaml", `id: r
source_kind: prometheus
params: [queue]
signals: {queue.backlog: 'x{queue="$queue", vhost="$vhost"}[$__rate]'}`, "uses $vhost"},
		{"unregistered kind", "recipes/r.yaml", `id: r
source_kind: honeycomb
signals: {queue.backlog: "x"}`, `source_kind "honeycomb"`},
		{"id differs from the file name", "recipes/other.yaml", `id: r
source_kind: prometheus
signals: {queue.backlog: "x"}`, "must match the file name"},
		{"unknown field", "recipes/r.yaml", `id: r
source_kind: prometheus
detects: {metric: x}
signals: {queue.backlog: "x"}`, "detects"},
		{"param declared twice", "recipes/r.yaml", `id: r
source_kind: prometheus
params: [queue, queue]
signals: {queue.backlog: 'x{queue="$queue"}'}`, "declared twice"},
		{"no signals", "recipes/r.yaml", `id: r
source_kind: prometheus`, "no signals"},
		{"bad detect metric", "recipes/r.yaml", `id: r
source_kind: prometheus
detect: {metric: "not a metric"}
signals: {queue.backlog: "x"}`, "detect.metric"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"signals.yaml": {Data: []byte(testSignals)},
				tc.file:        {Data: []byte(tc.body)},
			}
			_, err := datasource.LoadCatalog(fsys, prometheusOnly)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoadCatalogRefusesABrokenSignal(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"unknown role", `signals: [{id: queue.backlog, kind: metric, unit: m, roles: [cron], aggregation: gauge}]`, "unknown role"},
		{"unknown aggregation", `signals: [{id: queue.backlog, kind: metric, unit: m, roles: [worker], aggregation: p99}]`, "unknown aggregation"},
		{"listed twice", `signals: [{id: a.b, kind: metric, unit: m, roles: [worker], aggregation: gauge}, {id: a.b, kind: metric, unit: m, roles: [worker], aggregation: gauge}]`, "twice"},
		{"undotted id", `signals: [{id: backlog, kind: metric, unit: m, roles: [worker], aggregation: gauge}]`, "dotted"},
		{"unknown direction", `signals: [{id: queue.backlog, kind: metric, unit: m, roles: [worker], aggregation: gauge, better: sideways}]`, "not higher or lower"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(tc.body)}}, prometheusOnly)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRenderQueryExpandsBuiltinsAndEscapesParams(t *testing.T) {
	got, err := datasource.RenderQuery(`rate(x{q="$queue"}[$__rate]) / $__interval`, map[string]string{"queue": `a"b\c`}, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, `rate(x{q="a\"b\\c"}[600s]) / 300s`, got)

	got, err = datasource.RenderQuery(`rate(x[$__rate])`, nil, 15*time.Second)
	require.NoError(t, err)
	assert.Equal(t, `rate(x[120s])`, got, "the window never drops under two scrapes at 60 s")

	_, err = datasource.RenderQuery(`x{q="$queue",v="$vhost"}`, map[string]string{"queue": "a"}, time.Minute)
	require.ErrorIs(t, err, datasource.ErrInvalidInput)
	assert.True(t, strings.Contains(err.Error(), "vhost"))
}

func TestValidateRecipeAdmitsPlaceholdersOnlyWhereTheyAreEscapedCorrectly(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(testSignals)}}, prometheusOnly)
	require.NoError(t, err)
	recipe := func(query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: domain.DataSourceKindPrometheus, Params: []string{"queue"},
			Signals: map[string]string{"queue.backlog": query}}
	}

	for _, ok := range []string{
		`sum(depth{queue="$queue"})`,
		`sum by (le) (rate(depth{queue != "pre-$queue", a=~"5.."}[$__rate]))`,
		`max_over_time(depth{queue="$queue"}[$__rate:$__interval])`,
		`label_replace(depth{queue="$queue"}, "q", "$1", "queue", "(.*)")`,
		"# don't count the retries\nsum(depth{queue=\"$queue\"}) # nor $queue's dead letters\n+ 0",
	} {
		assert.NoError(t, c.ValidateRecipe(recipe(ok)), ok)
	}

	cases := []struct {
		name, query, want string
	}{
		{"regex matcher", `depth{queue=~"$queue"}`, "must sit inside a double-quoted label value"},
		{"negative regex matcher", `depth{queue!~"$queue"}`, "must sit inside a double-quoted label value"},
		{"bare in by()", `sum by ($queue) (depth)`, "must sit inside a double-quoted label value"},
		{"bare in a range", `rate(depth[$queue])`, "must sit inside a double-quoted label value"},
		{"single-quoted value", `depth{queue='$queue'}`, "must sit inside a double-quoted label value"},
		{"string outside a selector", `label_replace(depth, "q", "$queue", "a", "b")`, "must sit inside a double-quoted label value"},
		{"comparison", `depth == "$queue"`, "must sit inside a double-quoted label value"},
		{"rate window outside brackets", `rate(depth[5m]) * $__rate`, "$__rate may appear only inside [...]"},
		{"interval in a string", `depth{queue="$__interval"}`, "$__interval may appear only inside [...]"},
		{"an undeclared name in a comment", "sum(depth{queue=\"$queue\"}) # see $other", "$other, which is neither a declared param nor a built-in"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidateRecipeAdmitsTheQuantileOnlyWhereANumberGoes(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(testSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	for _, kind := range []domain.DataSourceKind{domain.DataSourceKindPrometheus, domain.DataSourceKindGCPMonitoring} {
		recipe := func(query string) domain.Recipe {
			return domain.Recipe{ID: "r", SourceKind: kind, Signals: map[string]string{"queue.backlog": query}}
		}
		assert.NoError(t, c.ValidateRecipe(recipe(`histogram_quantile($__quantile, sum by (le) (rate(x_bucket[$__rate])))`)), kind)
		for _, bad := range []string{
			`x{q="$__quantile"}`,
			`rate(x[$__quantile])`,
			`label_replace(x, "q", "$__quantile", "a", "b")`,
		} {
			err := c.ValidateRecipe(recipe(bad))
			require.Error(t, err, "%s: %s", kind, bad)
			assert.Contains(t, err.Error(), "$__quantile may appear only where a number goes", bad)
		}
	}
}

func TestValidateRecipeAdmitsCloudWatchPlaceholdersOnlyInDimensionValues(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(testSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	recipe := func(query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: domain.DataSourceKindCloudWatch, Params: []string{"queue"},
			Signals: map[string]string{"queue.backlog": query}}
	}

	for _, ok := range []string{
		`{"namespace": "AWS/SQS", "metric": "ApproximateNumberOfMessagesVisible", "stat": "Maximum", "dimensions": {"QueueName": "$queue"}}`,
		`{"namespace": "AWS/SQS", "metric": "M", "stat": "$__quantile", "dimensions": {"QueueName": "prod-$queue"}, "period": 300}`,
		`{"expression": "100 * a / b", "metrics": {
			"a": {"namespace": "N", "metric": "A", "stat": "Sum", "dimensions": {"QueueName": "$queue"}},
			"b": {"namespace": "N", "metric": "B", "stat": "$__quantile", "dimensions": {"QueueName": "$queue"}}}}`,
		`{"expression": "SELECT AVG(CPUUtilization) FROM SCHEMA(\"AWS/EC2\", InstanceId)", "period": 300}`,
	} {
		assert.NoError(t, c.ValidateRecipe(recipe(ok)), ok)
	}

	cases := []struct {
		name, query, want string
	}{
		{"bare", `{"namespace": "N", "metric": "M", "stat": "Sum", "period": $queue}`, "$queue must sit inside a JSON string"},
		{"in the namespace", `{"namespace": "$queue", "metric": "M", "stat": "Sum"}`, "$queue may appear only in a dimension value"},
		{"in the expression", `{"expression": "a * $queue", "metrics": {"a": {"namespace": "N", "metric": "M", "stat": "Sum"}}}`, "$queue may appear only in a dimension value"},
		{"as a dimension name", `{"namespace": "N", "metric": "M", "stat": "Sum", "dimensions": {"$queue": "x"}}`, "may not sit in a JSON key"},
		{"quantile inside a stat", `{"namespace": "N", "metric": "M", "stat": "t$__quantile"}`, "$__quantile may appear only as a whole stat value"},
		{"quantile in a dimension", `{"namespace": "N", "metric": "M", "stat": "Sum", "dimensions": {"Q": "$__quantile"}}`, "$__quantile may appear only as a whole stat value"},
		{"rate window", `{"namespace": "N", "metric": "M", "stat": "Sum", "dimensions": {"Q": "$__rate"}}`, "the period follows the step"},
		{"undeclared", `{"namespace": "N", "metric": "M", "stat": "Sum", "dimensions": {"Q": "$vhost"}}`, "uses $vhost"},
		{"not JSON", `SELECT 1`, "is not JSON"},
		{"unknown key", `{"namespace": "N", "metric": "M", "stat": "Sum", "unit": "Count"}`, "unknown field"},
		{"no stat", `{"namespace": "N", "metric": "M"}`, "not a CloudWatch statistic"},
		{"bad period", `{"namespace": "N", "metric": "M", "stat": "Sum", "period": 90}`, "multiple of 60"},
		{"metrics without an expression", `{"namespace": "N", "metric": "M", "stat": "Sum", "metrics": {"a": {"namespace": "N", "metric": "M", "stat": "Sum"}}}`, "has none"},
		{"bad metric id", `{"expression": "A", "metrics": {"A": {"namespace": "N", "metric": "M", "stat": "Sum"}}}`, "must start with a lowercase letter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidateRecipeChecksTheCloudResourceItAppliesTo(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(testSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	recipe := func(a domain.RecipeAppliesTo) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: domain.DataSourceKindPrometheus, Params: []string{"queue"}, AppliesTo: a,
			Signals: map[string]string{"queue.backlog": `x{q="$queue"}`}}
	}

	assert.NoError(t, c.ValidateRecipe(recipe(domain.RecipeAppliesTo{
		ResourceKind: domain.CloudResourceECSService, ResourceParams: map[string]string{"queue": "extra.cluster_name"},
	})))
	cases := []struct {
		name string
		a    domain.RecipeAppliesTo
		want string
	}{
		{"unknown kind", domain.RecipeAppliesTo{ResourceKind: "ec2_instance"}, `resource_kind "ec2_instance"`},
		{"params without a kind", domain.RecipeAppliesTo{ResourceParams: map[string]string{"queue": "name"}}, "needs a resource_kind"},
		{"undeclared param", domain.RecipeAppliesTo{ResourceKind: domain.CloudResourceLambdaFunction, ResourceParams: map[string]string{"fn": "name"}}, `names "fn"`},
		{"bad ref path", domain.RecipeAppliesTo{ResourceKind: domain.CloudResourceLambdaFunction, ResourceParams: map[string]string{"queue": "arn"}}, "is not id, name, region or extra.<key>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.a))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestACloudWatchDetectMetricIsNamespaceSlashName(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(testSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	recipe := func(kind domain.DataSourceKind, metric string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: kind, Detect: domain.RecipeDetect{Metric: metric},
			Signals: map[string]string{"queue.backlog": `{"namespace": "AWS/SQS", "metric": "M", "stat": "Sum"}`}}
	}
	assert.NoError(t, c.ValidateRecipe(recipe(domain.DataSourceKindCloudWatch, "AWS/SQS/ApproximateNumberOfMessagesVisible")))
	assert.ErrorContains(t, c.ValidateRecipe(recipe(domain.DataSourceKindCloudWatch, "ApproximateNumberOfMessagesVisible")), "detect.metric")
}

func TestTheShippedCatalogSaysWhichWayEachJudgedSignalIsBetter(t *testing.T) {
	c, err := datasource.LoadCatalog(catalog.ObservabilityFS(), domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	better := map[string]domain.SignalDirection{}
	for _, s := range c.Signals() {
		better[s.ID] = s.Better
	}
	assert.Equal(t, domain.SignalLowerIsBetter, better[domain.SignalHTTPErrorRatio])
	assert.Equal(t, domain.SignalLowerIsBetter, better[domain.SignalHTTPLatency])
	assert.Equal(t, domain.SignalLowerIsBetter, better["queue.backlog"])
	assert.Equal(t, domain.SignalLowerIsBetter, better["mobile.cold_start"])
	assert.Equal(t, domain.SignalHigherIsBetter, better["http.requests"])
	assert.Equal(t, domain.SignalHigherIsBetter, better["mobile.crash_free_users"])
	assert.Equal(t, domain.SignalDirection(""), better["infra.instances"])
}
