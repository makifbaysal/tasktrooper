package prometheus_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/prometheus"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func newAdapter() *prometheus.Adapter {
	a := prometheus.New()
	a.SetURLPolicy(urlguard.HumanConfigured())
	return a
}

type recorded struct {
	mu       sync.Mutex
	requests []*http.Request
	forms    []map[string][]string
}

func (r *recorded) add(req *http.Request) {
	_ = req.ParseForm()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	r.forms = append(r.forms, req.PostForm)
}

func (r *recorded) last() (*http.Request, map[string][]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[len(r.requests)-1], r.forms[len(r.forms)-1]
}

func serve(t *testing.T, routes map[string]http.HandlerFunc) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			rec.add(r)
			h(w, r)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

func jsonReply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

func source(url string, config map[string]string) domain.DataSource {
	c := map[string]string{"url": url}
	for k, v := range config {
		c[k] = v
	}
	return domain.DataSource{Kind: domain.DataSourceKindPrometheus, Config: c}
}

func TestVerifyReadsBuildInfoWithBearerTokenAndTenantHeader(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/prometheus/api/v1/status/buildinfo": jsonReply(200, `{"status":"success","data":{"application":"Grafana Mimir","version":"2.14.0"}}`),
	})
	a := newAdapter()

	info, err := a.Verify(context.Background(), source(srv.URL+"/prometheus", map[string]string{"auth": "bearer"}),
		map[string]string{"token": "s3cret-token", "headers": "X-Scope-OrgID: tenant-1\nX-Extra: yes"})

	require.NoError(t, err)
	assert.Equal(t, domain.DataSourceInfo{Version: "2.14.0", Detail: "Grafana Mimir"}, info)
	req, _ := rec.last()
	assert.Equal(t, "Bearer s3cret-token", req.Header.Get("Authorization"))
	assert.Equal(t, "tenant-1", req.Header.Get("X-Scope-OrgID"))
	assert.Equal(t, "yes", req.Header.Get("X-Extra"))
}

func TestVerifyFallsBackToAnInstantQueryWithoutBuildInfo(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/api/v1/status/buildinfo": http.NotFound,
		"/api/v1/query":            jsonReply(200, `{"status":"success","data":{"resultType":"scalar","result":[1,"1"]}}`),
	})
	a := newAdapter()

	info, err := a.Verify(context.Background(), source(srv.URL, map[string]string{"auth": "basic", "username": "123456"}),
		map[string]string{"password": "glc_token"})

	require.NoError(t, err)
	assert.Equal(t, "", info.Version)
	assert.NotEmpty(t, info.Detail)
	req, form := rec.last()
	assert.Equal(t, "/api/v1/query", req.URL.Path)
	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, "application/x-www-form-urlencoded", req.Header.Get("Content-Type"))
	assert.Equal(t, []string{"1"}, form["query"])
	assert.Empty(t, req.URL.RawQuery)
	user, pass, ok := req.BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "123456", user)
	assert.Equal(t, "glc_token", pass)
}

func TestVerifyErrorsNeverCarryTheSecret(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"refused credential", jsonReply(401, `{"status":"error","errorType":"unauthorized","error":"bad token s3cret-token"}`), "authentication failed"},
		{"server echoing the token", jsonReply(500, `{"status":"error","errorType":"internal","error":"token s3cret-token is invalid"}`), "HTTP 500"},
		{"not a Prometheus API", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, "<html>Grafana</html>")
		}, "not with the Prometheus HTTP API"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serve(t, map[string]http.HandlerFunc{
				"/api/v1/status/buildinfo": tc.handler,
				"/api/v1/query":            tc.handler,
			})
			_, err := newAdapter().Verify(context.Background(), source(srv.URL, map[string]string{"auth": "bearer"}),
				map[string]string{"token": "s3cret-token"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "s3cret-token")
		})
	}
}

func TestTheURLPolicyAllowsLoopbackAndRefusesMetadata(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/api/v1/status/buildinfo": jsonReply(200, `{"status":"success","data":{"version":"2.53.0"}}`),
	})
	local := newAdapter()

	_, err := local.Verify(context.Background(), source(srv.URL, nil), nil)
	require.NoError(t, err, "127.0.0.1 is where a local Prometheus lives")

	for _, raw := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://[fd00:ec2::254]",
		"http://metadata.google.internal",
	} {
		_, err := local.Verify(context.Background(), source(raw, nil), nil)
		require.Error(t, err, raw)
		assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%s: %v", raw, err)
	}

	shared := prometheus.New()
	shared.SetURLPolicy(urlguard.PublicOnly())
	_, err = shared.Verify(context.Background(), source(srv.URL, nil), nil)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "a public-only policy must refuse loopback: %v", err)
}

func TestTheDialUsesTheAddressThePolicyVetted(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/api/v1/status/buildinfo": jsonReply(200, `{"status":"success","data":{"version":"2.53.0"}}`),
	})
	answers := []string{"127.0.0.1", "169.254.169.254"}
	var mu sync.Mutex
	var lookups int
	var dialed []string
	policy := urlguard.HumanConfigured()
	policy.Resolver = resolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		mu.Lock()
		defer mu.Unlock()
		ip := answers[min(lookups, len(answers)-1)]
		lookups++
		return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
	})
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	policy.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}
	a := prometheus.New()
	a.SetURLPolicy(policy)

	_, err := a.Verify(context.Background(), source(fmt.Sprintf("http://prom.rebind.test:%d", port), nil), nil)

	require.NoError(t, err)
	assert.Equal(t, 1, lookups, "a second lookup is the rebinding window")
	assert.Equal(t, []string{fmt.Sprintf("127.0.0.1:%d", port)}, dialed)
}

func TestARedirectToAnotherHostIsRefused(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/api/v1/status/buildinfo": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://127.0.0.2:9/api/v1/status/buildinfo", http.StatusFound)
		},
	})

	_, err := newAdapter().Verify(context.Background(), source(srv.URL, map[string]string{"auth": "bearer"}),
		map[string]string{"token": "s3cret-token"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Len(t, rec.requests, 1)
}

type resolverFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

func TestSeriesPostsTheRangeAndTurnsNaNAndInfIntoNull(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/api/v1/query_range": jsonReply(200, `{"status":"success","warnings":["partial response"],"data":{"resultType":"matrix","result":[
			{"metric":{"__name__":"up","job":"api"},"values":[[1700000000,"1"],[1700000060,"NaN"],[1700000120,"+Inf"],[1700000180.5,"0.25"]]},
			{"metric":{"job":"worker"},"values":[[1700000000,"-Inf"]]},
			{"metric":{},"values":[[1700000000,"3"]]}
		]}}`),
	})
	from := time.Unix(1700000000, 0).UTC()
	q := domain.SeriesQuery{Query: `sum(rate(x[1m]))`, From: from, To: from.Add(time.Hour), Step: time.Minute}

	res, err := newAdapter().Series(context.Background(), source(srv.URL, nil), nil, q)

	require.NoError(t, err)
	_, form := rec.last()
	assert.Equal(t, []string{`sum(rate(x[1m]))`}, form["query"])
	assert.Equal(t, []string{"1700000000.000"}, form["start"])
	assert.Equal(t, []string{"1700003600.000"}, form["end"])
	assert.Equal(t, []string{"60"}, form["step"])

	require.Len(t, res.Series, 3)
	assert.Equal(t, "up", res.Series[0].Name)
	assert.Equal(t, map[string]string{"job": "api"}, res.Series[0].Labels)
	assert.Equal(t, `{job="worker"}`, res.Series[1].Name)
	assert.Equal(t, `sum(rate(x[1m]))`, res.Series[2].Name)
	assert.Equal(t, []string{"partial response"}, res.Warnings)

	raw, err := json.Marshal(res.Series[0].Points)
	require.NoError(t, err)
	assert.JSONEq(t, `[[1700000000000,1],[1700000060000,null],[1700000120000,null],[1700000180500,0.25]]`, string(raw))
	assert.Nil(t, res.Series[1].Points[0].Value)
}

func TestSeriesReportsARejectedQueryAsTheCallersMistake(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/api/v1/query_range": jsonReply(400, `{"status":"error","errorType":"bad_data","error":"1:5: parse error: unexpected end of input"}`),
	})
	from := time.Now().Add(-time.Hour)
	_, err := newAdapter().Series(context.Background(), source(srv.URL, nil), nil,
		domain.SeriesQuery{Query: "sum(", From: from, To: from.Add(time.Hour), Step: time.Minute})

	require.Error(t, err)
	assert.True(t, errors.Is(err, port.ErrDataSourceQuery))
	assert.Contains(t, err.Error(), "parse error")
}

func TestSeriesRefusesAnOversizedResponse(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/api/v1/query_range": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[`)
			chunk := strings.Repeat(`{"metric":{"a":"b"},"values":[[1,"1"]]},`, 1<<14)
			for i := 0; i < 64; i++ {
				_, _ = fmt.Fprint(w, chunk)
			}
		},
	})
	from := time.Now().Add(-time.Hour)
	_, err := newAdapter().Series(context.Background(), source(srv.URL, nil), nil,
		domain.SeriesQuery{Query: "up", From: from, To: from.Add(time.Hour), Step: time.Minute})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger than")
}

func TestDiscoverListsMetricNamesUpToTheCap(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/api/v1/label/__name__/values": jsonReply(200, `{"status":"success","data":["up","rabbitmq_queue_messages_ready","go_goroutines"]}`),
	})
	a := newAdapter()

	inv, err := a.Discover(context.Background(), source(srv.URL, nil), nil)
	require.NoError(t, err)
	assert.Equal(t, domain.Inventory{Metrics: []string{"go_goroutines", "rabbitmq_queue_messages_ready", "up"}}, inv)
	req, _ := rec.last()
	assert.NotEmpty(t, req.URL.Query().Get("start"))

	a.SetMaxInventory(2)
	inv, err = a.Discover(context.Background(), source(srv.URL, nil), nil)
	require.NoError(t, err)
	assert.Equal(t, domain.Inventory{Metrics: []string{"go_goroutines", "rabbitmq_queue_messages_ready"}, Truncated: true}, inv)
}

func TestTransportErrorsDoNotRepeatTheRequestURL(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	_, err = newAdapter().Verify(context.Background(), source("http://"+addr+"/prom", nil), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
	assert.NotContains(t, err.Error(), "http://")
}

func TestSchemaMarksEverySecretField(t *testing.T) {
	schema := newAdapter().Schema()
	secret := map[string]bool{}
	for _, f := range schema.Fields {
		secret[f.Key] = f.IsSecret()
	}
	assert.Equal(t, map[string]bool{
		"url": false, "auth": false, "username": false,
		"token": true, "password": true, "headers": true,
	}, secret)
}

func TestWithoutAPolicyEveryDestinationIsRefused(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/api/v1/status/buildinfo": jsonReply(200, `{"status":"success","data":{"version":"2.53.0"}}`),
	})

	_, err := prometheus.New().Verify(context.Background(), source(srv.URL, nil), nil)

	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Empty(t, rec.requests)
}

func TestErrorsNameTheTypedHostNotTheAddressItResolvedTo(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	policy := urlguard.HumanConfigured()
	policy.Resolver = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	a := prometheus.New()
	a.SetURLPolicy(policy)

	_, err = a.Verify(context.Background(), source(fmt.Sprintf("http://prom.lan:%d", port), nil), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("cannot connect to prom.lan:%d", port))
	assert.NotContains(t, err.Error(), "127.0.0.1")
}

func TestTheEncodedBasicCredentialIsMaskedToo(t *testing.T) {
	echo := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "errorType": "internal", "error": "rejected " + r.Header.Get("Authorization")})
	}
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/v1/status/buildinfo": echo, "/api/v1/query": echo})

	_, err := newAdapter().Verify(context.Background(), source(srv.URL, map[string]string{"auth": "basic", "username": "123456"}),
		map[string]string{"password": "glc_secret"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected Basic [redacted]")
	assert.NotContains(t, err.Error(), base64.StdEncoding.EncodeToString([]byte("123456:glc_secret")))
}
