package designsystem

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appdesignsystem "github.com/makifbaysal/tasktrooper/server/internal/application/designsystem"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeService struct {
	effective map[uuid.UUID]domain.EffectiveDesignSystem
	project   appdesignsystem.ProjectView
	proposed  []domain.DesignSystemProposal
	requested []string
	files     []domain.DesignSystemFile
}

func (f *fakeService) RequestForProject(_ context.Context, id uuid.UUID, notes string) (appdesignsystem.RequestResult, error) {
	f.requested = append(f.requested, "project:"+id.String()+":"+notes)
	return appdesignsystem.RequestResult{Task: domain.BoardTask{ID: uuid.New(), Key: "D-9", Title: "Design system: Shop", Column: domain.TaskColumnTodo}, Created: true}, nil
}

func (f *fakeService) RequestForRepository(_ context.Context, id uuid.UUID, notes string) (appdesignsystem.RequestResult, error) {
	f.requested = append(f.requested, "repository:"+id.String()+":"+notes)
	return appdesignsystem.RequestResult{Task: domain.BoardTask{ID: uuid.New(), Key: "D-8", Column: domain.TaskColumnInProgress}}, nil
}

func (f *fakeService) Files(context.Context, uuid.UUID) ([]domain.DesignSystemFile, error) {
	return f.files, nil
}

func (f *fakeService) Effective(_ context.Context, id uuid.UUID) (domain.EffectiveDesignSystem, error) {
	return f.effective[id], nil
}

func (f *fakeService) ProjectView(context.Context, uuid.UUID) (appdesignsystem.ProjectView, error) {
	return f.project, nil
}

func (f *fakeService) Propose(_ context.Context, p domain.DesignSystemProposal) (domain.DesignSystem, error) {
	f.proposed = append(f.proposed, p)
	return domain.DesignSystem{ID: uuid.New(), Scope: p.Scope, Version: 3, Status: domain.DesignSystemInReview}, nil
}

type fakeTasks struct{ key string }

func (f fakeTasks) GetTask(_ context.Context, _, id uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{ID: id, Key: f.key}, nil
}

func newTestKit(t *testing.T) (*ToolKit, *fakeService, fakeTasks) {
	t.Helper()
	svc := &fakeService{effective: map[uuid.UUID]domain.EffectiveDesignSystem{}}
	tasks := fakeTasks{key: "D-4"}
	return &ToolKit{Service: svc, Tasks: tasks}, svc, tasks
}

func tool(t *testing.T, kit *ToolKit, name string) interface {
	Execute(context.Context, string) domain.ToolResult
} {
	t.Helper()
	for _, ex := range NewExecutors(kit) {
		if ex.Name() == name {
			return ex
		}
	}
	t.Fatalf("no tool %s", name)
	return nil
}

func TestGetDesignSystemDefaultsToTheRunsRepository(t *testing.T) {
	kit, svc, _ := newTestKit(t)
	repoID, projectID := uuid.New(), uuid.New()
	svc.effective[repoID] = domain.EffectiveDesignSystem{
		RepositoryID: repoID,
		Project:      &domain.InitiativeProject{ID: projectID, Name: "TaskTrooper"},
		Base:         &domain.DesignSystem{Version: 2, Status: domain.DesignSystemApproved, DesignMD: "## Overview", Tokens: json.RawMessage(`{"a":{"$value":1}}`)},
		Tokens:       json.RawMessage(`{"a":{"$value":1}}`),
	}
	ctx := registry.ContextWithRepositoryID(context.Background(), repoID)

	res := tool(t, kit, getToolName).Execute(ctx, `{}`)
	require.False(t, res.IsError, res.Content)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	assert.Equal(t, "TaskTrooper", out["project"].(map[string]any)["name"])
	assert.Equal(t, float64(2), out["base"].(map[string]any)["version"])
	assert.Nil(t, out["layer"])
	assert.Equal(t, map[string]any{"a": map[string]any{"$value": float64(1)}}, out["tokens"])
}

func TestGetDesignSystemNeedsARepository(t *testing.T) {
	kit, _, _ := newTestKit(t)
	res := tool(t, kit, getToolName).Execute(context.Background(), `{}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "get_design_system needs a repository")
}

func TestGetDesignSystemForAProject(t *testing.T) {
	kit, svc, _ := newTestKit(t)
	repoID := uuid.New()
	svc.project = appdesignsystem.ProjectView{
		Project: domain.InitiativeProject{ID: uuid.New(), Name: "TaskTrooper"},
		Current: &domain.DesignSystem{Version: 1, Status: domain.DesignSystemApproved},
		Repositories: []appdesignsystem.RepositoryLayerSummary{
			{ID: repoID, Name: "site", Kind: "frontend", Layer: &domain.DesignSystem{Version: 1, Rationale: "marketing"}},
		},
	}
	res := tool(t, kit, getToolName).Execute(context.Background(), `{"project_id":"`+uuid.NewString()+`"}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, `"layer_rationale":"marketing"`)
	assert.Contains(t, res.Content, `"pending":[]`)
}

func TestProposeOnlyFromADesignTask(t *testing.T) {
	kit, svc, _ := newTestKit(t)
	ctx := registry.ContextWithTaskID(context.Background(), uuid.New())
	ctx = registry.ContextWithTaskType(ctx, string(domain.TaskTypeTask))

	res := tool(t, kit, proposeToolName).Execute(ctx, `{"scope":"project"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "Only a design task (task_type=design) proposes a design system")
	assert.Empty(t, svc.proposed)
}

func TestProposePassesTheProposalThrough(t *testing.T) {
	kit, svc, _ := newTestKit(t)
	taskID, repoID, projectID := uuid.New(), uuid.New(), uuid.New()
	ctx := registry.ContextWithTaskID(registry.ContextWithRepositoryID(context.Background(), repoID), taskID)
	ctx = registry.ContextWithTaskType(ctx, string(domain.TaskTypeDesign))

	res := tool(t, kit, proposeToolName).Execute(ctx, `{
		"scope": "project",
		"project_id": "`+projectID.String()+`",
		"design_md": "## Overview",
		"tokens": {"color": {"primary": {"$value": "#15214b"}}},
		"inventory_md": "- Button"
	}`)
	require.False(t, res.IsError, res.Content)
	assert.JSONEq(t, `{"id":"`+gjsonID(t, res.Content)+`","scope":"project","version":3,"status":"in_review","lint":[]}`, res.Content)

	require.Len(t, svc.proposed, 1)
	p := svc.proposed[0]
	assert.Equal(t, domain.DesignSystemScopeProject, p.Scope)
	assert.Equal(t, projectID, *p.ProjectID)
	assert.Equal(t, taskID, p.TaskID)
	assert.Equal(t, "D-4", p.TaskKey)
	assert.JSONEq(t, `{"color":{"primary":{"$value":"#15214b"}}}`, string(p.Tokens))
}

func gjsonID(t *testing.T, content string) string {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(content), &out))
	return out["id"].(string)
}

func TestRequestDesignSystemOpensTheTaskTheTabWouldOpen(t *testing.T) {
	kit, svc, _ := newTestKit(t)
	projectID, repoID := uuid.New(), uuid.New()

	res := tool(t, kit, requestToolName).Execute(context.Background(), `{"scope":"project","project_id":"`+projectID.String()+`","notes":"keep the navy"}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, `"key":"D-9"`)
	assert.Contains(t, res.Content, `"created":true`)

	ctx := registry.ContextWithRepositoryID(context.Background(), repoID)
	res = tool(t, kit, requestToolName).Execute(ctx, `{"scope":"repository"}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, `"created":false`, "an open request comes back instead of a second one")
	assert.Equal(t, []string{"project:" + projectID.String() + ":keep the navy", "repository:" + repoID.String() + ":"}, svc.requested)

	res = tool(t, kit, requestToolName).Execute(context.Background(), `{"scope":"project"}`)
	assert.True(t, res.IsError)
}

func TestGetDesignSystemReturnsTheFilesWhenAsked(t *testing.T) {
	kit, svc, _ := newTestKit(t)
	repoID := uuid.New()
	svc.effective[repoID] = domain.EffectiveDesignSystem{RepositoryID: repoID, Tokens: json.RawMessage(`{}`)}
	svc.files = []domain.DesignSystemFile{{Path: "DESIGN.md", Content: "## Overview"}}
	ctx := registry.ContextWithRepositoryID(context.Background(), repoID)

	res := tool(t, kit, getToolName).Execute(ctx, `{}`)
	require.False(t, res.IsError)
	assert.NotContains(t, res.Content, `"files"`)

	res = tool(t, kit, getToolName).Execute(ctx, `{"files":true}`)
	require.False(t, res.IsError)
	assert.Contains(t, res.Content, `"files":[{"path":"DESIGN.md","content":"## Overview"}]`)
}
