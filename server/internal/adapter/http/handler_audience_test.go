package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/audience/audiencetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type audienceFixture struct {
	app   *fiber.App
	store *audiencetest.Store
	// gate holds every model call while a test has it locked, so a build stays
	// in flight for as long as the test needs.
	gate *sync.RWMutex
}

// gatedLLM makes the fixture's gate hold every model call, the people-writing
// ones included: the scripted answer function only sees interviews.
type gatedLLM struct {
	port.LLMClient
	gate *sync.RWMutex
}

func (g gatedLLM) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	g.gate.RLock()
	defer g.gate.RUnlock()
	return g.LLMClient.Chat(ctx, req)
}

func newAudienceFixture(t *testing.T, agents audiencetest.Agents) *audienceFixture {
	t.Helper()
	answers := audiencetest.AnswerJSON(6, "maybe", 12, true)
	store := audiencetest.NewStore()
	gate := &sync.RWMutex{}
	svc := audience.NewService(audience.Deps{
		Store: store, Agents: agents, LLM: gatedLLM{LLMClient: audiencetest.ScriptedModel(func(string) string { return answers }), gate: gate},
		Concurrency: 3, CallTimeout: 5 * time.Second,
	})
	h := &Handler{audienceSvc: svc}
	app := fiber.New()
	h.registerAudienceRoutes(app)
	return &audienceFixture{app: app, store: store, gate: gate}
}

func (f *audienceFixture) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.app.Test(req, 10000)
	require.NoError(t, err)
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(raw, &v), string(raw))
	return v
}

func (f *audienceFixture) waitFor(t *testing.T, path string, done func(raw []byte) bool) []byte {
	t.Helper()
	var raw []byte
	require.Eventually(t, func() bool {
		_, raw = f.do(t, "GET", path, nil)
		return done(raw)
	}, 5*time.Second, 20*time.Millisecond)
	return raw
}

func TestAudienceScreenFlowFromBriefToReportToAConversation(t *testing.T) {
	f := newAudienceFixture(t, audiencetest.OneAgent())

	status, body := f.do(t, "GET", "/v1/audience-research/status", nil)
	require.Equal(t, 200, status)
	assert.True(t, decode[audience.Runtime](t, body).Ready)

	status, body = f.do(t, "POST", "/v1/audiences", map[string]any{
		"name": "Designers", "persona_target": 3,
		"brief": map[string]any{"product": "A kanban for agents", "segments": []map[string]any{{"name": "Freelancers", "count": 3}}},
	})
	require.Equal(t, 201, status, string(body))
	a := decode[domain.Audience](t, body)
	assert.Equal(t, domain.AudienceDraft, a.Status)

	status, body = f.do(t, "PATCH", "/v1/audiences/"+a.ID.String(), map[string]any{"brief": map[string]any{"product": "Now with invoicing", "pricing": "9 USD"}})
	require.Equal(t, 200, status, string(body))
	assert.Equal(t, "Now with invoicing", decode[domain.Audience](t, body).Brief.Product)

	f.gate.Lock()
	status, body = f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/build", nil)
	require.Equal(t, 202, status, string(body))
	status, _ = f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/build", nil)
	assert.Equal(t, 409, status, "already building")
	f.gate.Unlock()

	raw := f.waitFor(t, "/v1/audiences/"+a.ID.String(), func(raw []byte) bool {
		return decode[audience.AudienceDetail](t, raw).Status == domain.AudienceReady
	})
	detail := decode[audience.AudienceDetail](t, raw)
	require.Len(t, detail.Personas, 3)
	assert.Equal(t, 1, detail.BuildCalls)

	status, body = f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/studies", map[string]any{
		"title": "Would they pay?", "questions": []string{"What would convince you?"}, "price_points": []float64{9, 19}, "currency": "USD",
	})
	require.Equal(t, 201, status, string(body))
	study := decode[domain.Study](t, body)
	assert.Equal(t, 4, study.CallsEstimate)

	status, body = f.do(t, "PATCH", "/v1/audience-studies/"+study.ID.String(), map[string]any{"questions": []string{"Q1", "Q2"}})
	require.Equal(t, 200, status, string(body))
	assert.Equal(t, []string{"Q1", "Q2"}, decode[domain.Study](t, body).Questions)

	status, body = f.do(t, "POST", "/v1/audience-studies/"+study.ID.String()+"/start", nil)
	require.Equal(t, 400, status, "starting needs the person's yes: "+string(body))
	status, body = f.do(t, "POST", "/v1/audience-studies/"+study.ID.String()+"/start", map[string]any{"user_approved": true})
	require.Equal(t, 409, status, "the script is not approved yet: "+string(body))
	assert.Contains(t, string(body), "script_not_approved")
	status, body = f.do(t, "POST", "/v1/audience-studies/"+study.ID.String()+"/script/approve", map[string]any{"version": 99})
	require.Equal(t, 409, status)
	assert.Contains(t, string(body), "script_changed")
	status, body = f.do(t, "GET", "/v1/audience-studies/"+study.ID.String(), nil)
	require.Equal(t, 200, status)
	shown := decode[audience.StudyDetail](t, body)
	assert.Equal(t, domain.ScriptAwaitingApproval, shown.ScriptState)
	status, body = f.do(t, "POST", "/v1/audience-studies/"+study.ID.String()+"/script/approve", map[string]any{"version": shown.ScriptVersion})
	require.Equal(t, 200, status, string(body))
	approved := decode[domain.Study](t, body)
	assert.Equal(t, domain.ScriptApproved, approved.ScriptState)
	assert.Equal(t, "local", approved.ScriptApprovedBy)
	assert.NotNil(t, approved.ScriptApprovedAt)
	status, _ = f.do(t, "POST", "/v1/audience-studies/"+study.ID.String()+"/start", map[string]any{"user_approved": true})
	require.Equal(t, 202, status)
	raw = f.waitFor(t, "/v1/audience-studies/"+study.ID.String(), func(raw []byte) bool {
		return decode[audience.StudyDetail](t, raw).Status == domain.StudyDone
	})
	sd := decode[audience.StudyDetail](t, raw)
	require.NotNil(t, sd.Report)
	assert.Equal(t, 3, sd.Report.Metrics.Respondents)
	assert.Equal(t, "Designers", sd.AudienceName)
	assert.Len(t, sd.Answers, 3)

	status, body = f.do(t, "POST", "/v1/audience-studies/"+study.ID.String()+"/rerun", map[string]any{"changes": "Added a free tier."})
	require.Equal(t, 201, status, string(body))
	rerun := decode[domain.Study](t, body)
	require.NotNil(t, rerun.ParentStudyID)
	assert.Equal(t, study.ID, *rerun.ParentStudyID)
	assert.Equal(t, "Added a free tier.", rerun.Changes)
	status, _ = f.do(t, "GET", "/v1/audience-studies/"+study.ID.String()+"/compare", nil)
	assert.Equal(t, 400, status, "a study that re-ran nothing has no earlier one to compare with")
	status, _ = f.do(t, "GET", "/v1/audience-studies/"+rerun.ID.String()+"/compare", nil)
	assert.Equal(t, 400, status, "the re-run has no report yet")
	status, body = f.do(t, "POST", "/v1/audience-studies/"+rerun.ID.String()+"/script/approve", map[string]any{"version": rerun.ScriptVersion})
	require.Equal(t, 200, status, string(body))
	status, _ = f.do(t, "POST", "/v1/audience-studies/"+rerun.ID.String()+"/start", map[string]any{"user_approved": true})
	require.Equal(t, 202, status)
	f.waitFor(t, "/v1/audience-studies/"+rerun.ID.String(), func(raw []byte) bool {
		return decode[audience.StudyDetail](t, raw).Status == domain.StudyDone
	})
	status, body = f.do(t, "GET", "/v1/audience-studies/"+rerun.ID.String()+"/compare", nil)
	require.Equal(t, 200, status, string(body))
	cmp := decode[audience.StudyComparison](t, body)
	assert.Equal(t, study.ID, cmp.Before.ID)
	assert.Equal(t, rerun.ID, cmp.After.ID)
	assert.Equal(t, 3, cmp.Paired)

	status, body = f.do(t, "GET", "/v1/audience-studies?audience_id="+a.ID.String(), nil)
	require.Equal(t, 200, status)
	assert.Contains(t, string(body), `"studies"`)
	assert.NotContains(t, string(body), `"report"`, "the list carries status, not reports")

	personaID := detail.Personas[0].ID.String()
	status, body = f.do(t, "POST", "/v1/audience-personas/"+personaID+"/messages", map[string]any{"message": "Why maybe?"})
	require.Equal(t, 200, status, string(body))
	assert.Equal(t, "persona", decode[domain.ChatMessage](t, body).Role)
	status, body = f.do(t, "GET", "/v1/audience-personas/"+personaID+"/messages", nil)
	require.Equal(t, 200, status)
	assert.Len(t, decode[struct{ Messages []domain.ChatMessage }](t, body).Messages, 2)

	status, body = f.do(t, "POST", "/v1/audience-studies/"+study.ID.String()+"/personas/"+personaID+"/follow-up", map[string]any{"question": "And free?"})
	require.Equal(t, 200, status, string(body))
	status, body = f.do(t, "POST", "/v1/audience-studies/"+study.ID.String()+"/ask-everyone", map[string]any{"question": "One thing missing?"})
	require.Equal(t, 202, status, string(body))
	assert.Equal(t, float64(3), decode[map[string]any](t, body)["calls"])

	status, _ = f.do(t, "DELETE", "/v1/audience-personas/"+personaID+"/messages", nil)
	assert.Equal(t, 204, status)
	status, _ = f.do(t, "DELETE", "/v1/audience-studies/"+study.ID.String(), nil)
	assert.Equal(t, 204, status)
	status, _ = f.do(t, "DELETE", "/v1/audiences/"+a.ID.String(), nil)
	assert.Equal(t, 204, status)
	status, _ = f.do(t, "GET", "/v1/audiences/"+a.ID.String(), nil)
	assert.Equal(t, 404, status)
}

func TestAudienceRefusalsKeepTheirStatusAndCode(t *testing.T) {
	f := newAudienceFixture(t, audiencetest.Agents{})

	status, body := f.do(t, "GET", "/v1/audience-research/status", nil)
	require.Equal(t, 200, status)
	assert.False(t, decode[audience.Runtime](t, body).Ready)

	status, body = f.do(t, "POST", "/v1/audiences", map[string]any{"name": "X", "brief": map[string]any{}})
	assert.Equal(t, 400, status, string(body))
	assert.Contains(t, string(body), "needs a description")

	status, body = f.do(t, "POST", "/v1/audiences", map[string]any{"name": "X", "brief": map[string]any{"product": "p"}})
	require.Equal(t, 201, status)
	a := decode[domain.Audience](t, body)

	status, body = f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/build", nil)
	assert.Equal(t, 409, status)
	assert.Contains(t, string(body), audienceNoRuntimeCode)

	status, _ = f.do(t, "GET", "/v1/audiences/not-a-uuid", nil)
	assert.Equal(t, 400, status)
	status, _ = f.do(t, "GET", "/v1/audiences/"+uuid.NewString(), nil)
	assert.Equal(t, 404, status)
	status, _ = f.do(t, "GET", "/v1/audience-studies/"+uuid.NewString(), nil)
	assert.Equal(t, 404, status)
	status, _ = f.do(t, "POST", "/v1/audience-personas/"+uuid.NewString()+"/messages", map[string]any{"message": "hi"})
	assert.Equal(t, 404, status)
	status, _ = f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/studies", map[string]any{})
	assert.Equal(t, 400, status, "no people yet")
}

func TestAudienceStudyKindsScriptAndManualReportOverHTTP(t *testing.T) {
	f := newAudienceFixture(t, audiencetest.OneAgent())
	_, body := f.do(t, "POST", "/v1/audiences", map[string]any{
		"name": "Designers", "persona_target": 2, "brief": map[string]any{"product": "A kanban for agents"},
	})
	a := decode[domain.Audience](t, body)
	_, _ = f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/build", nil)
	f.waitFor(t, "/v1/audiences/"+a.ID.String(), func(raw []byte) bool {
		return decode[audience.AudienceDetail](t, raw).Status == domain.AudienceReady
	})

	status, body := f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/studies", map[string]any{"kind": "survey"})
	assert.Equal(t, 400, status, string(body))
	status, body = f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/studies", map[string]any{
		"kind": "message_testing", "inputs": map[string]any{"variants": []string{"Boards for agents", "Ship faster"}},
	})
	require.Equal(t, 201, status, string(body))
	study := decode[domain.Study](t, body)
	assert.Equal(t, domain.KindMessageTesting, study.Kind)
	assert.Equal(t, 4, study.CallsEstimate, "two people, the report and the script")

	status, body = f.do(t, "POST", "/v1/audiences/"+a.ID.String()+"/studies", map[string]any{"title": "Plain"})
	require.Equal(t, 201, status, string(body))
	run := decode[domain.Study](t, body)
	assert.Equal(t, domain.KindUserInterviews, run.Kind)
	f.waitFor(t, "/v1/audience-studies/"+run.ID.String(), func(raw []byte) bool {
		return decode[audience.StudyDetail](t, raw).ScriptState == domain.ScriptAwaitingApproval
	})

	status, body = f.do(t, "PATCH", "/v1/audience-studies/"+run.ID.String(), map[string]any{
		"script": map[string]any{"sections": []map[string]any{{"title": "Mine", "questions": []string{"Why?"}}}},
	})
	require.Equal(t, 200, status, string(body))
	assert.Equal(t, []string{"Why?"}, decode[domain.Study](t, body).Questions)

	status, _ = f.do(t, "POST", "/v1/audience-studies/"+run.ID.String()+"/report", map[string]any{"user_approved": true})
	assert.Equal(t, 400, status, "no report before the interviews")
	status, _ = f.do(t, "POST", "/v1/audience-studies/"+run.ID.String()+"/start", map[string]any{"user_approved": true, "manual_report": true})
	assert.Equal(t, 409, status, "the edited script waits for its approval")
	status, body = f.do(t, "POST", "/v1/audience-studies/"+run.ID.String()+"/script/approve", nil)
	assert.Equal(t, 400, status, "the version that was shown is required")
	status, body = f.do(t, "GET", "/v1/audience-studies/"+run.ID.String(), nil)
	require.Equal(t, 200, status)
	status, _ = f.do(t, "POST", "/v1/audience-studies/"+run.ID.String()+"/script/approve", map[string]any{"version": decode[audience.StudyDetail](t, body).ScriptVersion})
	require.Equal(t, 200, status)
	status, body = f.do(t, "POST", "/v1/audience-studies/"+run.ID.String()+"/start", map[string]any{"user_approved": true, "manual_report": true})
	require.Equal(t, 202, status, string(body))
	f.waitFor(t, "/v1/audience-studies/"+run.ID.String(), func(raw []byte) bool {
		return decode[audience.StudyDetail](t, raw).Status == domain.StudyInterviewed
	})
	status, _ = f.do(t, "POST", "/v1/audience-studies/"+run.ID.String()+"/report", nil)
	assert.Equal(t, 400, status, "the report needs the yes too")
	status, _ = f.do(t, "POST", "/v1/audience-studies/"+run.ID.String()+"/report", map[string]any{"user_approved": true})
	require.Equal(t, 202, status)
	f.waitFor(t, "/v1/audience-studies/"+run.ID.String(), func(raw []byte) bool {
		return decode[audience.StudyDetail](t, raw).Status == domain.StudyDone
	})
	status, _ = f.do(t, "POST", "/v1/audience-studies/"+run.ID.String()+"/retry", map[string]any{"user_approved": true})
	assert.Equal(t, 400, status, "nothing to retry")
}

func TestAudienceRuntimeFollowsThePersonsProviderAndModelPick(t *testing.T) {
	f := newAudienceFixture(t, audiencetest.OneAgent())

	status, body := f.do(t, "PUT", "/v1/audience-research/runtime", map[string]any{"provider": "openai", "model": "gpt-5"})
	require.Equal(t, 200, status, string(body))
	rt := decode[audience.Runtime](t, body)
	assert.True(t, rt.Chosen)
	assert.Equal(t, "openai", rt.Provider)
	assert.Equal(t, "gpt-5", rt.Model)

	_, body = f.do(t, "GET", "/v1/audience-research/status", nil)
	assert.Equal(t, "gpt-5", decode[audience.Runtime](t, body).Model, "the pick outlives the request")

	status, body = f.do(t, "PUT", "/v1/audience-research/runtime", map[string]any{"provider": "", "model": "ignored"})
	require.Equal(t, 200, status)
	rt = decode[audience.Runtime](t, body)
	assert.False(t, rt.Chosen, "an empty provider goes back to the agent's own runtime")
	assert.Equal(t, "test-model", rt.Model)
}
