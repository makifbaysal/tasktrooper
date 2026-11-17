package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	telemetryapp "github.com/makifbaysal/tasktrooper/server/internal/application/telemetry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func asMap(t *testing.T, res domain.ToolResult) map[string]any {
	t.Helper()
	require.False(t, res.IsError, res.Content)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	return out
}

func TestExplainEndpointResolvesARouteToItsSlug(t *testing.T) {
	kit, r := newKit()
	r.answers["services/orders-api/endpoints"] = `[{"id":"get-orders","method":"GET","route":"/orders"},{"id":"post-orders","method":"POST","route":"/orders"}]`
	r.answers["services/orders-api/endpoints/post-orders/rca"] = `{"children":[{"name":"db"}]}`

	out := asMap(t, run(t, kit, explainEndpointToolName, `{"service":"orders-api","endpoint":"post  /orders","range":"6h","baseline":"1d"}`))
	assert.Equal(t, "post-orders", out["endpoint_id"])
	assert.Contains(t, out, "rca")
	last := r.calls[len(r.calls)-1]
	assert.Equal(t, "services/orders-api/endpoints/post-orders/rca", last.path)
	assert.Equal(t, "1d", last.query.Get("baseline"))
	assert.Equal(t, "6h", last.query.Get("range"))
}

func TestExplainEndpointTakesASlugDirectlyAndNamesAnUnknownRoute(t *testing.T) {
	kit, r := newKit()
	asMap(t, run(t, kit, explainEndpointToolName, `{"service":"a","endpoint":"get-x"}`))
	require.Len(t, r.calls, 1)
	assert.Equal(t, "services/a/endpoints/get-x/rca", r.calls[0].path)

	res := run(t, kit, explainEndpointToolName, `{"service":"a","endpoint":"GET /missing"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "GET /missing")
}

func TestListMonitorsIncludesProposedSuggestionsByDefault(t *testing.T) {
	kit, r := newKit()
	r.answers["monitors"] = `[{"id":"m1","source":"agent"}]`
	r.answers["suggestions"] = `[{"id":"s1"}]`
	out := asMap(t, run(t, kit, listMonitorsToolName, `{"service":"orders-api"}`))
	assert.Contains(t, out, "monitors")
	assert.Contains(t, out, "suggestions")
	assert.Equal(t, "orders-api", r.calls[0].query.Get("service"))
	assert.Equal(t, "proposed", r.calls[1].query.Get("state"))

	kit, r = newKit()
	out = asMap(t, run(t, kit, listMonitorsToolName, `{"suggestions":"none"}`))
	assert.NotContains(t, out, "suggestions")
	assert.Len(t, r.calls, 1)
}

func TestListSLOsFiltersByServiceOrReadsOne(t *testing.T) {
	kit, r := newKit()
	run(t, kit, listSLOsToolName, `{"service":"orders-api"}`)
	assert.Equal(t, "kpis", r.calls[0].path)
	assert.Equal(t, "orders-api", r.calls[0].query.Get("serviceId"))
	run(t, kit, listSLOsToolName, `{"slo_id":"k1"}`)
	assert.Equal(t, "kpis/k1", r.calls[1].path)
}

func TestListLoadTestsHidesSpecsAndDomainTokens(t *testing.T) {
	kit, r := newKit()
	r.answers["loadtests/scenarios"] = `[{"id":"sc1","name":"x","spec":{"target":"https://a"}}]`
	r.answers["loadtests/domains"] = `[{"host":"a.example.com","verified":true,"method":"dns","token":"secret-token"}]`
	res := run(t, kit, listLoadTestsToolName, `{}`)
	require.False(t, res.IsError, res.Content)
	assert.NotContains(t, res.Content, "secret-token")
	assert.NotContains(t, res.Content, `"spec"`)
	assert.Contains(t, res.Content, "a.example.com")

	run(t, kit, listLoadTestsToolName, `{"run_id":"r1"}`)
	assert.Equal(t, "loadtests/runs/r1", r.calls[len(r.calls)-1].path)
}

const validMonitor = `{"name":"orders p95","kind":"latency","target":{"service":"orders-api"},"query":{"percentile":"p95"},"condition":{"op":">","threshold":800},"severity":"warning"}`

func TestProposeMonitorWithoutApprovalOnlyPreviews(t *testing.T) {
	kit, r := newKit()
	r.writeAnswers["POST monitors/preview"] = `{"episodes":2,"firingNow":false}`
	out := asMap(t, run(t, kit, proposeMonitorToolName, `{"monitor":`+validMonitor+`}`))
	assert.Equal(t, true, out["dry_run"])
	assert.Contains(t, out, "preview")
	assert.Contains(t, out["approval_needed"], "user_approved")
	require.Len(t, r.writes, 1, "the preview is the only request sent")
	assert.Equal(t, "monitors/preview", r.writes[0].path)
	assert.Equal(t, "24h", r.writes[0].query.Get("range"))
	assert.Contains(t, r.writes[0].body, `"window":"5m"`, "the window is stated explicitly")
	assert.Contains(t, r.writes[0].body, `"channels":[]`)
}

func TestProposeMonitorWithApprovalCreates(t *testing.T) {
	kit, r := newKit()
	r.writeAnswers["POST monitors"] = `{"id":"m9"}`
	out := asMap(t, run(t, kit, proposeMonitorToolName, `{"monitor":`+validMonitor+`,"user_approved":true}`))
	assert.Contains(t, out, "created")
	require.Len(t, r.writes, 1)
	assert.Equal(t, http.MethodPost, r.writes[0].method)
	assert.Equal(t, "monitors", r.writes[0].path)
}

func TestProposeMonitorRefusesABadMonitor(t *testing.T) {
	kit, r := newKit()
	res := run(t, kit, proposeMonitorToolName, `{"monitor":{"name":"x","kind":"latency","condition":{"op":">","threshold":1},"severity":"urgent"}}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "severity")
	assert.Empty(t, r.writes)
}

func TestReplacingAMonitorAnAgentDidNotCreateIsRefusedBeforeApproval(t *testing.T) {
	kit, r := newKit()
	r.answers["monitors/m1"] = `{"id":"m1","source":"manual"}`
	res := run(t, kit, proposeMonitorToolName, `{"monitor":`+validMonitor+`,"replace_id":"m1","user_approved":true}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "not created by an agent")
	assert.Empty(t, r.writes)

	r.answers["monitors/m1"] = `{"id":"m1","source":"agent"}`
	out := asMap(t, run(t, kit, proposeMonitorToolName, `{"monitor":`+validMonitor+`,"replace_id":"m1","user_approved":true}`))
	assert.Contains(t, out, "updated")
	assert.Equal(t, http.MethodPut, r.writes[0].method)
	assert.Equal(t, "monitors/m1", r.writes[0].path)
}

func TestTheWriteToolsMapAnOwnershipRefusal(t *testing.T) {
	kit, r := newKit()
	r.answers["kpis/k1"] = `{"source":"agent"}`
	r.writeErr = telemetryapp.ErrNotOwned
	res := run(t, kit, proposeSLOToolName, `{"slo":{"name":"Checkout","target":{"kind":"endpoint","serviceId":"shop","ref":"POST /checkout"},"sli":"availability","objective":0.99,"window":"28d"},"replace_id":"k1","user_approved":true}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "not created by an agent")
}

func TestProposeSLOValidatesAndNeedsApproval(t *testing.T) {
	kit, r := newKit()
	bad := run(t, kit, proposeSLOToolName, `{"slo":{"name":"Checkout","target":{"kind":"endpoint","serviceId":"shop","ref":"post-checkout"},"sli":"availability","objective":0.99,"window":"28d"}}`)
	assert.True(t, bad.IsError)
	assert.Contains(t, bad.Content, "METHOD /route")

	slo := `{"name":"Checkout","target":{"kind":"endpoint","serviceId":"shop","ref":"POST /checkout"},"sli":"latency","good":{"thresholdMs":300},"objective":0.99,"window":"28d","alerts":[{"kind":"burn_rate","longWindow":"1h","shortWindow":"5m","threshold":14.4,"severity":"critical"}]}`
	out := asMap(t, run(t, kit, proposeSLOToolName, `{"slo":`+slo+`}`))
	assert.Equal(t, true, out["dry_run"])
	assert.Empty(t, r.writes)

	asMap(t, run(t, kit, proposeSLOToolName, `{"slo":`+slo+`,"user_approved":true}`))
	require.Len(t, r.writes, 1)
	assert.Equal(t, "kpis", r.writes[0].path)
	assert.Contains(t, r.writes[0].body, `"channels":[]`)
}

func TestAcceptSuggestionShowsItAndAcceptsOnlyWhenApproved(t *testing.T) {
	kit, r := newKit()
	r.answers["suggestions"] = `[{"id":"s1","title":"checkout 5xx"}]`
	out := asMap(t, run(t, kit, acceptSuggestionToolName, `{"suggestion_id":"s1","severity":"critical"}`))
	assert.Contains(t, out, "suggestion")
	assert.Empty(t, r.writes)

	asMap(t, run(t, kit, acceptSuggestionToolName, `{"suggestion_id":"s1","severity":"critical","user_approved":true}`))
	require.Len(t, r.writes, 1)
	assert.Equal(t, "suggestions/s1/accept", r.writes[0].path)
	assert.JSONEq(t, `{"severity":"critical"}`, r.writes[0].body)
}

const newScenario = `{"name":"checkout baseline (agent)","spec":{"target":"https://staging.shop.example.com","steps":["GET /v1/products"],"levels":[{"name":"steady","ramp":30,"hold":60,"target":5}]}}`

func loadTestKit() (*ToolKit, *fakeReader) {
	kit, r := newKit()
	r.answers["loadtests/domains"] = `[{"host":"shop.example.com","verified":true,"method":"dns"},{"host":"other.example.com","verified":false,"method":"http"}]`
	return kit, r
}

func TestRunLoadTestCreatesWithTheAbortGateAndRunsOnlyWhenApproved(t *testing.T) {
	kit, r := loadTestKit()
	out := asMap(t, run(t, kit, runLoadTestToolName, `{"scenario":`+newScenario+`}`))
	assert.Equal(t, true, out["dry_run"])
	assert.Equal(t, "staging", out["environment"])
	assert.Empty(t, r.writes)

	r.writeAnswers["POST loadtests/scenarios"] = `{"id":"sc7"}`
	r.writeAnswers["POST loadtests/scenarios/sc7/runs"] = `{"id":"run1","state":"queued"}`
	out = asMap(t, run(t, kit, runLoadTestToolName, `{"scenario":`+newScenario+`,"user_approved":true}`))
	assert.Equal(t, "sc7", out["scenario_id"])
	require.Len(t, r.writes, 2)
	assert.Contains(t, r.writes[0].body, `"abortErrorRate":0.5`)
	assert.Contains(t, r.writes[0].body, `"abortP95Ms":10000`)
	assert.Contains(t, r.writes[0].body, `"state":"ready"`)
	assert.Equal(t, "loadtests/scenarios/sc7/runs", r.writes[1].path)
}

func TestRunLoadTestRefusesAnUnverifiedDomain(t *testing.T) {
	kit, r := loadTestKit()
	res := run(t, kit, runLoadTestToolName, `{"scenario":{"name":"x","spec":{"target":"https://other.example.com","steps":["GET /"],"levels":[]}},"user_approved":true}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "other.example.com is not a verified")
	assert.Empty(t, r.writes)
}

func TestRunLoadTestRefusesADraftScenario(t *testing.T) {
	kit, r := loadTestKit()
	r.answers["loadtests/scenarios/sc1"] = `{"id":"sc1","state":"draft","target":"https://shop.example.com"}`
	res := run(t, kit, runLoadTestToolName, `{"scenario_id":"sc1","user_approved":true}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "draft")
	assert.Empty(t, r.writes)
}

type fakeTargets []domain.DeployTarget

func (f fakeTargets) Targets(context.Context, uuid.UUID) ([]domain.DeployTarget, error) { return f, nil }

func TestRunLoadTestKeepsOffProductionUnlessAskedFor(t *testing.T) {
	kit, r := loadTestKit()
	kit.Targets = fakeTargets{
		{Env: domain.DeployEnvStage, BaseURL: "https://staging.shop.example.com"},
		{Env: domain.DeployEnvProd, BaseURL: "https://api.shop.example.com"},
	}
	r.answers["loadtests/scenarios/sc1"] = `{"id":"sc1","state":"ready","target":"https://api.shop.example.com"}`
	ctx := registry.ContextWithRepositoryID(context.Background(), uuid.New())

	var res domain.ToolResult
	for _, ex := range NewExecutors(kit) {
		if ex.Name() == runLoadTestToolName {
			res = ex.Execute(ctx, `{"scenario_id":"sc1","user_approved":true}`)
		}
	}
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "production")
	assert.Empty(t, r.writes)

	for _, ex := range NewExecutors(kit) {
		if ex.Name() == runLoadTestToolName {
			res = ex.Execute(ctx, `{"scenario_id":"sc1","environment":"production","user_approved":true}`)
		}
	}
	require.False(t, res.IsError, res.Content)
	require.Len(t, r.writes, 1)
	assert.Equal(t, "loadtests/scenarios/sc1/runs", r.writes[0].path)
}

func TestStopLoadTestNeedsNoApproval(t *testing.T) {
	kit, r := newKit()
	out := asMap(t, run(t, kit, stopLoadTestToolName, `{"run_id":"run1"}`))
	assert.Contains(t, out, "stopped")
	require.Len(t, r.writes, 1)
	assert.Equal(t, "loadtests/runs/run1/stop", r.writes[0].path)
	assert.Empty(t, r.writes[0].body)
}
