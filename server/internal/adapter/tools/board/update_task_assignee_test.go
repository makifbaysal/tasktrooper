package board

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type recordingUpdateManager struct {
	*fakeTaskManager
	updated domain.UpdateBoardTaskRequest
}

func (m *recordingUpdateManager) UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	m.updated = req
	return domain.BoardTask{ID: taskID, RepositoryID: repositoryID}, nil
}

func runUpdate(t *testing.T, team rosterTeam, args map[string]any) (*recordingUpdateManager, domain.ToolResult) {
	t.Helper()
	repoID, taskID := uuid.New(), uuid.New()
	mgr := &recordingUpdateManager{fakeTaskManager: &fakeTaskManager{taskRepoID: repoID}}
	kit := &ToolKit{Tasks: mgr, Team: team}
	args["task_id"] = taskID.String()
	raw, _ := json.Marshal(args)
	return mgr, newUpdateTaskTool(kit).Execute(context.Background(), string(raw))
}

func TestUpdateTaskToolSetsTheAssigneeByName(t *testing.T) {
	dev := domain.Agent{ID: uuid.New(), Name: "backend-developer", Enabled: true}
	mgr, res := runUpdate(t, rosterTeam{agents: []domain.Agent{dev}}, map[string]any{"assignee": "Backend-Developer"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	got := mgr.updated.AssigneeAgentID
	if !got.Present || got.Value == nil || *got.Value != dev.ID {
		t.Fatalf("assignee not set to %s: %+v", dev.ID, got)
	}
}

func TestUpdateTaskToolNoneUnassigns(t *testing.T) {
	mgr, res := runUpdate(t, rosterTeam{}, map[string]any{"assignee": "none"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if got := mgr.updated.AssigneeAgentID; !got.Present || got.Value != nil {
		t.Fatalf("want explicit unassign, got %+v", got)
	}
}

func TestUpdateTaskToolLeavesTheAssigneeAloneWhenOmitted(t *testing.T) {
	mgr, res := runUpdate(t, rosterTeam{}, map[string]any{"priority": "high"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if mgr.updated.AssigneeAgentID.Present {
		t.Fatalf("assignee must stay untouched: %+v", mgr.updated.AssigneeAgentID)
	}
}

func TestUpdateTaskToolRefusesAnUnknownAssignee(t *testing.T) {
	_, res := runUpdate(t, rosterTeam{agents: []domain.Agent{{ID: uuid.New(), Name: "qa", Enabled: true}}}, map[string]any{"assignee": "nobody"})
	if !res.IsError {
		t.Fatal("want an error naming the valid agents")
	}
}
