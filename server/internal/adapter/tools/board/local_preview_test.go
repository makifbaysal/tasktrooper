package board

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeLocalPreviews is a scripted LocalPreviewRunner: Status returns the
// sequence of snapshots queued in statuses (repeating the last one once
// exhausted), and Start records that it was called and returns startResult.
type fakeLocalPreviews struct {
	statuses    []domain.LocalPreview
	statusOK    bool
	startResult domain.LocalPreview
	startErr    error

	startCalls int
	statusCall int
}

func (f *fakeLocalPreviews) Start(_ context.Context, repositoryID, taskID uuid.UUID, _ string) (domain.LocalPreview, error) {
	f.startCalls++
	if f.startErr != nil {
		return domain.LocalPreview{}, f.startErr
	}
	out := f.startResult
	out.RepositoryID = repositoryID
	out.TaskID = taskID
	return out, nil
}

func (f *fakeLocalPreviews) Status(_, taskID uuid.UUID) (domain.LocalPreview, bool) {
	if len(f.statuses) == 0 {
		return domain.LocalPreview{}, f.statusOK
	}
	idx := f.statusCall
	if idx >= len(f.statuses) {
		idx = len(f.statuses) - 1
	}
	f.statusCall++
	if got := f.statuses[idx]; got.TaskID == taskID {
		return got, true
	}
	return domain.LocalPreview{}, false
}

func runStartPreviewTool(t *testing.T, fake *fakeLocalPreviews, taskID uuid.UUID) (localPreviewResult, string) {
	t.Helper()
	repoID := uuid.New()
	if taskID == uuid.Nil {
		taskID = uuid.New()
	}
	tool := newStartTaskPreviewTool(&ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, LocalPreviews: fake})

	ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
	res := tool.Execute(ctx, `{}`)
	require.False(t, res.IsError, res.Content)

	var out localPreviewResult
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	return out, res.Content
}

func withFastPoll(t *testing.T) {
	t.Helper()
	prevInterval, prevTimeout := startTaskPreviewPollInterval, startTaskPreviewPollTimeout
	startTaskPreviewPollInterval = time.Millisecond
	startTaskPreviewPollTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		startTaskPreviewPollInterval = prevInterval
		startTaskPreviewPollTimeout = prevTimeout
	})
}

func TestStartTaskPreviewReusesAnAlreadyRunningPreviewForTheSameTask(t *testing.T) {
	withFastPoll(t)
	taskID := uuid.New()
	fake := &fakeLocalPreviews{
		statuses: []domain.LocalPreview{
			{TaskID: taskID, Status: domain.LocalPreviewRunning, URL: "http://localhost:3000", Branch: "task/T-1"},
		},
	}

	out, _ := runStartPreviewTool(t, fake, taskID)

	assert.Equal(t, 0, fake.startCalls, "an already-running preview for this task must not be restarted")
	assert.Equal(t, "running", out.Status)
	assert.Equal(t, "http://localhost:3000", out.URL)
	assert.Contains(t, out.Note, "browser_navigate")
}

func TestStartTaskPreviewStartsWhenNoneIsActive(t *testing.T) {
	withFastPoll(t)
	fake := &fakeLocalPreviews{
		statusOK:    false,
		startResult: domain.LocalPreview{Status: domain.LocalPreviewStarting, Branch: "task/T-2", Command: "npm run dev"},
	}

	out, _ := runStartPreviewTool(t, fake, uuid.Nil)

	assert.Equal(t, 1, fake.startCalls)
	assert.Equal(t, "starting", out.Status)
	assert.Empty(t, out.URL)
	assert.Contains(t, out.Note, "call start_task_preview again")
}

func TestStartTaskPreviewStartsWhenOnlyAnotherTaskHasAPreview(t *testing.T) {
	withFastPoll(t)
	otherTask := uuid.New()
	fake := &fakeLocalPreviews{
		statuses:    []domain.LocalPreview{{TaskID: otherTask, Status: domain.LocalPreviewRunning, URL: "http://localhost:4000"}},
		startResult: domain.LocalPreview{Status: domain.LocalPreviewStarting},
	}

	_, _ = runStartPreviewTool(t, fake, uuid.New())

	assert.Equal(t, 1, fake.startCalls, "another task's preview is never reused for this one")
}

func TestStartTaskPreviewWaitsForTheURLToAppear(t *testing.T) {
	withFastPoll(t)
	taskID := uuid.New()
	fake := &fakeLocalPreviews{
		startResult: domain.LocalPreview{Status: domain.LocalPreviewStarting},
		statuses: []domain.LocalPreview{
			{TaskID: taskID, Status: domain.LocalPreviewStarting},
			{TaskID: taskID, Status: domain.LocalPreviewStarting},
			{TaskID: taskID, Status: domain.LocalPreviewRunning, URL: "http://localhost:5173"},
		},
	}

	out, _ := runStartPreviewTool(t, fake, taskID)

	assert.Equal(t, "running", out.Status)
	assert.Equal(t, "http://localhost:5173", out.URL)
}

func TestStartTaskPreviewSurfacesFailureDetailAndLogTail(t *testing.T) {
	withFastPoll(t)
	taskID := uuid.New()
	longTail := make([]string, 30)
	for i := range longTail {
		longTail[i] = "line"
	}
	fake := &fakeLocalPreviews{
		startResult: domain.LocalPreview{Status: domain.LocalPreviewStarting},
		statuses: []domain.LocalPreview{
			{TaskID: taskID, Status: domain.LocalPreviewFailed, Detail: "the command exited on its own", LogTail: longTail},
		},
	}

	out, _ := runStartPreviewTool(t, fake, taskID)

	assert.Equal(t, "failed", out.Status)
	assert.Equal(t, "the command exited on its own", out.Detail)
	assert.Len(t, out.LogTail, startTaskPreviewLogTailLines)
	assert.Contains(t, out.Note, "do not approve without executing")
}

func TestStartTaskPreviewNotConfigured(t *testing.T) {
	tool := newStartTaskPreviewTool(&ToolKit{Tasks: &fakeTaskManager{}})
	res := tool.Execute(context.Background(), `{"task_id":"`+uuid.New().String()+`"}`)
	assert.True(t, res.IsError)
}
