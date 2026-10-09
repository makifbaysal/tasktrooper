package board

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// taskChanges is a run's change set against its base branch, read wherever
// the checkout is: this filesystem, or the computer a run executed on.
type taskChanges interface {
	TaskDiff(ctx context.Context) (string, error)
	TaskChangedFiles(ctx context.Context) ([]string, error)
}

type workspaceChanges struct {
	git       port.GitClient
	workspace string
}

func (w workspaceChanges) TaskDiff(ctx context.Context) (string, error) {
	if w.git == nil || w.workspace == "" {
		return "", nil
	}
	return w.git.TaskDiff(ctx, w.workspace)
}

func (w workspaceChanges) TaskChangedFiles(ctx context.Context) ([]string, error) {
	if w.git == nil || w.workspace == "" {
		return nil, nil
	}
	return w.git.TaskChangedFiles(ctx, w.workspace)
}
