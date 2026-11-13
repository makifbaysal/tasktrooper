package board

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type refusingTaskManager struct {
	*fakeTaskManager
	err   error
	calls int
}

func (m *refusingTaskManager) CreateTask(context.Context, uuid.UUID, domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	m.calls++
	return domain.BoardTask{}, m.err
}

func TestCreateTaskTool_RefusesAnEmptyCard(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no description", map[string]any{"title": "Stop local preview", "acceptance_criteria": []string{"Given a run When the card moves Then the preview stops"}}, "`description` is empty"},
		{"blank description", map[string]any{"title": "Stop local preview", "description": "  \n", "task_type": "technical"}, "`description` is empty"},
		{"task without criteria", map[string]any{"title": "Stop local preview", "description": "Why."}, "no acceptance criteria"},
		{"bug without criteria", map[string]any{"title": "Stop local preview", "description": "Why.", "task_type": "bug"}, "no acceptance criteria"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks := &recordingTaskManager{fakeTaskManager: &fakeTaskManager{}}
			tc.args["allow_duplicate"] = true
			res := newCreateTaskTool(&ToolKit{Tasks: tasks}).Execute(context.Background(), mustJSON(t, tc.args))
			if !res.IsError || !strings.Contains(res.Content, tc.want) {
				t.Fatalf("want refusal containing %q, got error=%v %q", tc.want, res.IsError, res.Content)
			}
			if tasks.created.Title != "" {
				t.Fatalf("task must not be created, got %+v", tasks.created)
			}
		})
	}
}

func TestCreateTaskTool_CriteriaOnlyForChecklistTypes(t *testing.T) {
	for _, taskType := range []string{"analiz", "technical", "design"} {
		tasks := &recordingTaskManager{fakeTaskManager: &fakeTaskManager{}}
		res := newCreateTaskTool(&ToolKit{Tasks: tasks}).Execute(context.Background(), mustJSON(t, map[string]any{
			"title": "Look into it", "description": "Why.", "task_type": taskType, "allow_duplicate": true,
		}))
		if res.IsError {
			t.Fatalf("%s: unexpected error: %s", taskType, res.Content)
		}
	}
}

func TestCreateTaskTool_StoresTheDescriptionAndAsksForAnAssignee(t *testing.T) {
	tasks := &recordingTaskManager{fakeTaskManager: &fakeTaskManager{}}
	res := newCreateTaskTool(&ToolKit{Tasks: tasks}).Execute(context.Background(), mustJSON(t, map[string]any{
		"title":               "Stop local preview",
		"description":         "When the card moves, its local preview stops.",
		"acceptance_criteria": []string{"Given a running preview When the card moves Then the preview stops"},
		"allow_duplicate":     true,
	}))
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if tasks.created.Description != "When the card moves, its local preview stops." {
		t.Fatalf("description = %q, want it passed through", tasks.created.Description)
	}
	if !tasks.created.RequireAssignee {
		t.Fatal("an agent's task must require an assignee")
	}
}

func TestCreateTaskTool_NoAssigneeIsExplained(t *testing.T) {
	tasks := &refusingTaskManager{fakeTaskManager: &fakeTaskManager{}, err: domain.ErrAssigneeRequired}
	res := newCreateTaskTool(&ToolKit{Tasks: tasks}).Execute(context.Background(), mustJSON(t, map[string]any{
		"title": "Stop local preview", "description": "Why.", "task_type": "technical", "allow_duplicate": true,
	}))
	if !res.IsError || !strings.Contains(res.Content, "list_team") {
		t.Fatalf("want the assignee guard, got error=%v %q", res.IsError, res.Content)
	}
}
