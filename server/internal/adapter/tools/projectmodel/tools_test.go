package projectmodel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"

	appprojectmodel "github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
)

func newTestKit(t *testing.T) (*ToolKit, *fakeStore, *fakeRepos) {
	t.Helper()
	store := newFakeStore()
	repos := newFakeRepos()
	svc := appprojectmodel.NewService(appprojectmodel.Deps{Store: store, Repos: repos})
	return &ToolKit{Model: svc}, store, repos
}

func repoCtx(id uuid.UUID) context.Context {
	return registry.ContextWithRepositoryID(context.Background(), id)
}

func seedComponent(store *fakeStore, repoID uuid.UUID, path string, role domain.ComponentRole) domain.Component {
	c := domain.Component{
		ID:           uuid.New(),
		RepositoryID: repoID,
		Path:         path,
		Role:         domain.Detected(role, domain.ConfidenceExact),
		Status:       domain.ComponentStatusActive,
	}
	saved, _ := store.SaveComponent(context.Background(), c)
	return saved
}

func TestNewExecutors(t *testing.T) {
	assert.Nil(t, NewExecutors(nil))
	assert.Nil(t, NewExecutors(&ToolKit{}))

	kit, _, _ := newTestKit(t)
	execs := NewExecutors(kit)
	require.Len(t, execs, 4)
	names := make(map[string]bool, len(execs))
	for _, e := range execs {
		names[e.Name()] = true
	}
	assert.True(t, names[getBriefToolName])
	assert.True(t, names[listChecksToolName])
	assert.True(t, names[listLinksToolName])
	assert.True(t, names[appprojectmodel.RecordNoteToolName])
}

func TestBriefTool_ScopesToComponent(t *testing.T) {
	kit, store, repos := newTestKit(t)
	repoID := repos.put(domain.Repository{Name: "demo"}).ID
	seedComponent(store, repoID, "apps/api", domain.ComponentRoleBackend)
	seedComponent(store, repoID, "apps/web", domain.ComponentRoleFrontend)

	tool := &briefTool{kit: kit}
	res := tool.Execute(repoCtx(repoID), `{"component":"apps/api"}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "apps/api")
	assert.NotContains(t, res.Content, "apps/web")
}

func TestBriefTool_UnknownComponentErrors(t *testing.T) {
	kit, _, repos := newTestKit(t)
	repoID := repos.put(domain.Repository{Name: "demo"}).ID

	tool := &briefTool{kit: kit}
	res := tool.Execute(repoCtx(repoID), `{"component":"does/not/exist"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "no active component")
}

func TestBriefTool_NoRepositoryErrors(t *testing.T) {
	kit, _, _ := newTestKit(t)
	tool := &briefTool{kit: kit}
	res := tool.Execute(context.Background(), `{}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "repository")
}

func TestBriefTool_RepositoryIDArgOverridesContext(t *testing.T) {
	kit, store, repos := newTestKit(t)
	ctxRepo := repos.put(domain.Repository{Name: "ctx-repo"}).ID
	argRepo := repos.put(domain.Repository{Name: "arg-repo"}).ID
	seedComponent(store, argRepo, ".", domain.ComponentRoleBackend)

	tool := &briefTool{kit: kit}
	res := tool.Execute(repoCtx(ctxRepo), `{"repository_id":"`+argRepo.String()+`"}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "arg-repo")
}

func TestBriefTool_InvalidArgumentsError(t *testing.T) {
	kit, _, _ := newTestKit(t)
	tool := &briefTool{kit: kit}
	res := tool.Execute(context.Background(), `not json`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "invalid arguments")
}

func TestChecksTool_GroupsByComponentAndFiltersRequired(t *testing.T) {
	kit, store, repos := newTestKit(t)
	repoID := repos.put(domain.Repository{Name: "demo"}).ID
	api := seedComponent(store, repoID, "apps/api", domain.ComponentRoleBackend)
	seedComponent(store, repoID, "apps/web", domain.ComponentRoleFrontend)

	_, _ = store.SaveCheck(context.Background(), domain.ComponentCheck{
		RepositoryID: repoID,
		ComponentID:  api.ID,
		Workflow:     ".github/workflows/ci.yml",
		JobKey:       "test",
		JobName:      "Test",
		Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckTest)},
		Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateRequired)},
		LocalCommands: domain.Fact[[]domain.LocalCommand]{Detected: &[]domain.LocalCommand{
			{Dir: "apps/api", Argv: []string{"go", "test", "./..."}},
		}},
		Status: domain.ModelStatusActive,
	})

	tool := &checksTool{kit: kit}
	res := tool.Execute(repoCtx(repoID), `{}`)
	require.False(t, res.IsError, res.Content)

	var payload struct {
		Components []componentChecksView `json:"components"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &payload))
	require.Len(t, payload.Components, 2)

	byPath := map[string]componentChecksView{}
	for _, c := range payload.Components {
		byPath[c.Path] = c
	}
	require.Len(t, byPath["apps/api"].Checks, 1)
	check := byPath["apps/api"].Checks[0]
	assert.Equal(t, "Test", check.Job)
	assert.Equal(t, "test", check.Purpose)
	assert.Equal(t, "required", check.Gate)
	assert.Equal(t, []string{"cd apps/api && go test ./..."}, check.LocalCommands)
	assert.True(t, check.Required)
	assert.False(t, check.Missing)
	assert.Empty(t, byPath["apps/web"].Checks)
}

func TestChecksTool_FiltersByComponentPath(t *testing.T) {
	kit, store, repos := newTestKit(t)
	repoID := repos.put(domain.Repository{Name: "demo"}).ID
	seedComponent(store, repoID, "apps/api", domain.ComponentRoleBackend)
	seedComponent(store, repoID, "apps/web", domain.ComponentRoleFrontend)

	tool := &checksTool{kit: kit}
	res := tool.Execute(repoCtx(repoID), `{"component":"apps/web"}`)
	require.False(t, res.IsError, res.Content)

	var payload struct {
		Components []componentChecksView `json:"components"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &payload))
	require.Len(t, payload.Components, 1)
	assert.Equal(t, "apps/web", payload.Components[0].Path)
}

func TestLinksTool_ListsOutgoingIncomingAndFiltersDirection(t *testing.T) {
	kit, store, repos := newTestKit(t)
	repoID := repos.put(domain.Repository{Name: "demo"}).ID
	otherRepoID := repos.put(domain.Repository{Name: "other"}).ID

	api := seedComponent(store, repoID, "apps/api", domain.ComponentRoleBackend)
	worker := seedComponent(store, otherRepoID, ".", domain.ComponentRoleWorker)

	resource, _ := store.EnsureResource(context.Background(), domain.SystemResource{
		Kind: domain.ResourceDatabase, Name: "primary-db",
	})

	_, _ = store.SaveLink(context.Background(), domain.ComponentLink{
		RepositoryID:    repoID,
		FromComponentID: api.ID,
		ToResourceID:    &resource.ID,
		Protocol:        domain.LinkSQL,
		Status:          domain.LinkConfirmed,
		Confidence:      domain.ConfidenceExact,
	})
	_, _ = store.SaveLink(context.Background(), domain.ComponentLink{
		RepositoryID:    otherRepoID,
		FromComponentID: worker.ID,
		ToComponentID:   &api.ID,
		Protocol:        domain.LinkHTTP,
		Status:          domain.LinkConfirmed,
		Confidence:      domain.ConfidenceHigh,
	})

	tool := &linksTool{kit: kit}

	resBoth := tool.Execute(repoCtx(repoID), `{}`)
	require.False(t, resBoth.IsError, resBoth.Content)
	var both struct {
		Links []linkView `json:"links"`
	}
	require.NoError(t, json.Unmarshal([]byte(resBoth.Content), &both))
	require.Len(t, both.Links, 2)

	resOut := tool.Execute(repoCtx(repoID), `{"direction":"out"}`)
	require.False(t, resOut.IsError, resOut.Content)
	var out struct {
		Links []linkView `json:"links"`
	}
	require.NoError(t, json.Unmarshal([]byte(resOut.Content), &out))
	require.Len(t, out.Links, 1)
	assert.Equal(t, "demo/apps/api", out.Links[0].From)
	assert.Contains(t, out.Links[0].To, "primary-db")
	assert.Equal(t, "sql", out.Links[0].Protocol)

	resIn := tool.Execute(repoCtx(repoID), `{"direction":"in"}`)
	require.False(t, resIn.IsError, resIn.Content)
	var in struct {
		Links []linkView `json:"links"`
	}
	require.NoError(t, json.Unmarshal([]byte(resIn.Content), &in))
	require.Len(t, in.Links, 1)
	assert.Equal(t, "other/.", in.Links[0].From)
	assert.Equal(t, "demo/apps/api", in.Links[0].To)
}

func TestLinksTool_InvalidDirectionErrors(t *testing.T) {
	kit, _, repos := newTestKit(t)
	repoID := repos.put(domain.Repository{Name: "demo"}).ID
	tool := &linksTool{kit: kit}
	res := tool.Execute(repoCtx(repoID), `{"direction":"sideways"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "invalid direction")
}

func TestRecordNoteTool_AcceptsWithValidEvidence(t *testing.T) {
	kit, _, repos := newTestKit(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644))
	repoID := repos.put(domain.Repository{Name: "demo", RootPath: root}).ID

	tool := &recordNoteTool{kit: kit}
	res := tool.Execute(repoCtx(repoID), `{"notes":[{"topic":"gotchas","body_md":"watch the retry loop","evidence":[{"path":"main.go"}]}]}`)
	require.False(t, res.IsError, res.Content)

	var payload struct {
		Results []appprojectmodel.NoteResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &payload))
	require.Len(t, payload.Results, 1)
	assert.True(t, payload.Results[0].Accepted)
}

func TestRecordNoteTool_RejectsWithoutResolvableEvidence(t *testing.T) {
	kit, _, repos := newTestKit(t)
	root := t.TempDir()
	repoID := repos.put(domain.Repository{Name: "demo", RootPath: root}).ID

	tool := &recordNoteTool{kit: kit}
	res := tool.Execute(repoCtx(repoID), `{"notes":[{"topic":"gotchas","body_md":"watch the retry loop","evidence":[{"path":"missing.go"}]}]}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "no note was recorded")
}

func TestRecordNoteTool_RequiresAtLeastOneNote(t *testing.T) {
	kit, _, repos := newTestKit(t)
	repoID := repos.put(domain.Repository{Name: "demo"}).ID
	tool := &recordNoteTool{kit: kit}
	res := tool.Execute(repoCtx(repoID), `{"notes":[]}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "notes is required")
}

func TestRecordNoteTool_NoRepositoryErrors(t *testing.T) {
	kit, _, _ := newTestKit(t)
	tool := &recordNoteTool{kit: kit}
	res := tool.Execute(context.Background(), `{"notes":[{"topic":"gotchas","body_md":"x","evidence":[{"path":"a.go"}]}]}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "repository")
}

func ptr[T any](v T) *T { return &v }
