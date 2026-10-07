package board

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
)

const WorkspaceReaperInterval = time.Hour

// Not a safety margin: the window in which a human drags back a premature release; a resumed task re-clones anyway.
const WorkspaceReapGrace = 48 * time.Hour

type TaskLister interface {
	ListAll(ctx context.Context) ([]domain.BoardTask, error)
}

type ActiveTaskProbe interface {
	HasLiveRunForTask(ctx context.Context, taskID uuid.UUID, liveWithin time.Duration) (bool, error)
}

// BranchIndexStore is what the reaper prunes besides directories: a task's
// branch index starts as a full copy of the repository's chunks and embeddings,
// and nothing else ever deletes one.
type BranchIndexStore interface {
	ListBranchIndexes(ctx context.Context) ([]domain.WorkspaceIndex, error)
	DeleteIndex(ctx context.Context, indexID uuid.UUID) error
}

// Driven by the directory listing: the dirs that matter most have no task left; an unreadable task list reaps nothing.
type WorkspaceReaper struct {
	tasks   TaskLister
	active  ActiveTaskProbe
	root    string
	grace   time.Duration
	indexes BranchIndexStore
}

// SetBranchIndexes turns pruning on. Only for a database no other host
// serves: pruning takes a workspace missing from this host's disk to mean its
// index is orphaned.
func (r *WorkspaceReaper) SetBranchIndexes(s BranchIndexStore) {
	if r != nil {
		r.indexes = s
	}
}

func NewWorkspaceReaper(tasks TaskLister, active ActiveTaskProbe, root string, grace time.Duration) *WorkspaceReaper {
	if grace <= 0 {
		grace = WorkspaceReapGrace
	}
	return &WorkspaceReaper{tasks: tasks, active: active, root: strings.TrimSpace(root), grace: grace}
}

func (r *WorkspaceReaper) Start(ctx context.Context, interval time.Duration) {
	if r == nil || r.tasks == nil || r.root == "" {
		return
	}
	if interval <= 0 {
		interval = WorkspaceReaperInterval
	}
	go func() {
		r.Sweep(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.Sweep(ctx)
			}
		}
	}()
}

func (r *WorkspaceReaper) Sweep(ctx context.Context) {
	root, err := workspace.ResolveRoot(r.root)
	if err != nil {
		log.Warn().Err(err).Msg("workspace reaper: could not resolve the workspace root, so nothing was reaped")
		return
	}
	all, err := r.tasks.ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("workspace reaper: listing tasks failed, skipping this pass")
		return
	}
	byID := make(map[uuid.UUID]domain.BoardTask, len(all))
	for _, t := range all {
		byID[t.ID] = t
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn().Err(err).Str("root", root).Msg("workspace reaper: reading the workspace root failed")
		}
		return
	}

	var reaped int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskID, ok := parseTaskDirName(entry.Name())
		if !ok {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !r.eligible(ctx, taskID, byID, path) {
			continue
		}
		if _, err := proctree.KillProcessesUnder(path, 3*time.Second); err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Msg("workspace reaper: stopping processes still running in the workspace failed")
		}
		if err := workspace.RemoveDirWithin(root, path); err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Msg("workspace reaper: removing a finished task's workspace failed")
			continue
		}
		reaped++
		log.Info().Str("task_id", taskID.String()).Msg("workspace reaper: reclaimed a finished task's workspace")
	}
	if reaped > 0 {
		log.Info().Int("count", reaped).Msg("workspace reaper: pass complete")
	}
	r.pruneBranchIndexes(ctx, root)
}

// pruneBranchIndexes drops the branch index of every task workspace that is no
// longer on disk — reaped above, deleted with its task, or reaped by a build
// that predates this pruning. Keyed on the workspace directory rather than on
// the task row, so an index whose task is gone is collected too. A run that
// needs the workspace again re-clones it and builds a fresh branch index from
// the repository's.
func (r *WorkspaceReaper) pruneBranchIndexes(ctx context.Context, root string) {
	if r.indexes == nil {
		return
	}
	indexes, err := r.indexes.ListBranchIndexes(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("workspace reaper: listing branch indexes failed")
		return
	}
	var pruned int
	for _, idx := range indexes {
		dir := filepath.Base(filepath.Clean(idx.RootPath))
		if _, ok := parseTaskDirName(dir); !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
			continue
		}
		if err := r.indexes.DeleteIndex(ctx, idx.ID); err != nil {
			log.Warn().Err(err).Str("index_id", idx.ID.String()).Msg("workspace reaper: deleting a reaped workspace's branch index failed")
			continue
		}
		pruned++
	}
	if pruned > 0 {
		log.Info().Int("count", pruned).Msg("workspace reaper: dropped branch indexes of reaped workspaces")
	}
}

func (r *WorkspaceReaper) eligible(ctx context.Context, taskID uuid.UUID, byID map[uuid.UUID]domain.BoardTask, path string) bool {
	if r.active != nil {
		live, err := r.active.HasLiveRunForTask(ctx, taskID, runLiveWithin)
		if err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).
				Msg("workspace reaper: could not tell whether a run holds this workspace, keeping it")
			return false
		}
		if live {
			return false
		}
	}

	task, known := byID[taskID]
	if !known {
		info, err := os.Stat(path)
		if err != nil {
			return false
		}
		return time.Since(info.ModTime()) > r.grace
	}

	if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
		return false
	}
	return time.Since(task.UpdatedAt) > r.grace
}

func parseTaskDirName(name string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(name, "task-")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
