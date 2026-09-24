package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeReleaseManager reuses fakeTaskManager's inert stubs and scripts only the
// one call trigger_release makes, so this file stays about the tool's handling
// of the release verdict rather than about the TaskManager surface.
type fakeReleaseManager struct {
	*fakeTaskManager
	pipeline domain.TaskPipeline
	err      error
	calls    int
}

func (f *fakeReleaseManager) TriggerRelease(context.Context, uuid.UUID, uuid.UUID) (domain.TaskPipeline, error) {
	f.calls++
	return f.pipeline, f.err
}

func newReleaseTool(m *fakeReleaseManager) *triggerReleaseTool {
	return &triggerReleaseTool{kit: &ToolKit{Tasks: m}}
}

// A release-identity block must reach the agent as an error with the reason
// intact: a model that reads it as a routine "not triggered" reports the task
// as released, which is the exact failure the gate exists to prevent.
func TestTriggerReleaseToolSurfacesIdentityBlocks(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantSubs []string
	}{
		{
			name: "moved target",
			err: fmt.Errorf("%w: verified at 111111111111, but the branch is now at 222222222222",
				domain.ErrReleaseTargetMoved),
			wantSubs: []string{"111111111111", "222222222222", "Do not retry"},
		},
		{
			name: "unverified target",
			err: fmt.Errorf("%w: no verified commit is stamped on this task",
				domain.ErrReleaseTargetUnverified),
			wantSubs: []string{"no verified commit", "Do not retry"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeReleaseManager{fakeTaskManager: &fakeTaskManager{}, err: tc.err}
			result := newReleaseTool(fake).Execute(context.Background(), `{"task_id":"`+uuid.New().String()+`"}`)

			if !result.IsError {
				t.Fatalf("a blocked release must be a tool error, got: %s", result.Content)
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(result.Content, want) {
					t.Fatalf("want %q in the tool result, got: %s", want, result.Content)
				}
			}
			if strings.Contains(result.Content, "triggered") {
				t.Fatalf("a blocked release must not claim anything was triggered: %s", result.Content)
			}
		})
	}
}

// The paths the guard must not disturb: a clean release still reports the
// dispatched pipeline, and the batched-release opt-out stays a quiet no-op
// rather than an error.
func TestTriggerReleaseToolUnblockedPaths(t *testing.T) {
	t.Run("dispatches", func(t *testing.T) {
		fake := &fakeReleaseManager{
			fakeTaskManager: &fakeTaskManager{},
			pipeline:        domain.TaskPipeline{ID: uuid.New(), Status: domain.PipelineStatusPending},
		}
		result := newReleaseTool(fake).Execute(context.Background(), `{"task_id":"`+uuid.New().String()+`"}`)
		if result.IsError {
			t.Fatalf("unexpected tool error: %s", result.Content)
		}
		var payload struct {
			Triggered  bool   `json:"triggered"`
			PipelineID string `json:"pipeline_id"`
		}
		if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if !payload.Triggered || payload.PipelineID != fake.pipeline.ID.String() {
			t.Fatalf("want the dispatched pipeline reported, got %+v", payload)
		}
	})

}

// Any other failure keeps its previous shape: a tool error carrying the
// service's message, with no release-identity advice bolted onto it.
func TestTriggerReleaseToolOtherErrorsUnchanged(t *testing.T) {
	fake := &fakeReleaseManager{fakeTaskManager: &fakeTaskManager{}, err: errors.New("pipeline runner unavailable")}
	result := newReleaseTool(fake).Execute(context.Background(), `{"task_id":"`+uuid.New().String()+`"}`)
	if !result.IsError || !strings.Contains(result.Content, "pipeline runner unavailable") {
		t.Fatalf("want the underlying error surfaced, got: %s", result.Content)
	}
	if strings.Contains(result.Content, "Do not retry") {
		t.Fatalf("release-identity advice leaked onto an unrelated failure: %s", result.Content)
	}
}
