package datasource_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestRenderKindQueryExpandsTheQuantileInEachLanguage(t *testing.T) {
	promQL := `histogram_quantile($__quantile, sum by (le) (rate(x_bucket[$__rate])))`
	cloudWatch := `{"namespace": "AWS/Lambda", "metric": "Duration", "stat": "$__quantile"}`
	cases := []struct {
		name     string
		kind     domain.DataSourceKind
		query    string
		quantile float64
		want     string
	}{
		{"prometheus default", domain.DataSourceKindPrometheus, promQL, 0, `histogram_quantile(0.95, sum by (le) (rate(x_bucket[120s])))`},
		{"gcp p99", domain.DataSourceKindGCPMonitoring, promQL, 0.99, `histogram_quantile(0.99, sum by (le) (rate(x_bucket[120s])))`},
		{"cloudwatch default", domain.DataSourceKindCloudWatch, cloudWatch, 0, `{"namespace": "AWS/Lambda", "metric": "Duration", "stat": "p95"}`},
		{"cloudwatch p99.9", domain.DataSourceKindCloudWatch, cloudWatch, 0.999, `{"namespace": "AWS/Lambda", "metric": "Duration", "stat": "p99.9"}`},
		{"cloudwatch p50", domain.DataSourceKindCloudWatch, cloudWatch, 0.5, `{"namespace": "AWS/Lambda", "metric": "Duration", "stat": "p50"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := datasource.RenderKindQuery(tc.kind, tc.query, nil, datasource.RenderOptions{Step: time.Minute, Quantile: tc.quantile})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	for _, q := range []float64{1, 1.5, -0.1} {
		_, err := datasource.RenderKindQuery(domain.DataSourceKindPrometheus, promQL, nil, datasource.RenderOptions{Step: time.Minute, Quantile: q})
		assert.ErrorIs(t, err, datasource.ErrInvalidInput, "%v", q)
	}
}

func TestRenderQueryKeepsTheDefaultQuantileForPromQL(t *testing.T) {
	got, err := datasource.RenderQuery(`histogram_quantile($__quantile, x)`, nil, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, `histogram_quantile(0.95, x)`, got)
}

func TestCloudWatchParamsAreEscapedForAJSONString(t *testing.T) {
	value := "a\"b\\c\nd\x01</script>"
	got, err := datasource.RenderKindQuery(domain.DataSourceKindCloudWatch,
		`{"namespace": "AWS/SQS", "metric": "M", "stat": "Sum", "dimensions": {"QueueName": "q-$queue"}}`,
		map[string]string{"queue": value}, datasource.RenderOptions{Step: time.Minute})
	require.NoError(t, err)

	q, err := domain.ParseCloudWatchQuery(got)
	require.NoError(t, err)
	assert.Equal(t, "q-"+value, q.Dimensions["QueueName"])
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &raw))
	assert.Len(t, raw, 4, "the value must not add a key of its own")
}
