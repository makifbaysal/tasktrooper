package httpapi_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

type recorder struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (r *recorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func serve(t *testing.T, h http.HandlerFunc) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func source(url string, config map[string]string) domain.DataSource {
	c := map[string]string{"url": url}
	for k, v := range config {
		c[k] = v
	}
	return domain.DataSource{Config: c}
}

func call(t *testing.T, policy urlguard.Policy, ds domain.DataSource, secrets map[string]string, path string) (httpapi.Response, *httpapi.Conn, error) {
	t.Helper()
	ep, err := httpapi.NewEndpoint("Loki", ds, secrets)
	require.NoError(t, err)
	c, err := httpapi.Open(context.Background(), policy, ep, 5*time.Second)
	if err != nil {
		return httpapi.Response{}, nil, err
	}
	t.Cleanup(c.Close)
	r, err := c.Do(context.Background(), httpapi.Request{Path: path, Limit: 1 << 20})
	return r, c, err
}

func TestWithoutAPolicyEveryDestinationIsRefused(t *testing.T) {
	srv, rec := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "ok") })

	_, _, err := call(t, urlguard.Policy{}, source(srv.URL, nil), nil, "/ready")

	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Zero(t, rec.count())
}

func TestTheHumanConfiguredPolicyRefusesMetadataAndAPublicOnlyOneLoopback(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "ok") })

	_, _, err := call(t, urlguard.HumanConfigured(), source(srv.URL, nil), nil, "/ready")
	require.NoError(t, err, "a local Loki lives on 127.0.0.1")

	for _, raw := range []string{"http://169.254.169.254/latest", "http://[fd00:ec2::254]", "http://metadata.google.internal"} {
		_, _, err := call(t, urlguard.HumanConfigured(), source(raw, nil), nil, "/ready")
		assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%s: %v", raw, err)
	}
	_, _, err = call(t, urlguard.PublicOnly(), source(srv.URL, nil), nil, "/ready")
	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
}

func TestARedirectToAnotherHostIsRefused(t *testing.T) {
	srv, rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.2:9/ready", http.StatusFound)
	})

	_, _, err := call(t, urlguard.HumanConfigured(), source(srv.URL, map[string]string{"auth": "bearer"}), map[string]string{"token": "s3cret-token"}, "/ready")

	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Equal(t, 1, rec.count())
}

func TestAPathThatClimbsOutOfTheBaseIsRefused(t *testing.T) {
	srv, rec := serve(t, func(w http.ResponseWriter, _ *http.Request) {})
	for _, path := range []string{"/../admin", "/a/%2e%2e/b", "/x?y=1", "/x#frag"} {
		_, _, err := call(t, urlguard.HumanConfigured(), source(srv.URL+"/loki", nil), nil, path)
		assert.Error(t, err, path)
	}
	assert.Zero(t, rec.count())
}

func TestThePathHangsOffTheBaseURLsOwnPath(t *testing.T) {
	var got string
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) { got = r.URL.EscapedPath() })

	_, _, err := call(t, urlguard.HumanConfigured(), source(srv.URL+"/tempo", nil), nil, "/api/search/tag/a%2Fb/values")

	require.NoError(t, err)
	assert.Equal(t, "/tempo/api/search/tag/a%2Fb/values", got)
}

func TestEachAuthSendsItsHeaderAndTheExtraHeadersWin(t *testing.T) {
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("123456:glc_pw"))
	tests := []struct {
		name    string
		config  map[string]string
		secrets map[string]string
		want    string
	}{
		{name: "none", config: map[string]string{"auth": "none"}, want: ""},
		{name: "bearer", config: map[string]string{"auth": "bearer"}, secrets: map[string]string{"token": "tok-1"}, want: "Bearer tok-1"},
		{name: "basic", config: map[string]string{"auth": "basic", "username": "123456"}, secrets: map[string]string{"password": "glc_pw"}, want: basic},
		{name: "api key", config: map[string]string{"auth": "api_key"}, secrets: map[string]string{"api_key": "ZW5jb2RlZA=="}, want: "ApiKey ZW5jb2RlZA=="},
		{name: "own header", config: map[string]string{"auth": "bearer"}, secrets: map[string]string{"token": "tok-1", "headers": "Authorization: Custom x\nX-Scope-OrgID: tenant-1"}, want: "Custom x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got http.Header
			srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() })

			_, _, err := call(t, urlguard.HumanConfigured(), source(srv.URL, tt.config), tt.secrets, "/ready")

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Get("Authorization"))
			assert.Equal(t, "TaskTrooper", got.Get("User-Agent"))
		})
	}
}

func TestStatusErrorsMaskEverySecretAndTheEncodedBasicValue(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, "rejected %s for tenant tenant-secret-1", r.Header.Get("Authorization"))
	})
	secrets := map[string]string{"password": "glc_secret", "headers": "X-Scope-OrgID: tenant-secret-1"}

	r, c, err := call(t, urlguard.HumanConfigured(), source(srv.URL, map[string]string{"auth": "basic", "username": "123456"}), secrets, "/ready")
	require.NoError(t, err)
	msg := c.StatusError("readiness", r).Error()

	assert.Contains(t, msg, "authentication failed (HTTP 401)")
	assert.Contains(t, msg, "rejected Basic [redacted]")
	assert.NotContains(t, msg, base64.StdEncoding.EncodeToString([]byte("123456:glc_secret")))
	assert.NotContains(t, msg, "tenant-secret-1")
}

func TestAMaskInsideAnotherNeverLeavesTheLongerOneShowing(t *testing.T) {
	key := `{"private_key_id":"kid-0123","private_key":"MATERIAL-xyz"}`
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, "bad key "+key)
	})
	for range 20 {
		c, err := httpapi.Open(context.Background(), urlguard.HumanConfigured(), httpapi.Endpoint{
			Base: srv.URL, Masks: []string{"kid-0123", key, "MATERIAL-xyz"}, API: "Test",
		}, time.Second)
		require.NoError(t, err)
		r, err := c.Do(context.Background(), httpapi.Request{Path: "/x", Limit: 1 << 20})
		require.NoError(t, err)
		assert.Equal(t, "bad key [redacted]", c.Detail(r))
		c.Close()
	}
}

func TestDetailReadsJSONAndPlainTextRefusals(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "elasticsearch", body: `{"error":{"root_cause":[{"reason":"inner"}],"type":"parsing_exception","reason":"unknown query [nope]"},"status":400}`, want: "unknown query [nope]"},
		{name: "flat error", body: `{"error":"bad tenant"}`, want: "bad tenant"},
		{name: "message", body: `{"message":"rate limited"}`, want: "rate limited"},
		{name: "plain text", body: "parse error at line 1, col 2: syntax error\nmore", want: "parse error at line 1, col 2: syntax error"},
		{name: "html", body: "<html>bad gateway</html>", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, tt.body)
			})
			r, c, err := call(t, urlguard.HumanConfigured(), source(srv.URL, nil), nil, "/x")
			require.NoError(t, err)
			assert.Equal(t, tt.want, c.Detail(r))
		})
	}
}

type resolverFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

func TestTransportErrorsNameTheTypedHostNotTheAddressItResolvedTo(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	policy := urlguard.HumanConfigured()
	policy.Resolver = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})

	_, _, err = call(t, policy, source(fmt.Sprintf("http://loki.lan:%d", port), nil), nil, "/ready")

	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("cannot connect to loki.lan:%d", port))
	assert.NotContains(t, err.Error(), "127.0.0.1")
}

func TestAnAnswerOverTheLimitIsAnError(t *testing.T) {
	srv, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, 2<<20)) })
	ep, err := httpapi.NewEndpoint("Loki", source(srv.URL, nil), nil)
	require.NoError(t, err)
	c, err := httpapi.Open(context.Background(), urlguard.HumanConfigured(), ep, 5*time.Second)
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Do(context.Background(), httpapi.Request{Path: "/x", Limit: 1 << 20})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than 1 MiB")
}
