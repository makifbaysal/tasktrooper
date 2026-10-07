package board

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// documentTaskManager serves one task and records the documents attached to it.
type documentTaskManager struct {
	*fakeTaskManager
	task  domain.BoardTask
	added []domain.CreateTaskDocumentRequest
}

func (d *documentTaskManager) ListTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	return []domain.BoardTask{d.task}, nil
}

func (d *documentTaskManager) GetTask(_ context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	if repositoryID != d.task.RepositoryID || taskID != d.task.ID {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return d.task, nil
}

func (d *documentTaskManager) AddDocument(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskDocumentRequest) (domain.TaskDocument, error) {
	d.added = append(d.added, req)
	return domain.TaskDocument{ID: uuid.New(), TaskID: taskID, Title: req.Title}, nil
}

func newDocumentKit(taskType domain.TaskType) (*ToolKit, *documentTaskManager, uuid.UUID) {
	repoID := uuid.New()
	taskID := uuid.New()
	tasks := &documentTaskManager{
		fakeTaskManager: &fakeTaskManager{taskRepoID: repoID},
		task:            domain.BoardTask{ID: taskID, RepositoryID: repoID, TaskType: taskType},
	}
	return &ToolKit{Tasks: tasks, Workflows: workflowtest.Default().Reader()}, tasks, taskID
}

func documentArgs(taskID uuid.UUID) string {
	return `{"task_id":"` + taskID.String() + `","title":"spec: android link","content":"# Spec"}`
}

func runContext(tools ...string) context.Context {
	ctx, usage := registry.ContextWithToolUsage(context.Background())
	for _, name := range tools {
		usage.Record(name)
	}
	return registry.ContextWithAgentID(ctx, uuid.New())
}

func TestAddDocumentRejectsAnalysisWithoutCodeExploration(t *testing.T) {
	kit, tasks, taskID := newDocumentKit("analiz")
	tool := newAddDocumentTool(kit)

	// The DE-1 ledger: a skill was loaded, the shell was used, the repository
	// was never read.
	res := tool.Execute(runContext("load_skill", "run_terminal"), documentArgs(taskID))

	if !res.IsError {
		t.Fatalf("expected the ungrounded analysis to be rejected, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "codebase_search") {
		t.Fatalf("rejection must tell the agent which tools prove grounding, got %q", res.Content)
	}
	if len(tasks.added) != 0 {
		t.Fatalf("no document may be attached, got %d", len(tasks.added))
	}
}

func TestAddDocumentAcceptsAnalysisAfterCodeExploration(t *testing.T) {
	kit, tasks, taskID := newDocumentKit("analiz")
	tool := newAddDocumentTool(kit)

	res := tool.Execute(runContext("get_repo_tree", "grep_code"), documentArgs(taskID))

	if res.IsError {
		t.Fatalf("grounded analysis must be accepted, got %q", res.Content)
	}
	if len(tasks.added) != 1 {
		t.Fatalf("expected the document to be attached, got %d", len(tasks.added))
	}
}

func TestAddDocumentDoesNotGateNonAnalysisTasks(t *testing.T) {
	kit, tasks, taskID := newDocumentKit("task")
	tool := newAddDocumentTool(kit)

	res := tool.Execute(runContext("run_terminal"), documentArgs(taskID))

	if res.IsError {
		t.Fatalf("an implementation task's document must not be gated, got %q", res.Content)
	}
	if len(tasks.added) != 1 {
		t.Fatalf("expected the document to be attached, got %d", len(tasks.added))
	}
}

func TestAddDocumentAllowsUnmeasuredRuns(t *testing.T) {
	kit, tasks, taskID := newDocumentKit("analiz")
	tool := newAddDocumentTool(kit)

	// No tracker in context: a chat session, not a board run. Missing evidence
	// is not evidence of a fabricated analysis.
	ctx := registry.ContextWithAgentID(context.Background(), uuid.New())
	res := tool.Execute(ctx, documentArgs(taskID))

	if res.IsError {
		t.Fatalf("unmeasured run must not be blocked, got %q", res.Content)
	}
	if len(tasks.added) != 1 {
		t.Fatalf("expected the document to be attached, got %d", len(tasks.added))
	}
}
