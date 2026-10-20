package datasource_test

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func embedded(t *testing.T) *datasource.Catalog {
	c, err := datasource.LoadCatalog(catalog.ObservabilityFS(), domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	return c
}

func TestValidateRecipeAdmitsWebPlaceholdersOnlyInWhatIsRead(t *testing.T) {
	c := embedded(t)
	recipe := func(kind domain.DataSourceKind, query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: kind, Params: []string{"host", "site", "property", "event", "project"},
			Signals: map[string]string{"web.page_views": query}}
	}
	for _, ok := range []struct {
		kind  domain.DataSourceKind
		query string
	}{
		{domain.DataSourceKindPostHog, `{"metric": "page_views", "host": "$host"}`},
		{domain.DataSourceKindPostHog, `{"metric": "events", "event": "$event"}`},
		{domain.DataSourceKindGA4, `{"property": "$property", "host": "www.$host"}`},
		{domain.DataSourceKindPlausible, `{"site": "$site", "metric": "visitors"}`},
		{domain.DataSourceKindVercel, `{"project": "$project", "metric": "lcp"}`},
	} {
		assert.NoError(t, c.ValidateRecipe(recipe(ok.kind, ok.query)), ok.query)
	}
	cases := []struct {
		name  string
		kind  domain.DataSourceKind
		query string
		want  string
	}{
		{"in the metric", domain.DataSourceKindPostHog, `{"metric": "$event"}`, "may appear only in the host or event value"},
		{"a built-in", domain.DataSourceKindGA4, `{"host": "$__interval"}`, "the kind picks the interval from the step"},
		{"a vital GA4 has not", domain.DataSourceKindGA4, `{"metric": "lcp"}`, "not one of"},
		{"site off Plausible", domain.DataSourceKindPostHog, `{"site": "x"}`, "site applies to Plausible only"},
		{"an unknown table", domain.DataSourceKindPlausible, `{"table": "queues"}`, "not one of pages, referrers"},
		{"a quantile on a count", domain.DataSourceKindPostHog, `{"metric": "sessions", "quantile": 0.5}`, "quantile applies to a web vital"},
		{"events without one", domain.DataSourceKindGA4, `{"metric": "events"}`, "needs the event"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.kind, tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestAWebDetectMetricIsAnEventName(t *testing.T) {
	c := embedded(t)
	r := domain.Recipe{ID: "r", SourceKind: domain.DataSourceKindPostHog, Detect: domain.RecipeDetect{Metric: "$web_vitals"},
		Signals: map[string]string{"web.lcp": `{"metric": "lcp"}`}}
	assert.NoError(t, c.ValidateRecipe(r))
}

func TestValidateRecipeAdmitsDatadogParamsAsTagValuesOrMetricNameParts(t *testing.T) {
	c := embedded(t)
	recipe := func(query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: domain.DataSourceKindDatadog, Params: []string{"service", "prefix"},
			Signals: map[string]string{"http.latency": query}}
	}
	for _, ok := range []string{
		`sum:$prefix.hits{service:$service,env:prod}.as_rate()`,
		`$__quantile:$prefix{service:$service} * 1000`,
		`sum:trace.$prefix.errors{service:$service} by {resource_name}.rollup(sum, $__interval)`,
	} {
		assert.NoError(t, c.ValidateRecipe(recipe(ok)), ok)
	}
	cases := []struct{ name, query, want string }{
		{"inside a tag value", `avg:x{service:web-$service}`, "may appear only as a whole tag value"},
		{"as a tag key", `avg:x{$service:web}`, "may appear only as a whole tag value"},
		{"bare", `avg:x{*} by {$service}`, "may appear only as a whole tag value"},
		{"the rate", `avg:x{*}.rollup(sum, $__rate)`, "use .as_rate()"},
		{"the quantile elsewhere", `avg:x{service:$__quantile}`, "only as the aggregator"},
		{"undeclared", `avg:x{env:$env}`, "uses $env"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestDatadogParamValuesAreCheckedNotEscaped(t *testing.T) {
	render := func(query string, params map[string]string) (string, error) {
		return datasource.RenderKindQuery(domain.DataSourceKindDatadog, query, params, datasource.RenderOptions{Step: 2 * time.Minute, Quantile: 0.99})
	}
	got, err := render(`$__quantile:$prefix{service:$service}.rollup(avg, $__interval)`, map[string]string{"prefix": "trace.flask.request", "service": "web-api"})
	require.NoError(t, err)
	assert.Equal(t, `p99:trace.flask.request{service:web-api}.rollup(avg, 120)`, got)

	for _, bad := range []map[string]string{
		{"prefix": "trace.http.request", "service": "web} by {host"},
		{"prefix": "trace.http.request", "service": "a b"},
		{"prefix": "trace{*}", "service": "web"},
		{"prefix": "trace..request", "service": "web"},
	} {
		_, err := render(`sum:$prefix.hits{service:$service}`, bad)
		assert.ErrorIs(t, err, datasource.ErrInvalidInput, bad)
	}
}

func TestValidateRecipeAdmitsNRQLParamsOnlyInComparedStrings(t *testing.T) {
	c := embedded(t)
	recipe := func(query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: domain.DataSourceKindNewRelic, Params: []string{"app"},
			Signals: map[string]string{"http.latency": query}}
	}
	for _, ok := range []string{
		`SELECT percentile(duration, $__quantile) * 1000 AS 'latency' FROM Transaction WHERE appName = '$app'`,
		`SELECT count(*) FROM Transaction WHERE appName != '$app-canary'`,
	} {
		assert.NoError(t, c.ValidateRecipe(recipe(ok)), ok)
	}
	cases := []struct{ name, query, want string }{
		{"in LIKE", `SELECT count(*) FROM Transaction WHERE appName LIKE '%$app%'`, "compared with = or !="},
		{"bare", `SELECT count(*) FROM $app`, "compared with = or !="},
		{"in a backquoted name", "SELECT count(*) FROM Transaction WHERE `$app` = 'x'", "compared with = or !="},
		{"the interval", `SELECT count(*) FROM Transaction TIMESERIES $__interval`, "adds SINCE, UNTIL and TIMESERIES"},
		{"the quantile in a string", `SELECT count(*) FROM Transaction WHERE name = '$__quantile'`, "where a number goes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestNRQLRefusesAValueThatCouldLeaveItsString(t *testing.T) {
	q := `SELECT percentile(duration, $__quantile) FROM Transaction WHERE appName = '$app'`
	got, err := datasource.RenderKindQuery(domain.DataSourceKindNewRelic, q, map[string]string{"app": `checkout "eu"`}, datasource.RenderOptions{Quantile: 0.5})
	require.NoError(t, err)
	assert.Equal(t, `SELECT percentile(duration, 50) FROM Transaction WHERE appName = 'checkout "eu"'`, got)
	for _, bad := range []string{`x' OR appName = 'y`, `a\`, "a\nb"} {
		_, err := datasource.RenderKindQuery(domain.DataSourceKindNewRelic, q, map[string]string{"app": bad}, datasource.RenderOptions{})
		assert.ErrorIs(t, err, datasource.ErrInvalidInput, bad)
	}
}

func TestEveryWebAndEndpointsTableRenders(t *testing.T) {
	c := embedded(t)
	seen := map[string][]string{}
	for _, r := range c.Recipes() {
		for _, name := range append(domain.WebTables(), domain.RecipeTableEndpoints) {
			table, ok := r.Tables[name]
			if !ok {
				continue
			}
			seen[name] = append(seen[name], r.ID)
			params := r.WithDefaults(map[string]string{})
			for _, p := range r.Params {
				if params[p] == "" {
					params[p] = "sample"
				}
			}
			for _, col := range table.ValueColumns() {
				rendered, err := datasource.RenderKindQuery(r.SourceKind, table.Queries[col.Key], params, datasource.RenderOptions{Step: time.Minute})
				require.NoError(t, err, "%s/%s/%s", r.ID, name, col.Key)
				assert.NotContains(t, rendered, "$", "%s/%s/%s", r.ID, name, col.Key)
				if _, isWeb := map[domain.DataSourceKind]bool{domain.DataSourceKindPostHog: true, domain.DataSourceKindGA4: true, domain.DataSourceKindPlausible: true}[r.SourceKind]; isWeb {
					q, err := domain.ParseWebQuery(r.SourceKind, rendered)
					require.NoError(t, err)
					assert.Equal(t, name, q.Table)
				}
			}
			if name == domain.RecipeTableEndpoints {
				assert.Equal(t, []string{"rpm", "error_pct", "p95_ms"}, columnKeys(table.ValueColumns()), r.ID)
			}
		}
	}
	assert.ElementsMatch(t, []string{"otel-http", "datadog-apm", "newrelic-apm"}, seen[domain.RecipeTableEndpoints])
	for _, name := range domain.WebTables() {
		assert.Subset(t, seen[name], []string{"posthog-web", "ga4-web", "plausible-web"}, name)
	}
}

func columnKeys(cols []domain.TableColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Key)
	}
	return out
}

func TestARecipeDefaultMustBeADeclaredParamThatRenders(t *testing.T) {
	load := func(body string) error {
		_, err := datasource.LoadCatalog(fstest.MapFS{
			"signals.yaml":   {Data: []byte("signals:\n  - {id: http.requests, kind: metric, unit: req/s, roles: [backend], aggregation: rate}\n")},
			"recipes/r.yaml": {Data: []byte(body)},
		}, domain.BuiltinDataSourceKinds())
		return err
	}
	require.NoError(t, load(`id: r
source_kind: datadog
params: [service, prefix]
defaults: {prefix: trace.http.request}
signals: {http.requests: 'sum:$prefix.hits{service:$service}.as_rate()'}`))
	assert.ErrorContains(t, load(`id: r
source_kind: datadog
params: [service]
defaults: {prefix: trace.http.request}
signals: {http.requests: 'sum:trace.http.request.hits{service:$service}'}`), "defaults.prefix is not a declared param")
	assert.ErrorContains(t, load(`id: r
source_kind: datadog
params: [service, prefix]
defaults: {prefix: "trace http"}
signals: {http.requests: 'sum:$prefix.hits{service:$service}'}`), "with its defaults")
}
