package observability

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/errordef"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var errNotFound = errors.New("not found")

const goldenToolDefinitionsPath = "testdata/tool_definitions.golden.json"

func definitionsJSON(t *testing.T) []byte {
	t.Helper()
	reg := registry.New()
	for _, ex := range NewExecutors(testKit()) {
		reg.Register(ex)
	}
	defs := reg.Definitions()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Function.Name < defs[j].Function.Name })
	got, err := json.MarshalIndent(defs, "", "  ")
	require.NoError(t, err)
	return append(got, '\n')
}

// UPDATE_GOLDEN=1 rewrites the golden file from the catalog docs.
func TestGoldenToolDefinitions(t *testing.T) {
	got := definitionsJSON(t)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(goldenToolDefinitionsPath, got, 0o644))
	}
	want, err := os.ReadFile(goldenToolDefinitionsPath)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestEveryToolHasACatalogDocWhosePathsMatchItsSchema(t *testing.T) {
	execs := NewExecutors(testKit())
	require.Len(t, execs, 5)
	for _, ex := range execs {
		doc, ok := prompt.Default().ToolDoc(ex.Name())
		require.Truef(t, ok, "%s has no catalog/system/tools/%s.md", ex.Name(), ex.Name())
		def := ex.Definition()
		assert.Empty(t, def.Function.Description, "%s carries Go prose", ex.Name())
		for path := range doc.Params {
			assert.NoError(t, registry.ValidateToolParamPath(def.Function.Parameters, path), "%s: %s", ex.Name(), path)
		}
	}
}

func TestNoToolIsOfferedWithoutItsService(t *testing.T) {
	assert.Empty(t, NewExecutors(&ToolKit{Errors: &fakeErrors{}}))
	execs := NewExecutors(&ToolKit{Components: &fakeComponents{}, Errors: &fakeErrors{}})
	require.Len(t, execs, 1)
	assert.Equal(t, listErrorGroupsToolName, execs[0].Name())
}

type kitFixture struct {
	kit  *ToolKit
	repo uuid.UUID
	api  domain.Component
	web  domain.Component
	jobs *fakeJobs
	defs *fakeDefinitions
}

func newFixture() kitFixture {
	repo := uuid.New()
	api := domain.Component{ID: uuid.New(), RepositoryID: repo, Path: "services/api", Status: domain.ComponentStatusActive}
	web := domain.Component{ID: uuid.New(), RepositoryID: repo, Path: "web", Status: domain.ComponentStatusActive}
	jobs := &fakeJobs{}
	defs := &fakeDefinitions{}
	kit := testKit()
	kit.Components = &fakeComponents{comps: []domain.Component{api, web}}
	kit.Jobs = jobs
	kit.Definitions = defs
	return kitFixture{kit: kit, repo: repo, api: api, web: web, jobs: jobs, defs: defs}
}

func execute(t *testing.T, kit *ToolKit, name string, args any) (map[string]any, domain.ToolResult) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	for _, ex := range NewExecutors(kit) {
		if ex.Name() == name {
			res := ex.Execute(context.Background(), string(raw))
			var body map[string]any
			if !res.IsError {
				require.NoError(t, json.Unmarshal([]byte(res.Content), &body), res.Content)
			}
			return body, res
		}
	}
	t.Fatalf("no tool %s", name)
	return nil, domain.ToolResult{}
}

func TestListErrorGroupsReadsTheJobsComponentAndCarriesDefinitions(t *testing.T) {
	fx := newFixture()
	fx.jobs.job = &domain.AIJob{ID: uuid.New(), RepositoryID: fx.repo, ComponentID: &fx.api.ID, Environment: domain.EnvironmentStaging}
	defID := uuid.New()
	fx.defs.annotate = func(in domain.OpsErrors) domain.OpsErrors {
		in.Groups[0].Definition = &domain.ErrorDefinitionRef{ID: defID, Name: "Timeout", Action: domain.ErrorActionIgnore}
		return in
	}
	errs := fx.kit.Errors.(*fakeErrors)
	seen := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	errs.answer = domain.OpsErrors{Groups: []domain.OpsErrorGroup{
		{ComponentID: fx.api.ID, RepositoryID: fx.repo, RuntimeErrorGroup: domain.RuntimeErrorGroup{
			Fingerprint: "fp1", Message: "timeout", Count: 4, FirstSeen: seen, LastSeen: seen, Sample: "a\nb",
		}},
		{ComponentID: fx.api.ID, RepositoryID: fx.repo, RuntimeErrorGroup: domain.RuntimeErrorGroup{Fingerprint: "fp2"}},
	}}

	body, res := execute(t, fx.kit, listErrorGroupsToolName, map[string]any{"limit": 1, "since": "2d"})
	require.False(t, res.IsError, res.Content)
	assert.Equal(t, domain.EnvironmentStaging, errs.got.Environment)
	assert.Equal(t, []uuid.UUID{fx.api.ID}, errs.got.ComponentIDs)
	assert.WithinDuration(t, time.Now().Add(-48*time.Hour), errs.got.Since, time.Minute)
	assert.Equal(t, true, body["truncated"])
	groups := body["groups"].([]any)
	require.Len(t, groups, 1)
	g := groups[0].(map[string]any)
	assert.Equal(t, "services/api", g["component"])
	assert.Equal(t, defID.String(), g["definition"].(map[string]any)["id"])
}

func TestProposeErrorDefinitionsStoresSuggestionsAndRecordsThemOnTheJob(t *testing.T) {
	fx := newFixture()
	fx.jobs.job = &domain.AIJob{ID: uuid.New(), Kind: domain.AIJobErrorCatalog, RepositoryID: fx.repo, ComponentID: &fx.api.ID}
	stored := domain.ErrorDefinition{ID: uuid.New(), Name: "Validation failed", Category: domain.ErrorCategoryUserError, Action: domain.ErrorActionIgnore}
	existing := uuid.New()
	fx.defs.outcomes = []errordef.ProposalOutcome{
		{Name: "Validation failed", Stored: &stored},
		{Name: "Not found", Existing: &existing},
		{Name: "Broken", Fields: errordef.FieldErrors{"match": "required"}},
	}

	body, res := execute(t, fx.kit, proposeErrorDefinitionsToolName, map[string]any{"definitions": []map[string]any{
		{"name": "Validation failed", "match": map[string]any{"exception_type": "ValidationError"}, "category": "user_error", "action": "ignore", "rationale": "rejected input"},
		{"name": "Not found", "component": "services/api", "match": map[string]any{"status": 404}, "category": "expected", "action": "none"},
		{"name": "Broken", "match": map[string]any{}, "category": "bug", "action": "task"},
		{"name": "Elsewhere", "component": "nope", "match": map[string]any{"status": 500}, "category": "bug", "action": "task"},
		{"name": "Other component", "component": "web", "match": map[string]any{"status": 500}, "category": "bug", "action": "task"},
	}})
	require.False(t, res.IsError, res.Content)

	require.Len(t, fx.defs.proposals, 3)
	assert.Equal(t, domain.ErrorDefinitionCode, fx.defs.proposals[0].Source)
	assert.Equal(t, fx.api.ID.String(), *fx.defs.proposals[0].ComponentID)
	assert.Equal(t, fx.api.ID.String(), *fx.defs.proposals[1].ComponentID)
	assert.Len(t, body["stored"], 1)
	assert.Len(t, body["duplicates"], 1)
	assert.Len(t, body["rejected"], 3, "inside a job scoped to services/api, web is refused")
	require.Len(t, fx.jobs.recorded, 1)
	assert.Equal(t, domain.AIJobItem{Kind: domain.AIJobItemErrorDefinition, ID: stored.ID, Name: stored.Name, Rationale: "rejected input"}, fx.jobs.recorded[0])
}

func TestProposeErrorDefinitionsNeedsARepository(t *testing.T) {
	fx := newFixture()
	_, res := execute(t, fx.kit, proposeErrorDefinitionsToolName, map[string]any{"definitions": []map[string]any{
		{"name": "x", "match": map[string]any{"status": 500}, "category": "bug", "action": "none"},
	}})
	assert.True(t, res.IsError)
}

func TestProposeAlertRuleCreatesADraftUnlessTheSignalHasARule(t *testing.T) {
	fx := newFixture()
	alerts := fx.kit.Alerts.(*fakeAlerts)
	args := map[string]any{
		"repository_id": fx.repo.String(), "component": "services/api", "name": "High error ratio",
		"signal": "http.error_ratio", "condition": map[string]any{"op": "gt", "threshold": 2},
	}

	body, res := execute(t, fx.kit, proposeAlertRuleToolName, args)
	require.False(t, res.IsError, res.Content)
	require.Len(t, alerts.created, 1)
	assert.Equal(t, fx.api.ID.String(), *alerts.created[0].ComponentID)
	assert.Equal(t, "production", *alerts.created[0].Environment)
	assert.Equal(t, "ai", body["rule"].(map[string]any)["created_by"])

	alerts.rules = []domain.AlertRule{{ID: uuid.New(), AlertRuleSpec: domain.AlertRuleSpec{Name: "Mine", Signal: "http.error_ratio"}}}
	body, res = execute(t, fx.kit, proposeAlertRuleToolName, args)
	require.False(t, res.IsError, res.Content)
	assert.Len(t, alerts.created, 1)
	assert.Equal(t, "Mine", body["existing_rule"].(map[string]any)["name"])
}

func TestProposeSignalBindingRecordsOnlyANewAISuggestion(t *testing.T) {
	fx := newFixture()
	jobStart := time.Now()
	fx.jobs.job = &domain.AIJob{ID: uuid.New(), RepositoryID: fx.repo, ComponentID: &fx.web.ID, CreatedAt: jobStart}
	source := uuid.New()
	fx.kit.DataSources.(*fakeDataSources).sources = []domain.DataSource{{ID: source}}
	b := fx.kit.Bindings.(*fakeBindings)
	b.view = domain.SignalBindingView{SignalBinding: domain.SignalBinding{
		ID: uuid.New(), Signal: "web.lcp", Status: domain.SignalBindingSuggested, Source: domain.SignalBindingAI, CreatedAt: jobStart.Add(time.Second),
	}}
	args := map[string]any{"signal": "web.lcp", "data_source_id": source.String(), "query": "q", "rationale": "the lcp histogram"}

	_, res := execute(t, fx.kit, proposeSignalBindingToolName, args)
	require.False(t, res.IsError, res.Content)
	assert.Equal(t, "q", b.got.Query)
	require.Len(t, fx.jobs.recorded, 1)
	assert.Equal(t, domain.AIJobItemSignalBinding, fx.jobs.recorded[0].Kind)

	b.view.Status = domain.SignalBindingDismissed
	_, res = execute(t, fx.kit, proposeSignalBindingToolName, args)
	require.False(t, res.IsError, res.Content)
	assert.Len(t, fx.jobs.recorded, 1)

	b.view.Status = domain.SignalBindingSuggested
	b.view.CreatedAt = jobStart.Add(-time.Hour)
	_, res = execute(t, fx.kit, proposeSignalBindingToolName, args)
	require.False(t, res.IsError, res.Content)
	assert.Len(t, fx.jobs.recorded, 1, "a suggestion from before the job is not this job's")
}

func TestInsideAJobTheToolsRefuseAnythingOutsideItsScope(t *testing.T) {
	fx := newFixture()
	fx.jobs.job = &domain.AIJob{ID: uuid.New(), Kind: domain.AIJobMapSignals, RepositoryID: fx.repo, ComponentID: &fx.api.ID,
		Environment: domain.EnvironmentProduction}
	other := uuid.New()

	_, res := execute(t, fx.kit, listErrorGroupsToolName, map[string]any{"repository_id": other.String()})
	require.True(t, res.IsError)
	assert.Contains(t, res.Content, "inside an AI job")
	assert.Contains(t, res.Content, "another repository")

	_, res = execute(t, fx.kit, listErrorGroupsToolName, map[string]any{"component": "web"})
	require.True(t, res.IsError)
	assert.Contains(t, res.Content, "another component")

	_, res = execute(t, fx.kit, proposeAlertRuleToolName, map[string]any{
		"repository_id": other.String(), "name": "x", "signal": "http.latency", "condition": map[string]any{"op": "gt", "threshold": 1},
	})
	require.True(t, res.IsError)
	assert.Empty(t, fx.kit.Alerts.(*fakeAlerts).created)

	_, res = execute(t, fx.kit, listErrorGroupsToolName, map[string]any{"repository_id": fx.repo.String(), "component": "services/api"})
	assert.False(t, res.IsError, res.Content)
}

func TestInsideAJobListDataSourceMetricsShowsOnlyTheRepositorysSources(t *testing.T) {
	fx := newFixture()
	fx.jobs.job = &domain.AIJob{ID: uuid.New(), Kind: domain.AIJobMapSignals, RepositoryID: fx.repo, ComponentID: &fx.api.ID,
		Environment: domain.EnvironmentProduction}
	own, unused, foreign := uuid.New(), uuid.New(), uuid.New()
	sources := fx.kit.DataSources.(*fakeDataSources)
	sources.sources = []domain.DataSource{{ID: own, Name: "ours"}, {ID: unused, Name: "fresh"}, {ID: foreign, Name: "theirs"}}
	fx.kit.SourceUse = &fakeSourceUse{bindings: []domain.SignalBinding{
		{ComponentID: fx.web.ID, DataSourceID: own},
		{ComponentID: uuid.New(), DataSourceID: foreign},
	}}

	body, res := execute(t, fx.kit, listDataSourceMetricsToolName, map[string]any{})
	require.False(t, res.IsError, res.Content)
	var names []string
	for _, ds := range body["data_sources"].([]any) {
		names = append(names, ds.(map[string]any)["name"].(string))
	}
	assert.ElementsMatch(t, []string{"ours", "fresh"}, names)

	_, res = execute(t, fx.kit, listDataSourceMetricsToolName, map[string]any{"data_source_id": foreign.String()})
	require.True(t, res.IsError)
	assert.Contains(t, res.Content, "only other repositories use")
	assert.Empty(t, sources.discovered)

	_, res = execute(t, fx.kit, proposeSignalBindingToolName, map[string]any{"signal": "http.latency", "data_source_id": foreign.String(), "query": "q"})
	require.True(t, res.IsError)

	fx.jobs.job = nil
	body, res = execute(t, fx.kit, listDataSourceMetricsToolName, map[string]any{})
	require.False(t, res.IsError, res.Content)
	assert.Len(t, body["data_sources"], 3, "outside a job every source is listed")
}

func TestOutsideAJobARunBoundToARepositoryMayNameOnlyThatOne(t *testing.T) {
	fx := newFixture()
	ex := &listErrorGroupsTool{kit: fx.kit}
	bound := registry.ContextWithRepositoryID(context.Background(), fx.repo)

	res := ex.Execute(bound, `{"repository_id":"`+uuid.NewString()+`"}`)
	require.True(t, res.IsError)
	assert.Contains(t, res.Content, "which this run is bound to")

	res = ex.Execute(bound, `{"repository_id":"`+fx.repo.String()+`"}`)
	assert.False(t, res.IsError, res.Content)

	res = ex.Execute(context.Background(), `{"repository_id":"`+fx.repo.String()+`"}`)
	assert.False(t, res.IsError, res.Content)
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		raw  string
		want time.Time
		err  bool
	}{
		{name: "default", raw: "", want: now.Add(-time.Hour)},
		{name: "days", raw: "7d", want: now.Add(-7 * 24 * time.Hour)},
		{name: "duration", raw: "90m", want: now.Add(-90 * time.Minute)},
		{name: "instant", raw: "2026-10-16T00:00:00Z", want: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)},
		{name: "negative", raw: "-1h", err: true},
		{name: "garbage", raw: "yesterday", err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSince(tt.raw, now, time.Hour)
			if tt.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
