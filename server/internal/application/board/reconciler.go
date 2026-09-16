package board

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// Reconciler recovers task_agent_runs left orphaned by a process restart or a
// job dropped from the runner's in-memory queue. Dispatch only fires on board
// events (task.created/moved/assigned/commented), so without this nothing
// else ever revisits a task whose run silently died mid-flight.
type Reconciler struct {
	runs         port.TaskAgentRunStore
	tasks        port.BoardTaskStore
	dispatcher   *Dispatcher
	staleAfter   time.Duration
	plans        PlanSettler
	criteriaLoop *CriteriaLoopGuard
}

// PlanSettler is the slice of the catalog the reconciler needs to close out the
// orchestration plan a dead run left behind. A killed pod writes nothing on its
// way out, so the plan and its subtasks keep the "running" status they were
// given when they started — and the plan row is what the UI reads to decide a
// run is still live.
type PlanSettler interface {
	GetPlanByRunID(ctx context.Context, runID uuid.UUID) (domain.PlanView, error)
	UpdatePlanStatus(ctx context.Context, planID uuid.UUID, status string) error
	UpdateTaskStatus(ctx context.Context, taskID uuid.UUID, status, result, errMsg string) error
}

// maxRunStale caps how long the reconciler will wait before treating a running
// row as abandoned, whatever board.reconcile_stale_after says.
//
// The configured value defaulted to thirty minutes, from before a run
// heartbeated at all: age was the only evidence there was, so patience was the
// only safety. A running row now bumps updated_at every runHeartbeat, so a row
// eighteen beats old has a dead owner — there is nothing more to learn by
// waiting another twenty-nine minutes, and everything to lose: that is how long
// a card spun after a pod was evicted mid-run.
//
// Deliberately a clamp rather than a replacement of the setting. An operator
// who lowered it meant it; one who left it at the old default meant "be
// careful", and this is what careful now costs.
const maxRunStale = 18 * runHeartbeat

func NewReconciler(runs port.TaskAgentRunStore, tasks port.BoardTaskStore, dispatcher *Dispatcher, staleAfter time.Duration) *Reconciler {
	if staleAfter <= 0 || staleAfter > maxRunStale {
		staleAfter = maxRunStale
	}
	return &Reconciler{runs: runs, tasks: tasks, dispatcher: dispatcher, staleAfter: staleAfter}
}

// SetPlanSettler wires the catalog in so a recovered run also closes out its
// orchestration plan. Nil-safe: without it only the run row is recovered.
func (r *Reconciler) SetPlanSettler(p PlanSettler) {
	if r != nil {
		r.plans = p
	}
}

// SetCriteriaLoopGuard wires the brake on unsettled-criteria failure streaks.
// Nil-safe: without it dispatchNeverStarted keeps its old behaviour of simply
// giving up silently once maxConsecutiveFailedRuns is reached, whatever the
// failure reason.
func (r *Reconciler) SetCriteriaLoopGuard(g *CriteriaLoopGuard) {
	if r != nil {
		r.criteriaLoop = g
	}
}

// Start recovers everything the previous process left behind, then repeats a
// normal sweep every interval until ctx is cancelled. Runs in its own
// goroutine; callers don't need to wait on it.
func (r *Reconciler) Start(ctx context.Context, interval time.Duration) {
	if r == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	// The startup sweep used to run with a cutoff of NOW, on the premise that a
	// run row existing when this process boots cannot be executing anywhere —
	// "one pod serves a tenant, and this pod has just started with an empty
	// active set". Both halves of that premise are gone. A booting replica
	// joins a fleet that is already working, and its own empty memory says
	// nothing about anybody else's runs; sweeping on that premise would fail
	// every live run in the fleet on every deploy, which is the single most
	// destructive thing a second replica could do.
	//
	// It also no longer buys anything. What the boot sweep was FOR — not
	// leaving a killed run spinning for half an hour — is now the ordinary
	// sweep's job, because staleAfter is clamped to eighteen missed heartbeats
	// (maxRunStale). The first tick recovers a genuinely dead run within
	// minutes and cannot touch a live one, on any replica, at any time.
	go func() {
		r.Run(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.Run(ctx)
			}
		}
	}()
}

// Run performs a single periodic sweep with two independent passes:
//  1. any run still pending/running past staleAfter is marked failed and its
//     task re-dispatched (recovers a run orphaned by a restart or dropped job).
//     Start uses a tighter cutoff for its first pass — see there.
//  2. any task with an assignee that has never been dispatched at all — e.g.
//     it was created while dispatch was disabled, or the app crashed before
//     the first dispatch — is dispatched now. Tasks with at least one run in
//     their history are left alone here; a currently-stuck one is handled by
//     pass 1 instead, so this never fires a second concurrent run for it.
func (r *Reconciler) Run(ctx context.Context) {
	if r == nil {
		return
	}
	r.run(ctx, time.Now().Add(-r.staleAfter))
}

// run performs a sweep treating every unfinished run older than cutoff as
// orphaned. Callers choose the cutoff: the periodic sweep can only guess from
// age (staleAfter), while the startup sweep knows for certain that nothing
// predating this process is alive.
func (r *Reconciler) run(ctx context.Context, cutoff time.Time) {
	if r == nil || r.runs == nil || r.tasks == nil || r.dispatcher == nil {
		return
	}
	tasks, err := r.tasks.ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("reconciler: list tasks failed")
		return
	}
	byID := make(map[uuid.UUID]domain.BoardTask, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}

	// A PENDING run gets a far shorter rope than a running one, and the reason is
	// that the two rows mean different things. A running row heartbeats (Touch),
	// so its age is genuinely ambiguous and 30 minutes of patience is what keeps
	// a long, healthy run from being killed. A pending row heartbeats nothing: it
	// says "some process accepted this and has not started it", and the only
	// process that could answer is asked directly below, through IsActive — which
	// covers queued and parked jobs too. A pending run nobody here owns is
	// orphaned, whatever its age.
	//
	// This is what left a card spinning after a control-plane restart handed the
	// work from the cloud pod to the local runner mid-dispatch: the pod created
	// the row, went away with the job still in its memory, and the runner that
	// took over had booted BEFORE the row existed, so the startup sweep had
	// already passed it by. Nothing looked at it again for half an hour, and the
	// queued-run guard meant no new dispatch could replace it either.
	pendingCutoff := time.Now().Add(-pendingStaleAfter)
	if cutoff.After(pendingCutoff) {
		pendingCutoff = cutoff
	}
	// The more recent of the two cutoffs is the superset: every row either window
	// could claim comes back, and the per-row check below decides which applies.
	stale, err := r.runs.ListStale(ctx, pendingCutoff)
	if err != nil {
		log.Warn().Err(err).Msg("reconciler: list stale runs failed")
	} else {
		for _, run := range stale {
			limit := cutoff
			if run.Status == domain.TaskAgentRunStatusPending {
				limit = pendingCutoff
			}
			if !run.UpdatedAt.Before(limit) {
				continue
			}
			// No in-process "is this mine" check any more. It answered for one
			// replica's memory and reported every OTHER replica's live run as
			// abandoned, which is exactly the collision it was written to
			// prevent. The row's own heartbeat is the only answer visible to
			// every process, and recoverStale re-asserts it inside the write —
			// so an owner that heartbeats while this sweep is in flight keeps
			// its run, whichever pod that owner is.
			task, ok := byID[run.TaskID]
			r.recoverStale(ctx, run, task, ok, limit)
		}
	}

	r.dispatchNeverStarted(ctx, tasks)
}

// pendingStaleAfter is how long a queued run may sit unclaimed before the
// reconciler treats it as orphaned. Short on purpose — see the sweep for why a
// pending row is not the same kind of evidence as a running one. It is not zero
// because the ownership check is per process: a dispatch and the worker picking
// it up are two moments, and a control plane that has just handed the run
// over needs a beat for the new owner to be the one answering.
const pendingStaleAfter = 2 * time.Minute

// maxConsecutiveFailedRuns caps the reconciler's automatic retry of a task
// whose runs keep failing. After this many failures in a row the task is left
// where it is for a human — retrying a deterministic failure forever only
// burns provider budget.
const maxConsecutiveFailedRuns = 3

// dispatchNeverStarted dispatches assigned, non-terminal tasks that have no
// live run and would otherwise never be revisited. Two cases:
//  1. zero task_agent_runs ever — the triggering dispatch never happened
//     (config gap, crash before the first run row was even created);
//  2. the latest run FAILED (e.g. the agent loop hit max iterations) — the
//     self-dispatch guard means no follow-up event will retry it, so the
//     reconciler is now the only retry path. Bounded by
//     maxConsecutiveFailedRuns so a hard failure cannot loop forever.
func (r *Reconciler) dispatchNeverStarted(ctx context.Context, tasks []domain.BoardTask) {
	for _, task := range tasks {
		if task.AssigneeAgentID == nil {
			continue
		}
		switch task.Column {
		case domain.TaskColumnDone, domain.TaskColumnReleased,
			domain.TaskColumnBlocked, domain.TaskColumnBacklog:
			// Terminal columns are finished; a blocked task is waiting on a
			// human answer and resumes through the clarification flow; a
			// backlog task has not been taken onto the board, so reviving it
			// here would start work nobody scheduled (see
			// isDispatchSuspendedColumn).
			continue
		}
		if task.BlockedResource == domain.ResourceWorkOrder {
			// Parked in place (todo/in_progress) rather than moved to
			// TaskColumnBlocked, so it has zero task_agent_runs and would
			// otherwise look never_dispatched on every sweep. The
			// WorkOrderSweeper is what resumes it, not this loop.
			continue
		}
		runs, err := r.runs.ListByTask(ctx, task.ID, maxConsecutiveFailedRuns)
		if err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("reconciler: list runs for task failed")
			continue
		}
		reason := ""
		if len(runs) == 0 {
			reason = "never_dispatched"
		} else if runs[0].Status == domain.TaskAgentRunStatusFailed {
			failed := 0
			for _, run := range runs {
				if run.Status != domain.TaskAgentRunStatusFailed {
					break
				}
				failed++
			}
			if failed >= maxConsecutiveFailedRuns {
				// The ordinary cap gives up silently here. That is still
				// correct for a crash-caused streak — retrying a
				// deterministic failure forever only burns provider budget,
				// and nothing about a crash names what a human should do
				// next. An unsettled-criteria streak is different: the same
				// acceptance criteria staying open run after run is exactly
				// the evidence CriteriaLoopGuard's two sibling guards
				// (review loop, pipeline bounce) already act on, so it gets
				// the same treatment — a park with a comment naming what is
				// stuck — instead of the same silent stop.
				if r.criteriaLoop != nil {
					r.criteriaLoop.Hold(ctx, task.RepositoryID, task, runs)
				}
				continue
			}
			reason = "retry_failed_run"
		} else {
			// Latest run is pending, running, or completed — nothing to do here;
			// stuck pending/running runs are pass 1's job.
			continue
		}
		log.Info().Str("task_id", task.ID.String()).Str("agent_id", task.AssigneeAgentID.String()).
			Str("reason", reason).Msg("reconciler: dispatching idle assigned task")
		if err := r.dispatcher.Dispatch(ctx, DispatchInput{
			RepositoryID: task.RepositoryID,
			Task:         task,
			EventType:    domain.BoardEventTaskMoved,
			Payload: map[string]interface{}{
				"reconciled":              true,
				"reason":                  reason,
				domain.EventPayloadActor:  domain.EventActorSystem,
				domain.EventPayloadReason: domain.MoveReasonReconciled,
			},
		}); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("reconciler: dispatch idle task failed")
		}
	}
}

func (r *Reconciler) recoverStale(ctx context.Context, run domain.TaskAgentRun, task domain.BoardTask, taskFound bool, cutoff time.Time) {
	summary := run.Summary
	if summary == "" {
		summary = "reconciler: no progress before stale timeout, recovered for re-dispatch"
	}
	// FailIfStale, not Update: the list and this write are two moments, and the
	// gap between them can be arbitrarily long. Update wrote 'failed'
	// unconditionally, so a run whose owner heartbeated during that gap — a
	// live run, on another replica — was marked failed under it and its task
	// re-dispatched to a second agent on the same branch. Re-asserting the
	// cutoff in the WHERE clause makes the list advisory and the write
	// authoritative.
	failed, err := r.runs.FailIfStale(ctx, run.ID, cutoff, summary)
	if err != nil {
		log.Warn().Err(err).Str("run_id", run.ID.String()).Msg("reconciler: mark stale run failed failed")
		return
	}
	if !failed {
		log.Info().Str("run_id", run.ID.String()).
			Msg("reconciler: run was live or already settled by the time it was written, leaving it alone")
		return
	}

	r.settlePlan(ctx, run)

	// Task deleted, or already reached a terminal column since the run went
	// stale (e.g. a human moved it manually) — nothing left to re-dispatch.
	if !taskFound || task.Column == domain.TaskColumnDone || task.Column == domain.TaskColumnReleased {
		return
	}

	log.Info().Str("task_id", task.ID.String()).Str("agent_id", run.AgentID.String()).Msg("reconciler: re-dispatching stale task")
	if err := r.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"reconciled":              true,
			domain.EventPayloadActor:  domain.EventActorSystem,
			domain.EventPayloadReason: domain.MoveReasonReconciled,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("reconciler: re-dispatch failed")
	}
}

// planRecoveredReason is what a subtask left mid-flight by a dead process says
// on its card, instead of spinning.
const planRecoveredReason = "run ended before this subtask reported a result"

// settlePlan closes out the orchestration plan of a run the reconciler just
// failed. The run row alone is not enough: the plan and its subtasks carry
// their own status, and a card whose plan still says "running" keeps a spinner
// on work whose process died — DE-1 sat that way while its pod was already
// replaced.
func (r *Reconciler) settlePlan(ctx context.Context, run domain.TaskAgentRun) {
	if r.plans == nil || run.SessionRunID == nil {
		return
	}
	plan, err := r.plans.GetPlanByRunID(ctx, *run.SessionRunID)
	if err != nil {
		// No plan for this run is the normal case for a single-agent run.
		return
	}
	for _, task := range plan.Tasks {
		// Only subtasks that had started: a still-pending one never ran, and
		// "pending" is the honest thing for it to say.
		if task.Status != domain.TaskStatusRunning {
			continue
		}
		if err := r.plans.UpdateTaskStatus(ctx, task.ID, domain.TaskStatusFailed, task.Result, planRecoveredReason); err != nil {
			log.Warn().Err(err).Str("plan_task_id", task.ID.String()).Msg("reconciler: settle plan task failed")
		}
	}
	// Pending means the plan is parked on a stakeholder answer, which is a state
	// a human resumes — only a plan still claiming to run gets closed out here.
	if plan.Status == domain.PlanStatusRunning {
		if err := r.plans.UpdatePlanStatus(ctx, plan.ID, domain.PlanStatusFailed); err != nil {
			log.Warn().Err(err).Str("plan_id", plan.ID.String()).Msg("reconciler: settle plan failed")
		}
	}
}
