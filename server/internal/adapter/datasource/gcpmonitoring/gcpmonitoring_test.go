package gcpmonitoring_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/gcpmonitoring"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const promBase = "/v1/projects/acme-prod/location/global/prometheus"

type minted struct {
	mu     sync.Mutex
	calls  int
	scopes []string
	keys   []string
}

func (m *minted) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func fakeMinter(m *minted, now func() time.Time, ttl time.Duration) gcpmonitoring.Minter {
	return func(_ context.Context, client *http.Client, key, scope string) (string, time.Time, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if client == nil {
			return "", time.Time{}, errors.New("no client")
		}
		m.calls++
		m.scopes = append(m.scopes, scope)
		m.keys = append(m.keys, key)
		return fmt.Sprintf("tok-%d", m.calls), now().Add(ttl), nil
	}
}

type fixture struct {
	t        *testing.T
	srv      *httptest.Server
	adapter  *gcpmonitoring.Adapter
	accounts *mocks.CloudAccountReader
	minted   *minted
	now      time.Time
	mu       sync.Mutex
	auth     []string
	paths    []string
}

func newFixture(t *testing.T, routes map[string]http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{t: t, minted: &minted{}, now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.auth = append(f.auth, r.Header.Get("Authorization"))
			f.paths = append(f.paths, r.URL.Path)
			f.mu.Unlock()
			h(w, r)
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.accounts = &mocks.CloudAccountReader{}
	t.Cleanup(func() { f.accounts.AssertExpectations(t) })
	f.adapter = gcpmonitoring.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetCloudAccounts(f.accounts)
	f.adapter.SetBaseURL(f.srv.URL)
	f.adapter.SetClock(func() time.Time { return f.now })
	f.adapter.SetMinter(fakeMinter(f.minted, func() time.Time { return f.now }, time.Hour))
	return f
}

func (f *fixture) account(meta map[string]string, key string) domain.DataSource {
	id := uuid.New()
	f.accounts.On("CloudCredential", mock.Anything, id).Return(domain.CloudCredential{
		AccountID: id, Provider: domain.CloudGCP, Meta: meta,
		Fields: map[string]string{"service_account_json": key},
	}, nil)
	return domain.DataSource{Kind: domain.DataSourceKindGCPMonitoring, Config: map[string]string{"cloud_account_id": id.String()}}
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

const matrix = `{"status":"success","data":{"resultType":"matrix","result":[
	{"metric":{"service_name":"api"},"values":[[1700000000,"12.5"],[1700000060,"NaN"]]}]}}`

func rangeQuery() domain.SeriesQuery {
	from := time.Unix(1700000000, 0).UTC()
	return domain.SeriesQuery{Query: `sum(rate(run_googleapis_com:request_count[2m]))`, From: from, To: from.Add(time.Hour), Step: time.Minute}
}

func TestSeriesQueriesTheProjectsPrometheusAPIWithACachedToken(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{promBase + "/api/v1/query_range": reply(200, matrix)})
	ds := f.account(map[string]string{"project_id": "acme-prod"}, `{"client_email":"a@b"}`)

	res, err := f.adapter.Series(context.Background(), ds, nil, rangeQuery())
	require.NoError(t, err)
	_, err = f.adapter.Series(context.Background(), ds, nil, rangeQuery())
	require.NoError(t, err)

	require.Len(t, res.Series, 1)
	assert.Equal(t, map[string]string{"service_name": "api"}, res.Series[0].Labels)
	assert.Len(t, res.Series[0].Points, 2)
	assert.Equal(t, 1, f.minted.count(), "the second query reuses the token")
	assert.Equal(t, []string{gcpmonitoring.MonitoringReadScope}, f.minted.scopes)
	assert.Equal(t, []string{"Bearer tok-1", "Bearer tok-1"}, f.auth)
}

func TestATokenIsMintedAgainShortlyBeforeItExpiresAndPerKey(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{promBase + "/api/v1/query_range": reply(200, matrix)})
	f.adapter.SetMinter(fakeMinter(f.minted, func() time.Time { return f.now }, 90*time.Second))
	ds := f.account(map[string]string{"project_id": "acme-prod"}, `{"key":"one"}`)
	rotated := f.account(map[string]string{"project_id": "acme-prod"}, `{"key":"two"}`)

	_, err := f.adapter.Series(context.Background(), ds, nil, rangeQuery())
	require.NoError(t, err)
	f.now = f.now.Add(20 * time.Second)
	_, err = f.adapter.Series(context.Background(), ds, nil, rangeQuery())
	require.NoError(t, err)
	assert.Equal(t, 1, f.minted.count(), "still more than a minute left")

	f.now = f.now.Add(20 * time.Second)
	_, err = f.adapter.Series(context.Background(), ds, nil, rangeQuery())
	require.NoError(t, err)
	assert.Equal(t, 2, f.minted.count(), "under a minute left: renewed before it lapses in flight")

	_, err = f.adapter.Series(context.Background(), rotated, nil, rangeQuery())
	require.NoError(t, err)
	assert.Equal(t, 3, f.minted.count(), "another key file gets its own token")
	assert.Equal(t, []string{"Bearer tok-1", "Bearer tok-1", "Bearer tok-2", "Bearer tok-3"}, f.auth)
}

func TestTheProjectComesFromTheSourceThenTheAccountThenTheKeyFile(t *testing.T) {
	answer := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "1", r.PostForm.Get("query"))
		reply(200, `{"status":"success","data":{"resultType":"scalar","result":[1,"1"]}}`)(w, r)
	}
	f := newFixture(t, map[string]http.HandlerFunc{
		"/v1/projects/from-config/location/global/prometheus/api/v1/query":   answer,
		"/v1/projects/from-meta/location/global/prometheus/api/v1/query":     answer,
		"/v1/projects/from-key-file/location/global/prometheus/api/v1/query": answer,
	})

	configured := f.account(map[string]string{"project_id": "from-meta"}, `{"project_id":"from-key-file"}`)
	configured.Config["project_id"] = "from-config"
	cases := []struct {
		name string
		ds   domain.DataSource
		want string
	}{
		{"the source's own", configured, "from-config"},
		{"the account's", f.account(map[string]string{"project_id": "from-meta"}, `{"project_id":"from-key-file"}`), "from-meta"},
		{"the key file's", f.account(nil, `{"project_id":"from-key-file"}`), "from-key-file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := f.adapter.Verify(context.Background(), tc.ds, nil)
			require.NoError(t, err)
			assert.Equal(t, "Google Cloud Monitoring · "+tc.want, info.Detail)
		})
	}
	assert.Equal(t, []string{
		"/v1/projects/from-config/location/global/prometheus/api/v1/query",
		"/v1/projects/from-meta/location/global/prometheus/api/v1/query",
		"/v1/projects/from-key-file/location/global/prometheus/api/v1/query",
	}, f.paths)

	_, err := f.adapter.Verify(context.Background(), f.account(nil, `{}`), nil)
	assert.ErrorContains(t, err, "no project")
	bad := f.account(nil, `{}`)
	bad.Config["project_id"] = "../../admin"
	_, err = f.adapter.Verify(context.Background(), bad, nil)
	assert.ErrorContains(t, err, "is not a Google Cloud project id")
}

func TestDiscoverListsMetricNamesOverTheLastHour(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		promBase + "/api/v1/label/__name__/values": func(w http.ResponseWriter, r *http.Request) {
			assert.NotEmpty(t, r.URL.Query().Get("start"))
			reply(200, `{"status":"success","data":["run_googleapis_com:request_count","pubsub_googleapis_com:subscription_num_undelivered_messages"]}`)(w, r)
		},
	})

	inv, err := f.adapter.Discover(context.Background(), f.account(map[string]string{"project_id": "acme-prod"}, `{}`), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"pubsub_googleapis_com:subscription_num_undelivered_messages", "run_googleapis_com:request_count"}, inv.Metrics)
}

func TestARefusalNamesGooglesReasonButNeverTheToken(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		promBase + "/api/v1/query_range": reply(403, `{"error":{"code":403,"message":"Permission monitoring.timeSeries.list denied for tok-1","status":"PERMISSION_DENIED"}}`),
	})

	_, err := f.adapter.Series(context.Background(), f.account(map[string]string{"project_id": "acme-prod"}, `{}`), nil, rangeQuery())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed (HTTP 403)")
	assert.Contains(t, err.Error(), "monitoring.timeSeries.list denied")
	assert.NotContains(t, err.Error(), "tok-1")
}

func TestTheFixedAPIAddressIsStillJudgedByTheURLPolicy(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{promBase + "/api/v1/query_range": reply(200, matrix)})
	f.adapter.SetURLPolicy(urlguard.PublicOnly())

	_, err := f.adapter.Series(context.Background(), f.account(map[string]string{"project_id": "acme-prod"}, `{}`), nil, rangeQuery())

	require.Error(t, err)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Empty(t, f.paths)
}

func TestTheAccountMustBeAConnectedGoogleCloudOne(t *testing.T) {
	f := newFixture(t, nil)
	missing, aws := uuid.New(), uuid.New()
	f.accounts.On("CloudCredential", mock.Anything, missing).Return(domain.CloudCredential{}, port.ErrNotFound)
	f.accounts.On("CloudCredential", mock.Anything, aws).Return(domain.CloudCredential{AccountID: aws, Provider: domain.CloudAWS}, nil)
	source := func(id string) domain.DataSource {
		return domain.DataSource{Kind: domain.DataSourceKindGCPMonitoring, Config: map[string]string{"cloud_account_id": id}}
	}

	_, err := f.adapter.Verify(context.Background(), source(missing.String()), nil)
	assert.ErrorContains(t, err, "no longer connected")
	_, err = f.adapter.Verify(context.Background(), source(aws.String()), nil)
	assert.ErrorContains(t, err, "not a Google Cloud account")
	_, err = f.adapter.Verify(context.Background(), source(""), nil)
	assert.ErrorContains(t, err, "no Google Cloud account is selected")
	assert.Equal(t, 0, f.minted.count())

	unwired := gcpmonitoring.New()
	unwired.SetURLPolicy(urlguard.HumanConfigured())
	_, err = unwired.Verify(context.Background(), source(missing.String()), nil)
	assert.ErrorContains(t, err, "cloud accounts are not available")
}

func TestTheDefaultMinterAsksNoTokenURIButGooglesOwn(t *testing.T) {
	var grants []string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grants = append(grants, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"ya29.real","expires_in":3600}`)
	}))
	t.Cleanup(tokenSrv.Close)
	f := newFixture(t, map[string]http.HandlerFunc{promBase + "/api/v1/query_range": reply(200, matrix)})
	f.adapter = gcpmonitoring.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetCloudAccounts(f.accounts)
	f.adapter.SetBaseURL(f.srv.URL)

	for _, uri := range []string{tokenSrv.URL + "/token", "https://tokens.example.com/token"} {
		_, err := f.adapter.Series(context.Background(), f.account(map[string]string{"project_id": "acme-prod"}, serviceAccountKey(t, uri)), nil, rangeQuery())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token_uri is not Google's token endpoint")
	}
	assert.Empty(t, grants)
	assert.Empty(t, f.paths)
}

func TestAMintFailureNamesTheCauseButNotTheTokenURL(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"refused by the URL policy", fmt.Errorf("dial: %w", urlguard.ErrBlocked), "token_uri is not an allowed destination"},
		{"refused by Google", errors.New("gcloud: the token endpoint refused the key (HTTP 400, invalid_grant)"), "sign-in for the cloud account failed: gcloud: the token endpoint refused the key (HTTP 400, invalid_grant)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			f.adapter.SetMinter(func(context.Context, *http.Client, string, string) (string, time.Time, error) {
				return "", time.Time{}, tt.err
			})

			_, err := f.adapter.Verify(context.Background(), f.account(map[string]string{"project_id": "acme-prod"}, `{}`), nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func serviceAccountKey(t *testing.T, tokenURI string) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"client_email": "ops@acme-prod.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"project_id":   "acme-prod",
		"token_uri":    tokenURI,
	})
	require.NoError(t, err)
	return string(raw)
}
