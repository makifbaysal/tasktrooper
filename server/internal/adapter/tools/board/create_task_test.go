package board

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// recordingTaskManager reuses fakeTaskManager's inert stubs and records the
// CreateBoardTaskRequest CreateTask was actually called with, which is the
// only thing these tests need to inspect: whether discovered_from landed in
// req.Relations.
type recordingTaskManager struct {
	*fakeTaskManager
	created domain.CreateBoardTaskRequest
}

func (m *recordingTaskManager) CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	m.created = req
	return domain.BoardTask{ID: uuid.New(), Title: req.Title}, nil
}

func relationsOfType(relations []domain.TaskRelationInput, relType domain.TaskRelationType) []domain.TaskRelationInput {
	var out []domain.TaskRelationInput
	for _, rel := range relations {
		if rel.RelationType == relType {
			out = append(out, rel)
		}
	}
	return out
}

func TestCreateTaskTool_DiscoveredFrom(t *testing.T) {
	origin := uuid.New()
	other := uuid.New()

	cases := []struct {
		name           string
		ctxTaskID      uuid.UUID
		derivedFrom    []string
		wantDiscovered []uuid.UUID
		wantDerived    []uuid.UUID
	}{
		{
			name:           "task run without derived_from gets discovered_from targeting the run's task",
			ctxTaskID:      origin,
			wantDiscovered: []uuid.UUID{origin},
		},
		{
			name:           "no task in context: no relation written",
			ctxTaskID:      uuid.Nil,
			wantDiscovered: nil,
		},
		{
			name:           "derived_from names the same origin: only derived_from is written",
			ctxTaskID:      origin,
			derivedFrom:    []string{origin.String()},
			wantDiscovered: nil,
			wantDerived:    []uuid.UUID{origin},
		},
		{
			name:           "derived_from names a different task: both relations are written",
			ctxTaskID:      origin,
			derivedFrom:    []string{other.String()},
			wantDiscovered: []uuid.UUID{origin},
			wantDerived:    []uuid.UUID{other},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks := &recordingTaskManager{fakeTaskManager: &fakeTaskManager{}}
			kit := &ToolKit{Tasks: tasks}
			tool := newCreateTaskTool(kit)

			ctx := context.Background()
			if tc.ctxTaskID != uuid.Nil {
				ctx = registry.ContextWithTaskID(ctx, tc.ctxTaskID)
			}

			args := map[string]any{
				"title":           "Investigate flaky test",
				"allow_duplicate": true,
				"description":     "Why it matters.",
				"task_type":       "technical",
			}
			if len(tc.derivedFrom) > 0 {
				args["derived_from"] = tc.derivedFrom
			}
			result := tool.Execute(ctx, mustJSON(t, args))
			if result.IsError {
				t.Fatalf("Execute returned error: %s", result.Content)
			}

			discovered := relationsOfType(tasks.created.Relations, domain.TaskRelationDiscoveredFrom)
			if len(discovered) != len(tc.wantDiscovered) {
				t.Fatalf("discovered_from relations = %+v, want targets %v", discovered, tc.wantDiscovered)
			}
			for i, rel := range discovered {
				if rel.TargetTaskID != tc.wantDiscovered[i] {
					t.Fatalf("discovered_from[%d].TargetTaskID = %s, want %s", i, rel.TargetTaskID, tc.wantDiscovered[i])
				}
			}

			derived := relationsOfType(tasks.created.Relations, domain.TaskRelationDerivedFrom)
			if len(derived) != len(tc.wantDerived) {
				t.Fatalf("derived_from relations = %+v, want targets %v", derived, tc.wantDerived)
			}
			for i, rel := range derived {
				if rel.TargetTaskID != tc.wantDerived[i] {
					t.Fatalf("derived_from[%d].TargetTaskID = %s, want %s", i, rel.TargetTaskID, tc.wantDerived[i])
				}
			}
		})
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return string(raw)
}
