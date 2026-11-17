package datasource_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestValidateRecipeAdmitsTelemetryPlaceholdersOnlyInWhatIsRead(t *testing.T) {
	c := embedded(t)
	recipe := func(query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: domain.DataSourceKindTaskTrooperTelemetry, Params: []string{"service", "env", "ep"},
			Signals: map[string]string{"http.latency": query}}
	}
	for _, ok := range []string{
		`{"service": "$service"}`,
		`{"service": "$service", "env": "$env", "endpoint": "$ep", "metric": "error_ratio"}`,
		`{"service": "$service", "metric": "latency", "quantile": "$__quantile"}`,
		`{"service": "$service", "metric": "latency", "quantile": "0.99"}`,
	} {
		assert.NoError(t, c.ValidateRecipe(recipe(ok)), ok)
	}
	cases := []struct {
		name, query, want string
	}{
		{"in the metric", `{"service": "x", "metric": "$service"}`, "may appear only in the service, env, endpoint value"},
		{"quantile elsewhere", `{"service": "$__quantile"}`, "only as the whole quantile value"},
		{"quantile with more around it", `{"service": "x", "quantile": "0.$__quantile"}`, "only as the whole quantile value"},
		{"another built-in", `{"service": "$__rate"}`, "telemetry picks its own buckets"},
		{"outside a string", `{"service": $service}`, "must sit inside a JSON string"},
		{"an unknown metric", `{"service": "x", "metric": "cpu"}`, "not one of"},
		{"no service", `{"env": "production"}`, "service is required"},
		{"an unknown key", `{"service": "x", "table": "y"}`, "unknown field"},
		{"a bad quantile", `{"service": "x", "quantile": "95"}`, "not a fraction"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestTelemetryRecipeRendersEverySignal(t *testing.T) {
	c := embedded(t)
	var r domain.Recipe
	for _, x := range c.Recipes() {
		if x.ID == "tasktrooper-telemetry" {
			r = x
		}
	}
	require.Equal(t, domain.DataSourceKindTaskTrooperTelemetry, r.SourceKind)
	for _, sig := range []string{"http.requests", "http.error_ratio", "http.latency", "process.errors", "logs.lines", "traces.spans", "errors.issues"} {
		assert.Contains(t, r.Signals, sig)
	}
	params := r.WithDefaults(map[string]string{"service": `or"ders`})
	for id, q := range r.Signals {
		rendered, err := datasource.RenderKindQuery(r.SourceKind, q, params, datasource.RenderOptions{Step: time.Minute, Quantile: 0.99})
		require.NoError(t, err, id)
		parsed, err := domain.ParseTelemetryQuery(rendered)
		require.NoError(t, err, id)
		assert.Equal(t, `or"ders`, parsed.Service, id)
		assert.Empty(t, parsed.Env, "the default reads every environment")
		if id == "http.latency" {
			v, _ := parsed.QuantileValue()
			assert.InDelta(t, 0.99, v, 1e-9)
		}
	}
	assert.Equal(t, []datasource.ParamField{
		{Param: "service", Field: domain.TelemetryLabelService},
		{Param: "env", Field: domain.TelemetryLabelEnvironment},
	}, datasource.ParamFields(r.SourceKind, r.Signals["http.requests"]))
}
