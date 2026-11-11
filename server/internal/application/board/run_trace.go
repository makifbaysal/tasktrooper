package board

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (r *Runner) gitInspector() port.RunGitInspector {
	if r.git == nil {
		return nil
	}
	insp, _ := r.git.(port.RunGitInspector)
	return insp
}

// startRunTrace opens the trace of this run: the commit its task workspace
// starts from is what the comparison with git and any undo are measured from.
func (r *Runner) startRunTrace(ctx context.Context, job RunJob, workDir, taskWorkspace, taskBranch string, columnBefore domain.TaskColumn) *runtrace.Recorder {
	if r.traces == nil {
		return nil
	}
	base := ""
	if insp := r.gitInspector(); insp != nil && taskWorkspace != "" {
		base, _ = insp.HeadSHA(ctx, taskWorkspace)
	}
	return runtrace.Start(ctx, r.traces, runtrace.Meta{
		RunID: job.Run.ID, TaskID: job.Task.ID, AgentID: job.Run.AgentID,
		WorkDir: workDir, WorkspacePath: taskWorkspace, BaseSHA: base,
		Branch: taskBranch, ColumnBefore: string(columnBefore),
	})
}

func (r *Runner) finishRunTrace(ctx context.Context, rec *runtrace.Recorder) {
	if rec == nil {
		return
	}
	rec.Abandon(ctx)
	runtrace.Finalize(ctx, r.traces, r.gitInspector(), rec.Meta(), rec.Dropped())
}
