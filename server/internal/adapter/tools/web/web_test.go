package web_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// localPolicy is what a self-hosted install gets after the operator sets
// ALLOW_LOOPBACK_TOOL_URLS: loopback is reachable, everything else internal is
// not. The existing behaviour tests use it because httptest listens on 127.0.0.1
// — which is exactly the point of making it an explicit switch rather than a
// silent allowance.
func localPolicy() urlguard.Policy {
	p := urlguard.PublicOnly()
	p.AllowLoopback = true
	return p
}

type WebToolSuite struct {
	suite.Suite
}

func (s *WebToolSuite) TestName() {
	tool := web.New(1048576)
	s.Equal("fetch_url", tool.Name())
}

func (s *WebToolSuite) TestDefinition() {
	// The description is now filled by application/registry.Register from
	// catalog/system/tools/fetch_url.md, not Definition() itself — see
	// board's golden_tool_definitions_test.go (WP8a) for the same shift.
	reg := registry.New()
	reg.Register(web.New(1048576))
	var found *domain.ToolDefinition
	for _, d := range reg.Definitions() {
		if d.Function.Name == "fetch_url" {
			d := d
			found = &d
		}
	}
	s.Require().NotNil(found)
	s.Equal("function", found.Type)
	s.Equal("fetch_url", found.Function.Name)
	s.NotEmpty(found.Function.Description)
}

func (s *WebToolSuite) TestExecuteSuccess() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	tool := web.New(1048576, web.WithURLPolicy(localPolicy()))
	result := tool.Execute(context.Background(), `{"url":"`+srv.URL+`"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "hello world")
	s.Contains(result.Content, "200")
}

func (s *WebToolSuite) TestExecuteHTMLExtraction() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body><p>extracted text</p></body></html>"))
	}))
	defer srv.Close()

	tool := web.New(1048576, web.WithURLPolicy(localPolicy()))
	result := tool.Execute(context.Background(), `{"url":"`+srv.URL+`"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "extracted text")
	s.NotContains(result.Content, "<html>")
}

func (s *WebToolSuite) TestExecuteInvalidURL() {
	tool := web.New(1048576)
	result := tool.Execute(context.Background(), `{"url":"ftp://not-valid"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "could not fetch")
}

func (s *WebToolSuite) TestExecuteEmptyURL() {
	tool := web.New(1048576)
	result := tool.Execute(context.Background(), `{"url":""}`)
	s.True(result.IsError)
	s.Contains(result.Content, "url is required")
}

func (s *WebToolSuite) TestExecuteInvalidArguments() {
	tool := web.New(1048576)
	result := tool.Execute(context.Background(), `not json`)
	s.True(result.IsError)
	s.Contains(result.Content, "invalid arguments")
}

func (s *WebToolSuite) TestExecuteMaxBytesLimit() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 1000; i++ {
			_, _ = w.Write([]byte("0123456789"))
		}
	}))
	defer srv.Close()

	tool := web.New(50, web.WithURLPolicy(localPolicy()))
	result := tool.Execute(context.Background(), `{"url":"`+srv.URL+`"}`)
	s.False(result.IsError)
	s.LessOrEqual(len(result.Content), 200)
}

// ─── F1: SSRF ─────────────────────────────────────────────────────────────────

// TestExecuteRefusesTheOwnPodExploit is the finding verbatim: with the default
// policy, fetch_url {"url":"http://127.0.0.1:8080/metrics"} reached this pod's
// own listener, where everything outside /v1 and /admin is unauthenticated.
//
// The server here stands in for that listener and must never be touched.
func (s *WebToolSuite) TestExecuteRefusesTheOwnPodExploit() {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("go_goroutines 42"))
	}))
	defer srv.Close()

	tool := web.New(1048576) // the production default: loopback off
	result := tool.Execute(context.Background(), `{"url":"`+srv.URL+`/metrics"}`)

	s.True(result.IsError)
	s.Zero(hits.Load(), "the pod's own endpoint was reached")
	s.NotContains(result.Content, "go_goroutines")
}

func (s *WebToolSuite) TestExecuteRefusesInternalRanges() {
	tool := web.New(1048576)
	for _, raw := range []string{
		"http://127.0.0.1:8080/metrics",
		"http://[::1]:8080/metrics",
		"http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token",
		"http://10.4.0.9/",
		"http://192.168.1.1/",
		"http://172.16.0.1/",
		"http://[fd00::1]/",
		"http://100.64.0.1/",
		"http://0.0.0.0:8080/",
		"http://[::ffff:127.0.0.1]:8080/",
		"file:///etc/passwd",
	} {
		result := tool.Execute(context.Background(), `{"url":"`+raw+`"}`)
		s.True(result.IsError, "%s was allowed", raw)
		s.Equal("could not fetch that URL", result.Content, "%s", raw)
	}
}

// TestExecuteErrorsDoNotDistinguishFailureModes closes the port-scanner half of
// F1: a closed port, a blocked address and a bad scheme have to be one answer,
// or the model can enumerate the pod by reading its own tool output.
func (s *WebToolSuite) TestExecuteErrorsDoNotDistinguishFailureModes() {
	// A real closed port on a public-looking address the guard would allow.
	closed := reservePort(s.T())

	tool := web.New(1048576, web.WithURLPolicy(localPolicy()))
	refused := tool.Execute(context.Background(), `{"url":"http://127.0.0.1:`+closed+`/"}`)

	blockedTool := web.New(1048576)
	blocked := blockedTool.Execute(context.Background(), `{"url":"http://169.254.169.254/"}`)
	badScheme := blockedTool.Execute(context.Background(), `{"url":"gopher://example.test/"}`)

	s.True(refused.IsError)
	s.Equal(refused.Content, blocked.Content, "a refused connection is distinguishable from a blocked address")
	s.Equal(refused.Content, badScheme.Content)
	for _, leak := range []string{"connection refused", "timeout", "no such host", "dial tcp", "127.0.0.1"} {
		s.NotContains(strings.ToLower(refused.Content), leak)
		s.NotContains(strings.ToLower(blocked.Content), leak)
	}
}

// TestExecuteRefusesRedirectToLoopback: a public page answering 302 to the pod's
// own listener is the same attack with one extra hop.
func (s *WebToolSuite) TestExecuteRefusesRedirectToLoopback() {
	var internalHits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHits.Add(1)
		_, _ = w.Write([]byte("go_goroutines 42"))
	}))
	defer internal.Close()

	// The first hop is reachable (self-hosted policy), the redirect target is
	// link-local, which stays blocked for every policy.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/computeMetadata/v1/", http.StatusFound)
	}))
	defer redirector.Close()

	tool := web.New(1048576, web.WithURLPolicy(localPolicy()))
	result := tool.Execute(context.Background(), `{"url":"`+redirector.URL+`"}`)

	s.True(result.IsError)
	s.Zero(internalHits.Load())
	s.Equal("could not fetch that URL", result.Content)
}

// ─── F9: no URL echoed back, no query string logged ───────────────────────────

// TestExecuteNeverEchoesTheURL: the URL an agent hands fetch_url carries the
// agent's own tokens often enough that echoing it into an error message (which
// then lands in model context, transcripts and logs) is a leak on its own.
func (s *WebToolSuite) TestExecuteNeverEchoesTheURL() {
	tool := web.New(1048576)
	const secret = "s3cret-token-value"
	result := tool.Execute(context.Background(), `{"url":"http://10.0.0.5/admin?token=`+secret+`"}`)

	s.True(result.IsError)
	s.NotContains(result.Content, secret)
	s.NotContains(result.Content, "10.0.0.5")
	s.Equal("could not fetch that URL", result.Content)
}

func TestWebToolSuite(t *testing.T) {
	suite.Run(t, new(WebToolSuite))
}

// reservePort returns a port that was just bound and released, so a connection
// to it is refused rather than answered — without guessing a number.
func reservePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	_ = l.Close()
	return port
}

func TestReaderGivesThePageTextWithoutScriptsOrStylesAndRefusesGuardedDestinations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><style>.x{color:red}</style><script>var secret = 1;</script></head><body><!-- note --><h1>Şule'nin Fatura Aracı</h1><p>Send invoices.</p></body></html>`))
	}))
	defer srv.Close()

	r := web.NewReader(1048576, web.WithURLPolicy(localPolicy()))
	text, err := r.Read(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Contains(t, text, "Şule'nin Fatura Aracı")
	assert.Contains(t, text, "Send invoices.")
	assert.NotContains(t, text, "secret")
	assert.NotContains(t, text, "color:red")
	assert.NotContains(t, text, "note")

	_, err = r.Read(context.Background(), srv.URL+"/missing")
	assert.Error(t, err, "a 404 is not a page")

	guarded := web.NewReader(1048576)
	_, err = guarded.Read(context.Background(), srv.URL)
	assert.EqualError(t, err, "could not fetch that URL", "loopback stays refused by default")
}
