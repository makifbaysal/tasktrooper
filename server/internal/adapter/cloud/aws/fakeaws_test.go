package aws

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// route matches one AWS operation the way the real service distinguishes it
// on the wire: the awsJson1.x protocols (ECS, App Runner, CloudWatch Logs)
// tag every request with X-Amz-Target; the query protocol (STS) puts the
// operation name in the form-encoded body; Lambda's REST protocol has
// neither and is matched on method + path.
type route struct {
	match  func(r *http.Request, body []byte) bool
	handle func(w http.ResponseWriter, r *http.Request, body []byte)
}

func newFakeServer(t *testing.T, routes ...route) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		for _, rt := range routes {
			if rt.match(r, body) {
				rt.handle(w, r, body)
				return
			}
		}
		t.Errorf("fake aws: unmatched request %s %s target=%q body=%s",
			r.Method, r.URL.String(), r.Header.Get("X-Amz-Target"), string(body))
		w.WriteHeader(http.StatusNotImplemented)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func staticRoute(match func(r *http.Request, body []byte) bool, status int, ctype, body string) route {
	return route{
		match: match,
		handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
			if ctype != "" {
				w.Header().Set("Content-Type", ctype)
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		},
	}
}

// onTarget answers every request for one awsJson operation with a fixed
// response; use it when a test doesn't need to vary the response per call.
func onTarget(target string, status int, body string) route {
	return staticRoute(func(r *http.Request, _ []byte) bool {
		return r.Header.Get("X-Amz-Target") == target
	}, status, "application/x-amz-json-1.1", body)
}

// onTargetContains additionally requires a substring of the request body to
// match, so two calls to the same operation (e.g. ListServices for two
// different clusters) can get different canned responses.
func onTargetContains(target, bodyContains string, status int, body string) route {
	return staticRoute(func(r *http.Request, b []byte) bool {
		return r.Header.Get("X-Amz-Target") == target && strings.Contains(string(b), bodyContains)
	}, status, "application/x-amz-json-1.1", body)
}

func onTargetDynamic(target string, handle func(w http.ResponseWriter, r *http.Request, body []byte)) route {
	return route{
		match:  func(r *http.Request, _ []byte) bool { return r.Header.Get("X-Amz-Target") == target },
		handle: handle,
	}
}

// onAction matches the STS query protocol, where the operation name is the
// Action field of the form-encoded POST body.
func onAction(action string, status int, body string) route {
	return staticRoute(func(_ *http.Request, b []byte) bool {
		values, err := url.ParseQuery(string(b))
		return err == nil && values.Get("Action") == action
	}, status, "text/xml", body)
}

// onPath matches Lambda's REST protocol by method and exact path.
func onPath(method, path string, status int, body string) route {
	return staticRoute(func(r *http.Request, _ []byte) bool {
		return r.Method == method && r.URL.Path == path
	}, status, "application/json", body)
}

func onPathDynamic(method, path string, handle func(w http.ResponseWriter, r *http.Request, body []byte)) route {
	return route{
		match:  func(r *http.Request, _ []byte) bool { return r.Method == method && r.URL.Path == path },
		handle: handle,
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(http.StatusOK)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

var testNow = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func testProvider(endpoint string) *Provider {
	return &Provider{endpointOverride: endpoint, now: func() time.Time { return testNow }}
}

func testCred() domain.CloudCredential {
	return domain.CloudCredential{
		AccountID: uuid.New(),
		Provider:  domain.CloudAWS,
		Fields: map[string]string{
			fieldAccessKeyID:     "AKIAFAKEFAKEFAKEFAKE",
			fieldSecretAccessKey: "fakefakefakefakefakefakefakefakefakefake",
			fieldRegion:          "us-east-1",
		},
	}
}

func callerIdentityXML(account, arn string) string {
	return `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult>
    <Arn>` + arn + `</Arn>
    <UserId>AIDAEXAMPLE</UserId>
    <Account>` + account + `</Account>
  </GetCallerIdentityResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetCallerIdentityResponse>`
}

func stsErrorXML(code, message string) string {
	return `<ErrorResponse><Error><Code>` + code + `</Code><Message>` + message + `</Message></Error><RequestId>req-err</RequestId></ErrorResponse>`
}
