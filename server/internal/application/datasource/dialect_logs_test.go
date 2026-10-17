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

const logSignals = `signals:
  - {id: logs.lines, kind: log, unit: lines/s, roles: [backend], aggregation: rate}
  - {id: traces.spans, kind: trace, unit: spans/s, roles: [backend], aggregation: rate}
  - {id: process.errors, kind: metric, unit: errors/s, roles: [worker], aggregation: rate}
`

func logCatalog(t *testing.T) *datasource.Catalog {
	t.Helper()
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(logSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	return c
}

func logRecipe(kind domain.DataSourceKind, signal, query string) domain.Recipe {
	return domain.Recipe{ID: "r", SourceKind: kind, Params: []string{"service", "route"}, Signals: map[string]string{signal: query}}
}

func TestValidateRecipeHoldsEachLogAndTraceLanguageToItsPlaceholderRule(t *testing.T) {
	c := logCatalog(t)
	ok := []struct {
		kind   domain.DataSourceKind
		signal string
		query  string
	}{
		{domain.DataSourceKindLoki, "logs.lines", `{service_name="$service"}`},
		{domain.DataSourceKindLoki, "logs.lines", `{service_name!="$service", env="prod"} |= "timeout" | json`},
		{domain.DataSourceKindLoki, "process.errors", `sum(rate({service_name="$service"} | detected_level=~"(?i)error" [$__rate]))`},
		{domain.DataSourceKindTempo, "traces.spans", `{ resource.service.name = "$service" && span.http.route != "$route" }`},
		{domain.DataSourceKindElasticsearch, "logs.lines", `{"filters": {"service.name": "$service", "url.path": "/v1/$route"}}`},
		{domain.DataSourceKindElasticsearch, "logs.lines", `{"filters": {}}`},
	}
	for _, tc := range ok {
		assert.NoError(t, c.ValidateRecipe(logRecipe(tc.kind, tc.signal, tc.query)), tc.query)
	}

	refused := []struct {
		name   string
		kind   domain.DataSourceKind
		signal string
		query  string
		want   string
	}{
		{"logql line filter", domain.DataSourceKindLoki, "logs.lines", `{app="api"} |= "$service"`, "must sit inside a double-quoted label value"},
		{"logql label filter stage", domain.DataSourceKindLoki, "logs.lines", `{app="api"} | level="$service"`, "must sit inside a double-quoted label value"},
		{"logql regex matcher", domain.DataSourceKindLoki, "logs.lines", `{app=~"$service"}`, "must sit inside a double-quoted label value"},
		{"logql rate outside a range", domain.DataSourceKindLoki, "process.errors", `rate({app="$service"}[5m]) > $__rate`, "only inside [...]"},
		{"logql undeclared", domain.DataSourceKindLoki, "logs.lines", `{app="$job"}`, "uses $job"},
		{"traceql regex", domain.DataSourceKindTempo, "traces.spans", `{ resource.service.name =~ "$service" }`, "compared with = or !="},
		{"traceql bare", domain.DataSourceKindTempo, "traces.spans", `{ resource.service.name = $service }`, "compared with = or !="},
		{"traceql built-in", domain.DataSourceKindTempo, "traces.spans", `{ duration > "$__interval" }`, "has no meaning in a TraceQL search"},
		{"es key", domain.DataSourceKindElasticsearch, "logs.lines", `{"filters": {"$service": "x"}}`, "may not sit in a JSON key"},
		{"es bare", domain.DataSourceKindElasticsearch, "logs.lines", `{"filters": {"a": $service}}`, "must sit inside a JSON string"},
		{"es outside the filters", domain.DataSourceKindElasticsearch, "logs.lines", `{"filters": {}, "index": "$service"}`, "only in a filter value"},
		{"es built-in", domain.DataSourceKindElasticsearch, "logs.lines", `{"filters": {"a": "$__rate"}}`, "has no meaning in an Elasticsearch query"},
		{"es unknown key", domain.DataSourceKindElasticsearch, "logs.lines", `{"query": {"match_all": {}}}`, "unknown field"},
		{"es not a field", domain.DataSourceKindElasticsearch, "logs.lines", `{"filters": {"a b": "x"}}`, "not a field name"},
		{"es not json", domain.DataSourceKindElasticsearch, "logs.lines", `service.name:$service`, "must sit inside a JSON string"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(logRecipe(tc.kind, tc.signal, tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLogAndTraceParamsAreEscapedForTheStringTheySitIn(t *testing.T) {
	evil := `a"} or {x="b\`
	tests := []struct {
		name  string
		kind  domain.DataSourceKind
		query string
		want  string
	}{
		{"logql", domain.DataSourceKindLoki, `{service_name="$service"}`, `{service_name="a\"} or {x=\"b\\"}`},
		{"traceql", domain.DataSourceKindTempo, `{ resource.service.name = "$service" }`, `{ resource.service.name = "a\"} or {x=\"b\\" }`},
		{"logql rate", domain.DataSourceKindLoki, `rate({app="x"}[$__rate])`, `rate({app="x"}[120s])`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := datasource.RenderKindQuery(tt.kind, tt.query, map[string]string{"service": evil}, datasource.RenderOptions{Step: time.Minute})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	got, err := datasource.RenderKindQuery(domain.DataSourceKindElasticsearch, `{"filters": {"service.name": "$service"}}`,
		map[string]string{"service": `x", "other": "y`}, datasource.RenderOptions{Step: time.Minute})
	require.NoError(t, err)
	q, err := domain.ParseElasticsearchQuery(got)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"service.name": `x", "other": "y`}, q.Filters)
}

func TestParamFieldsReadEachLanguagesEqualityComparisons(t *testing.T) {
	tests := []struct {
		name  string
		kind  domain.DataSourceKind
		query string
		want  []datasource.ParamField
	}{
		{"promql", domain.DataSourceKindPrometheus, `rate(x{job="$job", env!="$env"}[5m])`, []datasource.ParamField{{Param: "job", Field: "job"}}},
		{"logql", domain.DataSourceKindLoki, `sum(rate({service_name = "$service"} |= "$x" [1m]))`, []datasource.ParamField{{Param: "service", Field: "service_name"}}},
		{"traceql", domain.DataSourceKindTempo, `{ resource.service.name = "$service" && .http.route = "$route" }`, []datasource.ParamField{
			{Param: "service", Field: "resource.service.name"}, {Param: "route", Field: ".http.route"},
		}},
		{"elasticsearch", domain.DataSourceKindElasticsearch, `{"filters": {"service.name": "$service", "env": "prod-$env"}}`, []datasource.ParamField{{Param: "service", Field: "service.name"}}},
		{"cloudwatch", domain.DataSourceKindCloudWatch, `{"namespace": "AWS/SQS", "metric": "m", "stat": "Sum", "dimensions": {"QueueName": "$queue"}}`, nil},
		{"kafka", domain.DataSourceKindKafka, `{"group": "$group"}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, datasource.ParamFields(tt.kind, tt.query))
		})
	}
}

func TestALogOrTraceDetectMetricIsALabelFieldOrTag(t *testing.T) {
	c := logCatalog(t)
	recipe := func(kind domain.DataSourceKind, metric, signal, query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: kind, Detect: domain.RecipeDetect{Metric: metric}, Signals: map[string]string{signal: query}}
	}
	assert.NoError(t, c.ValidateRecipe(recipe(domain.DataSourceKindLoki, "service_name", "logs.lines", `{app="x"}`)))
	assert.ErrorContains(t, c.ValidateRecipe(recipe(domain.DataSourceKindLoki, "service.name", "logs.lines", `{app="x"}`)), "detect.metric")
	assert.NoError(t, c.ValidateRecipe(recipe(domain.DataSourceKindTempo, "resource.service.name", "traces.spans", `{ }`)))
	assert.ErrorContains(t, c.ValidateRecipe(recipe(domain.DataSourceKindTempo, "service_name", "traces.spans", `{ }`)), "detect.metric")
	assert.NoError(t, c.ValidateRecipe(recipe(domain.DataSourceKindElasticsearch, "service.name", "logs.lines", `{"filters": {}}`)))
}

func TestTheLogAndTraceRecipesShipAndRender(t *testing.T) {
	c, err := datasource.LoadCatalog(catalog.ObservabilityFS(), domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	byID := map[string]domain.Recipe{}
	for _, r := range c.Recipes() {
		byID[r.ID] = r
	}
	tests := []struct {
		recipe string
		kind   domain.DataSourceKind
		signal string
		want   string
	}{
		{"loki-service", domain.DataSourceKindLoki, domain.SignalLogsLines, `{service_name="api"}`},
		{"loki-app", domain.DataSourceKindLoki, domain.SignalLogsLines, `{app="api"}`},
		{"elasticsearch-service", domain.DataSourceKindElasticsearch, domain.SignalLogsLines, `{"filters": {"service.name": "api"}}`},
		{"tempo-service", domain.DataSourceKindTempo, domain.SignalTracesSpans, `{ resource.service.name = "api" }`},
	}
	for _, tt := range tests {
		t.Run(tt.recipe, func(t *testing.T) {
			r, ok := byID[tt.recipe]
			require.True(t, ok)
			assert.Equal(t, tt.kind, r.SourceKind)
			params := map[string]string{}
			for _, p := range r.Params {
				params[p] = "api"
			}
			got, err := datasource.RenderKindQuery(r.SourceKind, r.Signals[tt.signal], params, datasource.RenderOptions{Step: time.Minute})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	errorsFromLogs, err := datasource.RenderKindQuery(domain.DataSourceKindLoki, byID["loki-service"].Signals["process.errors"],
		map[string]string{"service": "api"}, datasource.RenderOptions{Step: time.Minute})
	require.NoError(t, err)
	assert.Equal(t, `sum(rate({service_name="api"} | detected_level=~"(?i)(error|err|critical|crit|fatal|panic|alert|emerg)" [120s]))`, errorsFromLogs)
}
