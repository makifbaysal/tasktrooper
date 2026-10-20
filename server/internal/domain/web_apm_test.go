package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWebQueryFillsTheDefaultsOfASeriesAndATable(t *testing.T) {
	q, err := ParseWebQuery(DataSourceKindPostHog, `{}`)
	require.NoError(t, err)
	assert.Equal(t, WebQuery{Metric: WebMetricPageViews}, q)

	q, err = ParseWebQuery(DataSourceKindPostHog, `{"metric": "inp"}`)
	require.NoError(t, err)
	assert.Equal(t, DefaultWebVitalQuantile, q.Quantile)

	q, err = ParseWebQuery(DataSourceKindGA4, `{"table": "countries", "property": "42"}`)
	require.NoError(t, err)
	assert.Equal(t, WebQuery{Table: RecipeTableCountries, Property: "42", Days: DefaultWebTableDays, Limit: DefaultWebTableLimit}, q)
	assert.True(t, q.IsTable())
}

func TestParseWebQueryRefusesWhatItsKindCannotRead(t *testing.T) {
	cases := []struct {
		name string
		kind DataSourceKind
		raw  string
		want string
	}{
		{"not a web kind", DataSourceKindPrometheus, `{}`, "not a web analytics kind"},
		{"unknown key", DataSourceKindPostHog, `{"hogql": "SELECT 1"}`, "unknown field"},
		{"errors on Plausible", DataSourceKindPlausible, `{"metric": "js_errors"}`, "not one of"},
		{"property off GA4", DataSourceKindPlausible, `{"property": "1"}`, "Google Analytics only"},
		{"a spaced host", DataSourceKindGA4, `{"host": "a b"}`, "not a hostname"},
		{"days on a series", DataSourceKindPostHog, `{"days": 3}`, "table only"},
		{"too many days", DataSourceKindPostHog, `{"table": "pages", "days": 91}`, "days must be 1 to 90"},
		{"a quantile of one", DataSourceKindPostHog, `{"metric": "lcp", "quantile": 1}`, "between 0 and 1"},
		{"a metric on a table", DataSourceKindGA4, `{"table": "pages", "metric": "sessions"}`, "takes no metric"},
		{"project off Vercel", DataSourceKindPlausible, `{"project": "prj_1"}`, "Vercel only"},
		{"a Vercel session", DataSourceKindVercel, `{"metric": "sessions"}`, "not one of"},
		{"a spaced Vercel project", DataSourceKindVercel, `{"project": "prj 1"}`, "not a Vercel project id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseWebQuery(tc.kind, tc.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestWithDefaultsFillsOnlyWhatIsEmpty(t *testing.T) {
	r := Recipe{Defaults: map[string]string{"prefix": "trace.http.request", "env": "prod"}}
	got := r.WithDefaults(map[string]string{"prefix": "trace.flask.request", "env": " ", "service": "api"})
	assert.Equal(t, map[string]string{"prefix": "trace.flask.request", "env": "prod", "service": "api"}, got)
	assert.Equal(t, map[string]string{}, Recipe{}.WithDefaults(nil))
}

func TestParseWebQueryReadsAVercelProjectsVitals(t *testing.T) {
	q, err := ParseWebQuery(DataSourceKindVercel, `{"metric": "lcp", "project": "prj_abc"}`)
	require.NoError(t, err)
	assert.Equal(t, WebQuery{Metric: WebMetricLCP, Project: "prj_abc", Quantile: DefaultWebVitalQuantile}, q)
}
