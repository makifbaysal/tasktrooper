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

func TestEveryEmbeddedRecipeValidatesAndRenders(t *testing.T) {
	c, err := datasource.LoadCatalog(catalog.ObservabilityFS(), prometheusOnly)
	require.NoError(t, err)

	recipes := c.Recipes()
	ids := make([]string, 0, len(recipes))
	for _, r := range recipes {
		ids = append(ids, r.ID)
		assert.Equal(t, domain.RecipeOriginCatalog, r.Origin)
		assert.NotEmpty(t, r.Detect.Metric, r.ID)
		params := map[string]string{}
		for _, p := range r.Params {
			params[p] = "sample"
		}
		for signal, query := range r.Signals {
			rendered, err := datasource.RenderQuery(query, params, time.Minute)
			require.NoError(t, err, "%s/%s", r.ID, signal)
			assert.NotContains(t, rendered, "$", "%s/%s", r.ID, signal)
		}
	}
	assert.Subset(t, ids, []string{"otel-http", "rabbitmq-prometheus", "kafka-exporter", "kminion", "cadvisor"})
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
source_kind: datadog
signals: {queue.backlog: "x"}`, `source_kind "datadog"`},
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
