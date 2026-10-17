package cloudwatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/cloudwatch"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const (
	accessKey    = "AKIDEXAMPLE"
	secretKey    = "wJalrXUtnFEMI/K7MDENG/bPxRfiCY"
	sessionToken = "FwoGZXIvYXdzEBYaDsessiontoken"
)

type call struct {
	form   url.Values
	header http.Header
}

type fixture struct {
	t        *testing.T
	srv      *httptest.Server
	adapter  *cloudwatch.Adapter
	accounts *mocks.CloudAccountReader
	now      time.Time
	mu       sync.Mutex
	calls    []call
}

func newFixture(t *testing.T, answer func(n int, form url.Values) (int, string)) *fixture {
	t.Helper()
	f := &fixture{t: t, now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		f.mu.Lock()
		f.calls = append(f.calls, call{form: r.PostForm, header: r.Header.Clone()})
		n := len(f.calls)
		f.mu.Unlock()
		status, body := answer(n, r.PostForm)
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(f.srv.Close)
	f.accounts = &mocks.CloudAccountReader{}
	t.Cleanup(func() { f.accounts.AssertExpectations(t) })
	f.adapter = cloudwatch.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetCloudAccounts(f.accounts)
	f.adapter.SetBaseURL(f.srv.URL)
	f.adapter.SetClock(func() time.Time { return f.now })
	return f
}

func (f *fixture) source(config map[string]string) domain.DataSource {
	id := uuid.New()
	f.accounts.On("CloudCredential", mock.Anything, id).Return(domain.CloudCredential{
		AccountID: id, Provider: domain.CloudAWS,
		Meta: map[string]string{"account_id": "123456789012", "region": "eu-central-1"},
		Fields: map[string]string{
			"access_key_id": accessKey, "secret_access_key": secretKey, "session_token": sessionToken, "region": "eu-central-1",
		},
	}, nil)
	c := map[string]string{"cloud_account_id": id.String()}
	for k, v := range config {
		c[k] = v
	}
	return domain.DataSource{Kind: domain.DataSourceKindCloudWatch, Config: c}
}

func dataPage(id, label string, points [][2]string, next string) string {
	var ts, vs strings.Builder
	for _, p := range points {
		ts.WriteString("<member>" + p[0] + "</member>")
		vs.WriteString("<member>" + p[1] + "</member>")
	}
	token := ""
	if next != "" {
		token = "<NextToken>" + next + "</NextToken>"
	}
	return `<GetMetricDataResponse xmlns="http://monitoring.amazonaws.com/doc/2010-08-01/"><GetMetricDataResult>
		<MetricDataResults><member><Id>` + id + `</Id><Label>` + label + `</Label><StatusCode>Complete</StatusCode>
		<Timestamps>` + ts.String() + `</Timestamps><Values>` + vs.String() + `</Values></member></MetricDataResults>` +
		token + `<Messages/></GetMetricDataResult><ResponseMetadata><RequestId>r</RequestId></ResponseMetadata></GetMetricDataResponse>`
}

const backlog = `{"namespace": "AWS/SQS", "metric": "ApproximateNumberOfMessagesVisible", "stat": "Maximum", "dimensions": {"QueueName": "orders"}}`

func query(q string, step time.Duration, from time.Time) domain.SeriesQuery {
	return domain.SeriesQuery{Query: q, From: from, To: from.Add(time.Hour), Step: step}
}

func TestSeriesSignsGetMetricDataAndFollowsTheNextToken(t *testing.T) {
	f := newFixture(t, func(n int, _ url.Values) (int, string) {
		if n == 1 {
			return 200, dataPage("m", "ApproximateNumberOfMessagesVisible", [][2]string{{"2026-10-16T10:00:00Z", "3.0"}, {"2026-10-16T10:05:00Z", "4.5"}}, "page-2")
		}
		return 200, dataPage("m", "ApproximateNumberOfMessagesVisible", [][2]string{{"2026-10-16T10:10:00Z", "1E+1"}}, "")
	})
	from := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)

	res, err := f.adapter.Series(context.Background(), f.source(nil), nil, query(backlog, 5*time.Minute, from))

	require.NoError(t, err)
	require.Len(t, f.calls, 2)
	first := f.calls[0]
	assert.Equal(t, "GetMetricData", first.form.Get("Action"))
	assert.Equal(t, "2010-08-01", first.form.Get("Version"))
	assert.Equal(t, "2026-10-16T10:00:00Z", first.form.Get("StartTime"))
	assert.Equal(t, "2026-10-16T11:00:00Z", first.form.Get("EndTime"))
	assert.Equal(t, "m", first.form.Get("MetricDataQueries.member.1.Id"))
	assert.Equal(t, "AWS/SQS", first.form.Get("MetricDataQueries.member.1.MetricStat.Metric.Namespace"))
	assert.Equal(t, "ApproximateNumberOfMessagesVisible", first.form.Get("MetricDataQueries.member.1.MetricStat.Metric.MetricName"))
	assert.Equal(t, "QueueName", first.form.Get("MetricDataQueries.member.1.MetricStat.Metric.Dimensions.member.1.Name"))
	assert.Equal(t, "orders", first.form.Get("MetricDataQueries.member.1.MetricStat.Metric.Dimensions.member.1.Value"))
	assert.Equal(t, "300", first.form.Get("MetricDataQueries.member.1.MetricStat.Period"))
	assert.Equal(t, "Maximum", first.form.Get("MetricDataQueries.member.1.MetricStat.Stat"))
	assert.Empty(t, first.form.Get("NextToken"))
	assert.Equal(t, "page-2", f.calls[1].form.Get("NextToken"))

	auth := first.header.Get("Authorization")
	assert.True(t, strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential="+accessKey+"/20261016/eu-central-1/monitoring/aws4_request"), auth)
	assert.Contains(t, auth, "SignedHeaders=")
	assert.Contains(t, auth, "Signature=")
	assert.Equal(t, "20261016T120000Z", first.header.Get("X-Amz-Date"))
	assert.Equal(t, sessionToken, first.header.Get("X-Amz-Security-Token"))
	assert.NotContains(t, auth, secretKey)

	require.Len(t, res.Series, 1)
	s := res.Series[0]
	assert.Equal(t, "ApproximateNumberOfMessagesVisible", s.Name)
	assert.Equal(t, map[string]string{"QueueName": "orders", "stat": "Maximum"}, s.Labels)
	raw, err := json.Marshal(s.Points)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`[[%d,3],[%d,4.5],[%d,10]]`,
		from.UnixMilli(), from.Add(5*time.Minute).UnixMilli(), from.Add(10*time.Minute).UnixMilli()), string(raw))
}

func TestAnExpressionSendsItsMetricsHiddenAndReturnsTheExpression(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) {
		return 200, dataPage("e1", "error ratio", [][2]string{{"2026-10-16T10:00:00Z", "2.5"}}, "")
	})
	q := `{"expression": "100 * FILL(e, 0) / i", "metrics": {
		"e": {"namespace": "AWS/Lambda", "metric": "Errors", "stat": "Sum", "dimensions": {"FunctionName": "resize"}},
		"i": {"namespace": "AWS/Lambda", "metric": "Invocations", "stat": "Sum", "dimensions": {"FunctionName": "resize"}}}}`

	res, err := f.adapter.Series(context.Background(), f.source(nil), nil, query(q, time.Minute, f.now.Add(-time.Hour)))

	require.NoError(t, err)
	form := f.calls[0].form
	assert.Equal(t, "e", form.Get("MetricDataQueries.member.1.Id"))
	assert.Equal(t, "false", form.Get("MetricDataQueries.member.1.ReturnData"))
	assert.Equal(t, "i", form.Get("MetricDataQueries.member.2.Id"))
	assert.Equal(t, "false", form.Get("MetricDataQueries.member.2.ReturnData"))
	assert.Equal(t, "e1", form.Get("MetricDataQueries.member.3.Id"), "the expression takes an id its metrics leave free")
	assert.Equal(t, "100 * FILL(e, 0) / i", form.Get("MetricDataQueries.member.3.Expression"))
	assert.Equal(t, "60", form.Get("MetricDataQueries.member.3.Period"))
	assert.Equal(t, "true", form.Get("MetricDataQueries.member.3.ReturnData"))
	require.Len(t, res.Series, 1)
	assert.Equal(t, "error ratio", res.Series[0].Name)
}

func TestThePeriodFollowsTheStepAndWhatCloudWatchStillKeeps(t *testing.T) {
	cases := []struct {
		name  string
		q     string
		step  time.Duration
		since time.Duration
		want  string
	}{
		{"under a minute", backlog, 15 * time.Second, time.Hour, "60"},
		{"rounded up to a minute", backlog, 90 * time.Second, time.Hour, "120"},
		{"older than 15 days", backlog, time.Minute, 20 * 24 * time.Hour, "300"},
		{"older than 63 days", backlog, 10 * time.Minute, 70 * 24 * time.Hour, "3600"},
		{"the query's own", strings.Replace(backlog, `"stat"`, `"period": 30, "stat"`, 1), 5 * time.Minute, time.Hour, "30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, func(int, url.Values) (int, string) { return 200, dataPage("m", "x", nil, "") })
			_, err := f.adapter.Series(context.Background(), f.source(nil), nil, query(tc.q, tc.step, f.now.Add(-tc.since)))
			require.NoError(t, err)
			assert.Equal(t, tc.want, f.calls[0].form.Get("MetricDataQueries.member.1.MetricStat.Period"))
		})
	}
}

func TestTheRegionComesFromTheSourceThenTheAccount(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) { return 200, dataPage("m", "x", nil, "") })

	_, err := f.adapter.Series(context.Background(), f.source(map[string]string{"region": "us-east-1"}), nil, query(backlog, time.Minute, f.now.Add(-time.Hour)))
	require.NoError(t, err)
	_, err = f.adapter.Series(context.Background(), f.source(nil), nil, query(backlog, time.Minute, f.now.Add(-time.Hour)))
	require.NoError(t, err)

	assert.Contains(t, f.calls[0].header.Get("Authorization"), "/us-east-1/monitoring/")
	assert.Contains(t, f.calls[1].header.Get("Authorization"), "/eu-central-1/monitoring/")

	_, err = f.adapter.Series(context.Background(), f.source(map[string]string{"region": "evil.example.com#"}), nil, query(backlog, time.Minute, f.now.Add(-time.Hour)))
	assert.ErrorContains(t, err, "is not an AWS region")
	assert.Len(t, f.calls, 2)
}

func TestDiscoverListsEachNamespaceAndMetricOnceAcrossPages(t *testing.T) {
	page := func(metrics [][2]string, next string) string {
		var b strings.Builder
		for _, m := range metrics {
			b.WriteString("<member><Namespace>" + m[0] + "</Namespace><MetricName>" + m[1] + "</MetricName><Dimensions/></member>")
		}
		token := ""
		if next != "" {
			token = "<NextToken>" + next + "</NextToken>"
		}
		return `<ListMetricsResponse><ListMetricsResult><Metrics>` + b.String() + `</Metrics>` + token + `</ListMetricsResult></ListMetricsResponse>`
	}
	f := newFixture(t, func(n int, _ url.Values) (int, string) {
		if n == 1 {
			return 200, page([][2]string{{"AWS/SQS", "NumberOfMessagesSent"}, {"AWS/SQS", "NumberOfMessagesSent"}, {"AWS/Lambda", "Invocations"}}, "more")
		}
		return 200, page([][2]string{{"AWS/SQS", "ApproximateNumberOfMessagesVisible"}}, "")
	})
	f.adapter.SetMaxInventory(2)

	inv, err := f.adapter.Discover(context.Background(), f.source(nil), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"AWS/Lambda/Invocations", "AWS/SQS/ApproximateNumberOfMessagesVisible"}, inv.Metrics)
	assert.True(t, inv.Truncated)
	assert.Equal(t, "ListMetrics", f.calls[0].form.Get("Action"))
	assert.Equal(t, "PT3H", f.calls[0].form.Get("RecentlyActive"))
	assert.Equal(t, "more", f.calls[1].form.Get("NextToken"))
	assert.True(t, strings.HasPrefix(f.calls[1].header.Get("Authorization"), "AWS4-HMAC-SHA256 "))
}

func TestVerifyListsMetricsInTheRegion(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) {
		return 200, `<ListMetricsResponse><ListMetricsResult><Metrics/></ListMetricsResult></ListMetricsResponse>`
	})

	info, err := f.adapter.Verify(context.Background(), f.source(nil), nil)

	require.NoError(t, err)
	assert.Equal(t, "Amazon CloudWatch · eu-central-1", info.Detail)
	assert.Equal(t, "ListMetrics", f.calls[0].form.Get("Action"))
}

func errorXML(code, message string) string {
	return `<ErrorResponse xmlns="http://monitoring.amazonaws.com/doc/2010-08-01/"><Error><Type>Sender</Type><Code>` + code +
		`</Code><Message>` + message + `</Message></Error><RequestId>r</RequestId></ErrorResponse>`
}

func TestCloudWatchRefusalsReadAsAuthOrTheCallersMistakeAndNeverCarryAKey(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		want      string
		callers   bool
		forbidden []string
	}{
		{"bad signature", 403, errorXML("SignatureDoesNotMatch", "The request signature we calculated does not match for "+secretKey), "authentication failed", false, []string{secretKey}},
		{"expired session", 400, errorXML("ExpiredToken", "The security token "+sessionToken+" is expired"), "authentication failed", false, []string{sessionToken}},
		{"bad parameter", 400, errorXML("InvalidParameterValue", "The value p999x for parameter Stat is not valid."), "InvalidParameterValue", true, nil},
		{"throttled", 400, errorXML("Throttling", "Rate exceeded"), "Throttling: Rate exceeded", false, nil},
		{"no body", 500, "", "HTTP 500", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, func(int, url.Values) (int, string) { return tc.status, tc.body })

			_, err := f.adapter.Series(context.Background(), f.source(nil), nil, query(backlog, time.Minute, f.now.Add(-time.Hour)))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Equal(t, tc.callers, errors.Is(err, port.ErrDataSourceQuery))
			for _, s := range tc.forbidden {
				assert.NotContains(t, err.Error(), s)
			}
		})
	}
}

func TestAQueryThatIsNotCloudWatchJSONIsRefusedBeforeAnyCall(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) { return 200, "" })
	ds := domain.DataSource{Kind: domain.DataSourceKindCloudWatch, Config: map[string]string{"cloud_account_id": uuid.NewString()}}

	_, err := f.adapter.Series(context.Background(), ds, nil, query(`sum(rate(x[5m]))`, time.Minute, f.now.Add(-time.Hour)))

	require.Error(t, err)
	assert.True(t, errors.Is(err, port.ErrDataSourceQuery))
	assert.Empty(t, f.calls)
}

func TestTheEndpointIsJudgedByTheURLPolicy(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) { return 200, dataPage("m", "x", nil, "") })
	f.adapter.SetURLPolicy(urlguard.PublicOnly())

	_, err := f.adapter.Series(context.Background(), f.source(nil), nil, query(backlog, time.Minute, f.now.Add(-time.Hour)))

	require.Error(t, err)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Empty(t, f.calls)
}

func TestTheAccountMustBeAConnectedAWSOne(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) { return 200, "" })
	missing, gcp := uuid.New(), uuid.New()
	f.accounts.On("CloudCredential", mock.Anything, missing).Return(domain.CloudCredential{}, port.ErrNotFound)
	f.accounts.On("CloudCredential", mock.Anything, gcp).Return(domain.CloudCredential{AccountID: gcp, Provider: domain.CloudGCP}, nil)
	source := func(id uuid.UUID) domain.DataSource {
		return domain.DataSource{Kind: domain.DataSourceKindCloudWatch, Config: map[string]string{"cloud_account_id": id.String()}}
	}

	_, err := f.adapter.Verify(context.Background(), source(missing), nil)
	assert.ErrorContains(t, err, "no longer connected")
	_, err = f.adapter.Verify(context.Background(), source(gcp), nil)
	assert.ErrorContains(t, err, "not an AWS account")
	assert.Empty(t, f.calls)
}

func TestSeriesReportsThePeriodItReadAtAsTheStep(t *testing.T) {
	f := newFixture(t, func(int, url.Values) (int, string) { return 200, dataPage("m", "x", nil, "") })

	res, err := f.adapter.Series(context.Background(), f.source(nil), nil, query(backlog, 15*time.Second, f.now.Add(-time.Hour)))

	require.NoError(t, err)
	assert.Equal(t, time.Minute, res.Step)
}

func TestDiscoverMetricAsksListMetricsForThatNamespaceAndNameOnly(t *testing.T) {
	answers := map[string]string{
		"ECS/ContainerInsights RunningTaskCount":     `<ListMetricsResponse><ListMetricsResult><Metrics><member><Namespace>ECS/ContainerInsights</Namespace><MetricName>RunningTaskCount</MetricName><Dimensions/></member></Metrics></ListMetricsResult></ListMetricsResponse>`,
		"AWS/SQS ApproximateNumberOfMessagesVisible": `<ListMetricsResponse><ListMetricsResult><Metrics/></ListMetricsResult></ListMetricsResponse>`,
	}
	f := newFixture(t, func(_ int, form url.Values) (int, string) {
		return 200, answers[form.Get("Namespace")+" "+form.Get("MetricName")]
	})
	ds := f.source(nil)

	found, err := f.adapter.DiscoverMetric(context.Background(), ds, nil, "ECS/ContainerInsights/RunningTaskCount")
	require.NoError(t, err)
	assert.True(t, found)
	found, err = f.adapter.DiscoverMetric(context.Background(), ds, nil, "AWS/SQS/ApproximateNumberOfMessagesVisible")
	require.NoError(t, err)
	assert.False(t, found)
	found, err = f.adapter.DiscoverMetric(context.Background(), ds, nil, "no-namespace")
	require.NoError(t, err)
	assert.False(t, found)

	require.Len(t, f.calls, 2)
	assert.Equal(t, "ListMetrics", f.calls[0].form.Get("Action"))
	assert.Equal(t, "PT3H", f.calls[0].form.Get("RecentlyActive"))
	assert.Empty(t, f.calls[0].form.Get("NextToken"))
}
