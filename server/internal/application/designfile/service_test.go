package designfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/designfile"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeTool struct{ renders int }

func (f *fakeTool) Installed() bool { return true }
func (f *fakeTool) Info(context.Context, string) (designfile.Info, error) {
	return designfile.Info{Name: "App", Pages: []designfile.Page{{ID: 1, Name: "00 · Foundations"}}}, nil
}
func (f *fakeTool) RenderPage(_ context.Context, _, _, out string, _ float64) error {
	f.renders++
	return os.WriteFile(out, []byte("png"), 0o644)
}
func (f *fakeTool) Open(context.Context, string) error   { return nil }
func (f *fakeTool) Reveal(context.Context, string) error { return nil }

type fixedBase struct{ id *uuid.UUID }

func (b fixedBase) BaseProject(context.Context, uuid.UUID) (*uuid.UUID, error) { return b.id, nil }

func TestATaskStartsFromTheDesignSystemFileItBuildsOn(t *testing.T) {
	project := uuid.New()
	svc := designfile.NewService(t.TempDir(), &fakeTool{}, fixedBase{&project})
	repo, task := uuid.New(), uuid.New()

	empty, err := svc.PrepareTask(context.Background(), repo, task)
	require.NoError(t, err)
	assert.False(t, empty.TaskExists, "no design system file yet: the designer creates the task's file")

	require.NoError(t, os.MkdirAll(filepath.Dir(svc.ProjectFile(project)), 0o755))
	require.NoError(t, os.WriteFile(svc.ProjectFile(project), []byte("base"), 0o644))
	other := uuid.New()
	seeded, err := svc.PrepareTask(context.Background(), repo, other)
	require.NoError(t, err)
	assert.Equal(t, svc.ProjectFile(project), seeded.SeededFrom)
	got, _ := os.ReadFile(seeded.TaskFile)
	assert.Equal(t, "base", string(got))

	require.NoError(t, os.WriteFile(seeded.TaskFile, []byte("drawn"), 0o644))
	again, err := svc.PrepareTask(context.Background(), repo, other)
	require.NoError(t, err)
	assert.Empty(t, again.SeededFrom, "a task's own file is never overwritten")
	got, _ = os.ReadFile(again.TaskFile)
	assert.Equal(t, "drawn", string(got))
}

func TestApprovingADesignSystemTaskPromotesItsFile(t *testing.T) {
	project := uuid.New()
	svc := designfile.NewService(t.TempDir(), &fakeTool{}, fixedBase{&project})
	task := uuid.New()
	require.NoError(t, os.MkdirAll(filepath.Dir(svc.TaskFile(task)), 0o755))
	require.NoError(t, os.WriteFile(svc.TaskFile(task), []byte("v2"), 0o644))

	svc.Promote(task, []domain.DesignSystem{{Scope: domain.DesignSystemScopeProject, ProjectID: &project}})

	got, err := os.ReadFile(svc.ProjectFile(project))
	require.NoError(t, err)
	assert.Equal(t, "v2", string(got))
}

func TestAPagePreviewIsRenderedOncePerVersionOfTheFile(t *testing.T) {
	tool := &fakeTool{}
	svc := designfile.NewService(t.TempDir(), tool, nil)
	task := uuid.New()
	require.NoError(t, os.MkdirAll(filepath.Dir(svc.TaskFile(task)), 0o755))
	require.NoError(t, os.WriteFile(svc.TaskFile(task), []byte("x"), 0o644))

	first, err := svc.Preview(context.Background(), svc.TaskFile(task), 1, 1)
	require.NoError(t, err)
	second, err := svc.Preview(context.Background(), svc.TaskFile(task), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, tool.renders)

	_, err = svc.Preview(context.Background(), svc.TaskFile(task), 99, 1)
	assert.ErrorIs(t, err, designfile.ErrUnknownPage)
}

func TestAFileOutsideTheDesignFilesFolderIsNeverTouched(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.figcraft")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))
	root := t.TempDir()
	svc := designfile.NewService(root, &fakeTool{}, nil)

	for _, file := range []string{outside, filepath.Join(root, "..", filepath.Base(outside)), root} {
		m := svc.Manifest(context.Background(), file)
		assert.False(t, m.Exists, file)
		_, err := svc.Preview(context.Background(), file, 1, 1)
		assert.ErrorIs(t, err, designfile.ErrNoFile, file)
		assert.ErrorIs(t, svc.Open(context.Background(), file), designfile.ErrNoFile, file)
		assert.ErrorIs(t, svc.Reveal(context.Background(), file), designfile.ErrNoFile, file)
	}
}
