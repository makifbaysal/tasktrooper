package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	telemetryapp "github.com/makifbaysal/tasktrooper/server/internal/application/telemetry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type getCall struct {
	path  string
	query url.Values
}

type fakeReader struct {
	calls   []getCall
	answers map[string]string
	status  map[string]int
	err     error
}

func (f *fakeReader) Get(_ context.Context, rel string, q url.Values) (port.TelemetryResponse, error) {
	f.calls = append(f.calls, getCall{rel, q})
	if f.err != nil {
		return port.TelemetryResponse{}, f.err
	}
	status := 200
	if s, ok := f.status[rel]; ok {
		status = s
	}
	body, ok := f.answers[rel]
	if !ok {
		body = `[]`
	}
	return port.TelemetryResponse{Status: status, Body: []byte(body)}, nil
}

func newKit() (*ToolKit, *fakeReader) {
	r := &fakeReader{answers: map[string]string{}, status: map[string]int{}}
	return &ToolKit{Reader: r}, r
}

func run(t *testing.T, kit *ToolKit, name, args string) domain.ToolResult {
	t.Helper()
	for _, ex := range NewExecutors(kit) {
		if ex.Name() == name {
			return ex.Execute(context.Background(), args)
		}
	}
	t.Fatalf("no tool %s", name)
	return domain.ToolResult{}
}

func TestNewExecutorsNeedsAReader(t *testing.T) {
	assert.Nil(t, NewExecutors(nil))
	assert.Nil(t, NewExecutors(&ToolKit{}))
	kit, _ := newKit()
	names := []string{}
	for _, ex := range NewExecutors(kit) {
		names = append(names, ex.Name())
	}
	sort.Strings(names)
	want := append([]string(nil), ToolNames...)
	sort.Strings(want)
	assert.Equal(t, want, names)
}

func TestGetIncidentReadsTheIncidentAndItsTimeline(t *testing.T) {
	kit, r := newKit()
	r.answers["incidents/42"] = `{"id":"42","title":"boom"}`
	r.answers["incidents/42/events"] = `[{"kind":"fired"}]`

	res := run(t, kit, getIncidentToolName, `{"incident_id":"42"}`)
	require.False(t, res.IsError, res.Content)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	assert.Equal(t, "boom", out["incident"].(map[string]any)["title"])
	assert.Len(t, out["events"], 1)
}

func TestGetIncidentRequiresAnId(t *testing.T) {
	kit, _ := newKit()
	res := run(t, kit, getIncidentToolName, `{}`)
	assert.True(t, res.IsError)
}

func TestGetServiceBundlesHealthEndpointsDeploysAndRCA(t *testing.T) {
	kit, r := newKit()
	r.answers["services/orders-api"] = `{"name":"orders-api","errorRate":0.12}`
	r.answers["services/orders-api/endpoints"] = `[{"id":"post-orders","route":"POST /orders"}]`
	r.answers["services/orders-api/deploys"] = `[{"version":"1.4.2"}]`
	r.answers["services/orders-api/endpoints/post-orders/rca"] = `{"children":[{"name":"db"}]}`

	res := run(t, kit, getServiceToolName, `{"service":"orders-api","range":"6h","endpoint_id":"post-orders","baseline":"1d"}`)
	require.False(t, res.IsError, res.Content)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	for _, k := range []string{"service", "endpoints", "deploys", "dependencies", "rca"} {
		assert.Contains(t, out, k)
	}
	for _, c := range r.calls {
		if strings.HasSuffix(c.path, "/rca") {
			assert.Equal(t, "1d", c.query.Get("baseline"))
			assert.Equal(t, "6h", c.query.Get("range"))
		}
	}
}

func TestGetServiceKeepsAnswersWhenOneSubCallFails(t *testing.T) {
	kit, r := newKit()
	r.answers["services/x"] = `{"name":"x"}`
	r.status["services/x/deploys"] = 500
	r.answers["services/x/deploys"] = `{"error":"db down"}`
	res := run(t, kit, getServiceToolName, `{"service":"x"}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "deploys_error")
}

func TestGetServiceFailsWhenTheServiceItselfIsUnknown(t *testing.T) {
	kit, r := newKit()
	r.status["services/ghost"] = 404
	r.answers["services/ghost"] = `{"error":"service not found"}`
	res := run(t, kit, getServiceToolName, `{"service":"ghost"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "404")
}

func TestSearchLogsMapsArgumentsToQuery(t *testing.T) {
	kit, r := newKit()
	res := run(t, kit, searchLogsToolName, `{"service":"orders-api","level":"error","q":"timeout","trace_id":"abc","route":"/orders/:id","range":"6h","env":"production","before":99,"limit":9999}`)
	require.False(t, res.IsError, res.Content)
	require.Len(t, r.calls, 1)
	c := r.calls[0]
	assert.Equal(t, "logs", c.path)
	assert.Equal(t, "orders-api", c.query.Get("service"))
	assert.Equal(t, "error", c.query.Get("level"))
	assert.Equal(t, "timeout", c.query.Get("q"))
	assert.Equal(t, "abc", c.query.Get("traceId"))
	assert.Equal(t, "/orders/:id", c.query.Get("route"))
	assert.Equal(t, "99", c.query.Get("before"))
	assert.Equal(t, "200", c.query.Get("limit"), "clamped")
}

func TestSearchTracesByIdIgnoresTheOtherArguments(t *testing.T) {
	kit, r := newKit()
	r.answers["traces/0123"] = `{"spans":[]}`
	res := run(t, kit, searchTracesToolName, `{"trace_id":"0123","service":"x","errors_only":true}`)
	require.False(t, res.IsError, res.Content)
	require.Len(t, r.calls, 1)
	assert.Equal(t, "traces/0123", r.calls[0].path)
	assert.Empty(t, r.calls[0].query)
}

func TestSearchTracesSearchQuery(t *testing.T) {
	kit, r := newKit()
	run(t, kit, searchTracesToolName, `{"service":"x","endpoint":"POST /orders","errors_only":true,"min_ms":500,"sort":"slowest"}`)
	c := r.calls[0]
	assert.Equal(t, "traces", c.path)
	assert.Equal(t, "true", c.query.Get("errors"))
	assert.Equal(t, "500", c.query.Get("minMs"))
	assert.Equal(t, "slowest", c.query.Get("sort"))
	assert.Equal(t, "POST /orders", c.query.Get("endpoint"))
	assert.Equal(t, "10", c.query.Get("limit"))
}

func TestListErrorsListsOrReadsOneGroup(t *testing.T) {
	kit, r := newKit()
	run(t, kit, listErrorsToolName, `{"service":"x","status":"open"}`)
	assert.Equal(t, "errors", r.calls[0].path)
	assert.Equal(t, "open", r.calls[0].query.Get("status"))
	run(t, kit, listErrorsToolName, `{"fingerprint":"123456","service":"ignored","range":"24h"}`)
	assert.Equal(t, "errors/123456", r.calls[1].path)
	assert.Equal(t, "24h", r.calls[1].query.Get("range"))
	assert.Empty(t, r.calls[1].query.Get("service"))
}

func TestListDeploysAndIncidents(t *testing.T) {
	kit, r := newKit()
	run(t, kit, listDeploysToolName, `{"service":"x","range":"24h"}`)
	assert.Equal(t, "deploys", r.calls[0].path)
	run(t, kit, listIncidentsToolName, `{"service":"x","state":"firing","since":1760000000000}`)
	assert.Equal(t, "incidents", r.calls[1].path)
	assert.Equal(t, "1760000000000", r.calls[1].query.Get("since"))
	assert.Equal(t, "20", r.calls[1].query.Get("limit"))
}

func TestAPIGetPassesQueryAndEscapesNothingItShouldNot(t *testing.T) {
	kit, r := newKit()
	res := run(t, kit, apiGetToolName, `{"path":"infra/hosts","query":{"range":"6h","empty":""}}`)
	require.False(t, res.IsError, res.Content)
	assert.Equal(t, "infra/hosts", r.calls[0].path)
	assert.Equal(t, "6h", r.calls[0].query.Get("range"))
	assert.False(t, r.calls[0].query.Has("empty"))
}

func TestToolsExplainEachFailureMode(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"not configured": {telemetryapp.ErrNotConfigured, prompt.Text(notConfiguredKey)},
		"unauthorized":   {telemetryapp.ErrUnauthorized, prompt.Text(unauthorizedKey)},
		"path":           {telemetryapp.ErrPathNotAllowed, prompt.Text(pathKey)},
		"unreachable":    {fmt.Errorf("%w: connection refused", port.ErrTelemetryUnreachable), "connection refused"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kit, r := newKit()
			r.err = tc.err
			res := run(t, kit, listDeploysToolName, `{}`)
			assert.True(t, res.IsError)
			assert.Contains(t, res.Content, tc.want)
		})
	}
}

func TestAnUpstreamErrorBodyIsSurfacedShort(t *testing.T) {
	kit, r := newKit()
	r.status["deploys"] = 400
	r.answers["deploys"] = `{"error":"invalid range \"2h\""}`
	res := run(t, kit, listDeploysToolName, `{"range":"2h"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "400")
	assert.Contains(t, res.Content, "invalid range")
}

func TestInvalidArgumentsAreRefused(t *testing.T) {
	kit, _ := newKit()
	for _, ex := range NewExecutors(kit) {
		res := ex.Execute(context.Background(), `{not json`)
		assert.True(t, res.IsError, ex.Name())
	}
}

func TestBigAnswersAreTrimmedAndSaySo(t *testing.T) {
	kit, r := newKit()
	items := make([]string, 0, 800)
	for i := 0; i < 800; i++ {
		items = append(items, fmt.Sprintf(`{"ts":%d,"message":"%s"}`, i, strings.Repeat("x", 200)))
	}
	r.answers["logs"] = `{"levels":{"error":800},"lines":[` + strings.Join(items, ",") + `]}`

	res := run(t, kit, searchLogsToolName, `{}`)
	require.False(t, res.IsError)
	assert.LessOrEqual(t, len(res.Content), maxResultBytes)
	var out struct {
		Truncated []cut `json:"truncated"`
		Hint      string
		Data      struct {
			Lines  []any          `json:"lines"`
			Levels map[string]int `json:"levels"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	require.Len(t, out.Truncated, 1)
	assert.Equal(t, "lines", out.Truncated[0].Field)
	assert.Equal(t, 800, out.Truncated[0].Total)
	assert.Equal(t, len(out.Data.Lines), out.Truncated[0].Shown)
	assert.Equal(t, 800, out.Data.Levels["error"], "small fields survive untouched")
	assert.NotEmpty(t, out.Hint)
}

func TestBigTopLevelArraysAreTrimmedToo(t *testing.T) {
	kit, r := newKit()
	items := make([]string, 0, 600)
	for i := 0; i < 600; i++ {
		items = append(items, fmt.Sprintf(`{"v":"%s"}`, strings.Repeat("y", 200)))
	}
	r.answers["errors"] = "[" + strings.Join(items, ",") + "]"
	res := run(t, kit, listErrorsToolName, `{}`)
	require.False(t, res.IsError)
	assert.LessOrEqual(t, len(res.Content), maxResultBytes)
	assert.Contains(t, res.Content, `"truncated"`)
}

func TestSmallAnswersPassThroughUntouched(t *testing.T) {
	kit, r := newKit()
	r.answers["deploys"] = `[{"version":"1.0.0"}]`
	res := run(t, kit, listDeploysToolName, `{}`)
	assert.JSONEq(t, `[{"version":"1.0.0"}]`, res.Content)
}

func TestToolDefinitionsAreStableGolden(t *testing.T) {
	kit, _ := newKit()
	reg := registry.New()
	for _, ex := range NewExecutors(kit) {
		reg.Register(ex)
	}
	defs := reg.Definitions()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Function.Name < defs[j].Function.Name })
	got, err := json.MarshalIndent(defs, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')

	const path = "testdata/tool_definitions.golden.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestEveryToolHasACatalogDocWhoseParamsMatchTheSchema(t *testing.T) {
	kit, _ := newKit()
	for _, ex := range NewExecutors(kit) {
		doc, ok := prompt.Default().ToolDoc(ex.Name())
		require.Truef(t, ok, "%s has no catalog/system/tools/%s.md", ex.Name(), ex.Name())
		for p := range doc.Params {
			assert.NoErrorf(t, registry.ValidateToolParamPath(ex.Definition().Function.Parameters, p), "%s param %s", ex.Name(), p)
		}
	}
}

func TestToolDefinitionsCarryNoGoProse(t *testing.T) {
	kit, _ := newKit()
	for _, ex := range NewExecutors(kit) {
		def := ex.Definition()
		assert.Emptyf(t, def.Function.Description, "%s", ex.Name())
		assertNoParamProse(t, ex.Name(), def.Function.Parameters)
	}
}

func assertNoParamProse(t *testing.T, tool string, node map[string]interface{}) {
	t.Helper()
	if node == nil {
		return
	}
	if d, ok := node["description"].(string); ok && d != "" {
		t.Errorf("%s: a parameter carries a Go-literal description: %q", tool, d)
	}
	if props, ok := node["properties"].(map[string]interface{}); ok {
		for _, raw := range props {
			if sub, ok := raw.(map[string]interface{}); ok {
				assertNoParamProse(t, tool, sub)
			}
		}
	}
}
