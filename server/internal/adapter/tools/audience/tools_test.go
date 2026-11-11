package audience

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/audience/audiencetest"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type kitFixture struct {
	kit   *ToolKit
	model *audiencetest.Model
	store *audiencetest.Store
}

func newKit(t *testing.T, agents audiencetest.Agents) *kitFixture {
	t.Helper()
	model := audiencetest.ScriptedModel(func(string) string { return audiencetest.AnswerJSON(7, "yes", 15, true) })
	store := audiencetest.NewStore()
	svc := audience.NewService(audience.Deps{Store: store, Agents: agents, LLM: model, Concurrency: 3, CallTimeout: 5 * time.Second})
	return &kitFixture{kit: &ToolKit{Service: svc}, model: model, store: store}
}

func run(t *testing.T, k *ToolKit, name, args string) domain.ToolResult {
	t.Helper()
	for _, ex := range NewExecutors(k) {
		if ex.Name() == name {
			return ex.Execute(context.Background(), args)
		}
	}
	t.Fatalf("no tool %s", name)
	return domain.ToolResult{}
}

func decode(t *testing.T, res domain.ToolResult) map[string]any {
	t.Helper()
	require.False(t, res.IsError, res.Content)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &m), res.Content)
	return m
}

func TestNewExecutorsNeedsAService(t *testing.T) {
	assert.Empty(t, NewExecutors(nil))
	assert.Empty(t, NewExecutors(&ToolKit{}))
	f := newKit(t, audiencetest.OneAgent())
	assert.Len(t, NewExecutors(f.kit), 15)
}

func TestThePMCanGoFromABriefToAReportOnlyWithTheUsersYes(t *testing.T) {
	f := newKit(t, audiencetest.OneAgent())

	created := decode(t, run(t, f.kit, audienceCreateName, `{"name":"Designers","product":"A kanban for AI agents; the board and the PM chat work today, billing does not.",
	  "segments":[{"name":"Freelancers","count":3}],"pricing":"19 USD a month","language":"Turkish","persona_count":3}`))
	audienceID := created["audience"].(map[string]any)["id"].(string)
	assert.Equal(t, float64(1), created["build_calls"])
	assert.Empty(t, f.model.Calls, "creating a brief asks nobody")

	refused := run(t, f.kit, audienceBuildName, `{"audience_id":"`+audienceID+`"}`)
	require.True(t, refused.IsError)
	assert.Contains(t, refused.Content, "about 1 model calls")
	assert.Contains(t, refused.Content, "user_approved")
	assert.Empty(t, f.model.Calls)
	refused = run(t, f.kit, audienceBuildName, `{"audience_id":"`+audienceID+`","user_approved":false}`)
	assert.True(t, refused.IsError)

	started := decode(t, run(t, f.kit, audienceBuildName, `{"audience_id":"`+audienceID+`","user_approved":true}`))
	assert.Equal(t, "building", started["audience"].(map[string]any)["status"])
	require.Eventually(t, func() bool {
		got := decode(t, run(t, f.kit, audienceGetName, `{"audience_id":"`+audienceID+`"}`))
		return got["audience"].(map[string]any)["status"] == "ready"
	}, 5*time.Second, 20*time.Millisecond)

	got := decode(t, run(t, f.kit, audienceGetName, `{"audience_id":"`+audienceID+`"}`))
	people := got["personas"].([]any)
	require.Len(t, people, 3)
	personaID := people[0].(map[string]any)["id"].(string)
	assert.Equal(t, "A kanban for AI agents; the board and the PM chat work today, billing does not.", got["brief"].(map[string]any)["product"])

	planned := decode(t, run(t, f.kit, studyPlanName, `{"audience_id":"`+audienceID+`","questions":["What would convince you?"],"price_points":[9,19]}`))
	studyID := planned["id"].(string)
	assert.Equal(t, float64(4), planned["calls_estimate"])
	assert.Equal(t, "draft", planned["status"])
	callsBefore := len(f.model.Calls)

	refused = run(t, f.kit, studyStartName, `{"study_id":"`+studyID+`"}`)
	require.True(t, refused.IsError)
	assert.Contains(t, refused.Content, "about 4 model calls")
	assert.Equal(t, callsBefore, len(f.model.Calls), "nobody was asked")

	decode(t, run(t, f.kit, studyStartName, `{"study_id":"`+studyID+`","user_approved":true}`))
	require.Eventually(t, func() bool {
		st := decode(t, run(t, f.kit, studyStatusName, `{"study_id":"`+studyID+`"}`))
		return st["study"].(map[string]any)["status"] == "done"
	}, 5*time.Second, 20*time.Millisecond)

	report := decode(t, run(t, f.kit, studyReportName, `{"study_id":"`+studyID+`"}`))
	r := report["report"].(map[string]any)
	assert.Equal(t, float64(70), r["verdict"].(map[string]any)["score"])
	assert.Contains(t, r["disclaimer"], "synthetic personas")

	list := decode(t, run(t, f.kit, studyListName, `{}`))
	assert.Len(t, list["studies"], 1)
	assert.NotContains(t, run(t, f.kit, studyListName, `{}`).Content, `"metrics"`, "the list does not carry reports")

	talked := decode(t, run(t, f.kit, audienceTalkName, `{"persona_id":"`+personaID+`","message":"Why would you pay?"}`))
	assert.Equal(t, "As I said, it depends on the price.", talked["reply"])
	follow := decode(t, run(t, f.kit, studyFollowUpName, `{"study_id":"`+studyID+`","persona_id":"`+personaID+`","question":"And at five?"}`))
	assert.NotEmpty(t, follow["reply"])
	ask := decode(t, run(t, f.kit, studyAskName, `{"study_id":"`+studyID+`","question":"Who objected?"}`))
	assert.NotEmpty(t, ask["answer"])

	all := run(t, f.kit, studyAskAllName, `{"study_id":"`+studyID+`","question":"One thing missing?"}`)
	require.True(t, all.IsError)
	assert.Contains(t, all.Content, "about 3 model calls")
	ok := decode(t, run(t, f.kit, studyAskAllName, `{"study_id":"`+studyID+`","question":"One thing missing?","user_approved":true}`))
	assert.Equal(t, float64(3), ok["calls"])
}

func TestUpdatingTheBriefKeepsWhatWasNotSaid(t *testing.T) {
	f := newKit(t, audiencetest.OneAgent())
	created := decode(t, run(t, f.kit, audienceCreateName, `{"name":"D","product":"Original","pricing":"9 USD","segments":[{"name":"A"}]}`))
	id := created["audience"].(map[string]any)["id"].(string)

	updated := decode(t, run(t, f.kit, audienceUpdateName, `{"audience_id":"`+id+`","product":"Now with invoicing"}`))
	brief := updated["brief"].(map[string]any)
	assert.Equal(t, "Now with invoicing", brief["product"])
	assert.Equal(t, "9 USD", brief["pricing"])
	assert.Len(t, brief["segments"], 1)
	assert.Contains(t, updated["note"], "audience_build")
}

func TestFailuresExplainThemselves(t *testing.T) {
	f := newKit(t, audiencetest.Agents{})
	created := decode(t, run(t, f.kit, audienceCreateName, `{"name":"D","product":"p"}`))
	id := created["audience"].(map[string]any)["id"].(string)

	res := run(t, f.kit, audienceBuildName, `{"audience_id":"`+id+`","user_approved":true}`)
	require.True(t, res.IsError)
	assert.Contains(t, res.Content, "no enabled agent has a runtime")
	assert.Contains(t, res.Content, "LLM Connection")

	res = run(t, f.kit, audienceGetName, `{"audience_id":"not-an-id"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "audience_id must be the id")
	res = run(t, f.kit, audienceCreateName, `{"name":"x","product":" "}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "needs a description")
	res = run(t, f.kit, audienceListName, `not json`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "invalid arguments")
	res = run(t, f.kit, studyPlanName, `{"audience_id":"`+id+`"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "no people yet")
}

func TestToolDefinitionsAreStableGolden(t *testing.T) {
	f := newKit(t, audiencetest.OneAgent())
	reg := registry.New()
	for _, ex := range NewExecutors(f.kit) {
		reg.Register(ex)
	}
	defs := reg.Definitions()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Function.Name < defs[j].Function.Name })
	got, err := json.MarshalIndent(defs, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')

	const path = "testdata/tool_definitions.golden.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestEveryToolHasACatalogDocWhoseParamsMatchTheSchema(t *testing.T) {
	f := newKit(t, audiencetest.OneAgent())
	for _, ex := range NewExecutors(f.kit) {
		doc, ok := prompt.Default().ToolDoc(ex.Name())
		require.Truef(t, ok, "%s has no catalog/system/tools/%s.md", ex.Name(), ex.Name())
		for p := range doc.Params {
			assert.NoErrorf(t, registry.ValidateToolParamPath(ex.Definition().Function.Parameters, p), "%s param %s", ex.Name(), p)
		}
	}
}

func TestEveryCostlyToolRequiresAndDocumentsItsApproval(t *testing.T) {
	costly := map[string]bool{audienceBuildName: true, studyStartName: true, studyAskAllName: true}
	for _, d := range tools() {
		doc, ok := prompt.Default().ToolDoc(d.name)
		require.True(t, ok)
		if costly[d.name] {
			require.NotNil(t, d.approvalCalls, d.name)
			assert.Contains(t, doc.Params, "user_approved", d.name)
			assert.Contains(t, d.params["required"], "user_approved", d.name)
		} else {
			assert.Nil(t, d.approvalCalls, d.name)
		}
	}
}
