package board

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type rosterTeam struct{ agents []domain.Agent }

func (r rosterTeam) ListAgents(context.Context) ([]domain.Agent, error) { return r.agents, nil }

type roleFake struct {
	port.RoleResolver
	role     domain.AgentRole
	resolved *uuid.UUID
}

func (r roleFake) RoleByKey(context.Context, string) (domain.AgentRole, error) { return r.role, nil }
func (r roleFake) AgentForRole(context.Context, uuid.UUID, string) (*uuid.UUID, error) {
	return r.resolved, nil
}

func assigneeKit(agents []domain.Agent, roles port.RoleResolver) (*ToolKit, *recordingTaskManager) {
	tasks := &recordingTaskManager{fakeTaskManager: &fakeTaskManager{}}
	return &ToolKit{Tasks: tasks, Team: rosterTeam{agents: agents}, Roles: roles}, tasks
}

func TestCreateTask_AssigneeGuard(t *testing.T) {
	enabled := domain.Agent{ID: uuid.New(), Name: "backend-developer", Enabled: true}
	disabled := domain.Agent{ID: uuid.New(), Name: "data-scientist", Enabled: false}

	t.Run("disabled agent by name is refused and named", func(t *testing.T) {
		kit, tasks := assigneeKit([]domain.Agent{enabled, disabled}, nil)
		res := newCreateTaskTool(kit).Execute(context.Background(), mustJSON(t, map[string]any{
			"title": "Churn model", "assignee": "data-scientist", "allow_duplicate": true,
		}))
		if !res.IsError || !strings.Contains(res.Content, "data-scientist") || !strings.Contains(res.Content, "Edit team") {
			t.Fatalf("want refusal naming data-scientist, got error=%v %q", res.IsError, res.Content)
		}
		if tasks.created.Title != "" {
			t.Fatalf("task must not be created, got %+v", tasks.created)
		}
	})

	t.Run("disabled agent by UUID is refused", func(t *testing.T) {
		kit, _ := assigneeKit([]domain.Agent{enabled, disabled}, nil)
		res := newCreateTaskTool(kit).Execute(context.Background(), mustJSON(t, map[string]any{
			"title": "Churn model", "assignee": disabled.ID.String(), "allow_duplicate": true,
		}))
		if !res.IsError || !strings.Contains(res.Content, "data-scientist") {
			t.Fatalf("want refusal, got %q", res.Content)
		}
	})

	t.Run("enabled agent is assigned", func(t *testing.T) {
		kit, tasks := assigneeKit([]domain.Agent{enabled, disabled}, nil)
		res := newCreateTaskTool(kit).Execute(context.Background(), mustJSON(t, map[string]any{
			"title": "API", "assignee": "backend-developer", "allow_duplicate": true,
		}))
		if res.IsError {
			t.Fatalf("unexpected error: %s", res.Content)
		}
		if tasks.created.AssigneeAgentID == nil || *tasks.created.AssigneeAgentID != enabled.ID {
			t.Fatalf("assignee = %v, want %s", tasks.created.AssigneeAgentID, enabled.ID)
		}
	})

	t.Run("role resolving to a disabled agent is refused listing its holders", func(t *testing.T) {
		second := domain.Agent{ID: uuid.New(), Name: "ml-engineer", Enabled: false}
		role := domain.AgentRole{ID: uuid.New(), Key: "data", Assignments: []domain.RoleAssignment{
			{AgentID: disabled.ID}, {AgentID: second.ID},
		}}
		kit, tasks := assigneeKit([]domain.Agent{enabled, disabled, second}, roleFake{role: role, resolved: &disabled.ID})
		res := newCreateTaskTool(kit).Execute(context.Background(), mustJSON(t, map[string]any{
			"title": "Churn model", "assignee_role": "data", "allow_duplicate": true,
		}))
		if !res.IsError || !strings.Contains(res.Content, "data-scientist") || !strings.Contains(res.Content, "ml-engineer") {
			t.Fatalf("want refusal listing both holders, got %q", res.Content)
		}
		if tasks.created.Title != "" {
			t.Fatal("task must not be created")
		}
	})

	t.Run("role resolving to an enabled agent is assigned", func(t *testing.T) {
		role := domain.AgentRole{ID: uuid.New(), Key: "developer", Assignments: []domain.RoleAssignment{{AgentID: enabled.ID}}}
		kit, tasks := assigneeKit([]domain.Agent{enabled, disabled}, roleFake{role: role, resolved: &enabled.ID})
		res := newCreateTaskTool(kit).Execute(context.Background(), mustJSON(t, map[string]any{
			"title": "API", "assignee_role": "developer", "allow_duplicate": true,
		}))
		if res.IsError {
			t.Fatalf("unexpected error: %s", res.Content)
		}
		if tasks.created.AssigneeAgentID == nil || *tasks.created.AssigneeAgentID != enabled.ID {
			t.Fatalf("assignee = %v", tasks.created.AssigneeAgentID)
		}
	})
}

func TestListTeam_SplitsTeamAndAvailable(t *testing.T) {
	kit := &ToolKit{Team: rosterTeam{agents: []domain.Agent{
		{ID: uuid.New(), Name: "backend-developer", Enabled: true},
		{ID: uuid.New(), Name: "data-scientist", CatalogSlug: "data-scientist", Description: "analysis"},
	}}}
	res := newListTeamTool(kit).Execute(context.Background(), "{}")
	if res.IsError {
		t.Fatalf("error: %s", res.Content)
	}
	var out struct {
		Count int `json:"count"`
		Team  []struct {
			Name string `json:"name"`
		} `json:"team"`
		Available []struct {
			Name        string `json:"name"`
			CatalogSlug string `json:"catalog_slug"`
			Role        string `json:"role"`
		} `json:"available_to_add"`
	}
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || len(out.Team) != 1 || out.Team[0].Name != "backend-developer" {
		t.Fatalf("team = %+v", out)
	}
	if len(out.Available) != 1 || out.Available[0].CatalogSlug != "data-scientist" || out.Available[0].Role != "analysis" {
		t.Fatalf("available = %+v", out.Available)
	}
}
