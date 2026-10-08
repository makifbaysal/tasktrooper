package toolattach

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeAttacher struct {
	repo, task uuid.UUID
	name, mime string
	data       []byte
	err        error
}

func (f *fakeAttacher) AttachToTask(_ context.Context, repositoryID, taskID uuid.UUID, filename, contentType string, data []byte) (domain.AttachmentMeta, error) {
	f.repo, f.task, f.name, f.mime, f.data = repositoryID, taskID, filename, contentType, data
	if f.err != nil {
		return domain.AttachmentMeta{}, f.err
	}
	return domain.AttachmentMeta{ID: uuid.New(), Filename: filename}, nil
}

func png() domain.ToolResultImage {
	return domain.ToolResultImage{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))}
}

func TestNoteSavesTheShotOnTheRunsTask(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	ctx := registry.ContextWithTaskID(registry.ContextWithRepositoryID(context.Background(), repoID), taskID)
	a := &fakeAttacher{}

	note := Note(ctx, a, "browser_screenshot", "export dialog / A @375", png())

	assert.Contains(t, note, "Saved on the task as `export-dialog-A-375.png`")
	assert.Equal(t, repoID, a.repo)
	assert.Equal(t, taskID, a.task)
	assert.Equal(t, "image/png", a.mime)
	assert.Equal(t, []byte("\x89PNG fake"), a.data)
}

func TestNoteWithoutATaskOrAttacherSavesNothing(t *testing.T) {
	a := &fakeAttacher{}
	note := Note(context.Background(), a, "browser_screenshot", "x", png())
	assert.Contains(t, note, "attach_to_task was ignored")
	assert.Empty(t, a.name)

	ctx := registry.ContextWithTaskID(registry.ContextWithRepositoryID(context.Background(), uuid.New()), uuid.New())
	assert.Contains(t, Note(ctx, nil, "browser_screenshot", "x", png()), "attach_to_task was ignored")
}

func TestNoteReportsAFailedSaveWithoutFailing(t *testing.T) {
	ctx := registry.ContextWithTaskID(registry.ContextWithRepositoryID(context.Background(), uuid.New()), uuid.New())
	note := Note(ctx, &fakeAttacher{err: errors.New("attachment too large")}, "mobile_screenshot", "", png())
	require.Contains(t, note, "could not be saved on the task (attachment too large)")
}

func TestFilenameFallsBackToTheToolName(t *testing.T) {
	assert.Equal(t, "mobile_screenshot.png", filename("mobile_screenshot", "  ", "image/png"))
	assert.Equal(t, "home.jpg", filename("browser_screenshot", "home", "image/jpeg"))
}
