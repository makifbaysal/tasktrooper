package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestParsePlayVitalsQueryFillsDefaultsAndRefusesWhatPlayCannotAnswer(t *testing.T) {
	q, err := domain.ParsePlayVitalsQuery(`{"package":"com.acme.app","metric_set":"crashRateMetricSet","metric":"crashRate"}`)
	require.NoError(t, err)
	assert.Equal(t, domain.PlayPeriodDaily, q.Period)
	assert.Equal(t, domain.PlayAsValue, q.As)
	assert.False(t, q.IsTable())

	q, err = domain.ParsePlayVitalsQuery(`{"issues":"anr"}`)
	require.NoError(t, err)
	assert.True(t, q.IsTable())
	assert.Equal(t, domain.DefaultPlayIssueDays, q.Days)

	cases := []struct{ name, query, want string }{
		{"no metric set", `{"metric":"crashRate"}`, "metric_set is required"},
		{"both kinds of read", `{"metric_set":"crashRateMetricSet","metric":"crashRate","issues":"all"}`, "not both"},
		{"hourly rolling average", `{"metric_set":"crashRateMetricSet","metric":"crashRate7dUserWeighted","period":"hourly"}`, "rolling average"},
		{"an unknown scale", `{"metric_set":"crashRateMetricSet","metric":"crashRate","as":"permille"}`, "as \"permille\""},
		{"an unknown issue type", `{"issues":"oom"}`, "is not one of"},
		{"too many days", `{"issues":"all","days":90}`, "days must be 1 to 30"},
		{"days on a series", `{"metric_set":"crashRateMetricSet","metric":"crashRate","days":2}`, "issues table only"},
		{"an unknown key", `{"metric_set":"crashRateMetricSet","metric":"crashRate","window":"7d"}`, "unknown field"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domain.ParsePlayVitalsQuery(tt.query)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
	assert.True(t, domain.ValidPlayPackage("com.acme.app_2"))
	assert.False(t, domain.ValidPlayPackage("acme"))
}

func TestParseAppStoreMetricsQueryTellsAMetricFromACategory(t *testing.T) {
	q, err := domain.ParseAppStoreMetricsQuery(`{"app":"1234567890","metric":"memory"}`)
	require.NoError(t, err)
	assert.Equal(t, "MEMORY", q.ASCCategory())
	assert.Equal(t, domain.DefaultASCDevice, q.Device)

	q, err = domain.ParseAppStoreMetricsQuery(`{"metric":"launchTime","percentile":"p50"}`)
	require.NoError(t, err)
	assert.Empty(t, q.ASCCategory())

	for _, bad := range []string{
		`{"app":"1"}`,
		`{"metric":"launchTime","build":"b1"}`,
		`{"diagnostics":"hangs","percentile":"p90"}`,
		`{"metric":"launch time"}`,
	} {
		_, err := domain.ParseAppStoreMetricsQuery(bad)
		assert.Error(t, err, bad)
	}
	assert.True(t, domain.ValidASCAppID("1234567890"))
	assert.False(t, domain.ValidASCAppID("com.acme.app"))
}

func TestParseSentryQueryDefaultsToEventsAndKeepsTheProjectASlug(t *testing.T) {
	q, err := domain.ParseSentryQuery(`{"project":"ios-app"}`)
	require.NoError(t, err)
	assert.Equal(t, domain.SentryMetricEvents, q.Metric)
	assert.Equal(t, domain.DefaultSentryIssueDays, q.Days)
	assert.False(t, domain.SentrySessionMetric(q.Metric))
	assert.True(t, domain.SentrySessionMetric(domain.SentryMetricCrashFreeUsers))

	for _, bad := range []string{`{"project":"a/b"}`, `{"project":" "}`, `{"project":"p","days":365}`, `{"project":"p","metric":"apdex"}`} {
		_, err := domain.ParseSentryQuery(bad)
		assert.Error(t, err, bad)
	}
}
