package board

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// Dispatch fires only on board events, so a run whose run died mid-flight is revisited by nothing else.
type Reconciler struct {
	runs         port.TaskAgentRunStore
	tasks        port.BoardTaskStore
	dispatcher   *Dispatcher
	staleAfter   time.Duration
	plans        PlanSettler
	criteriaLoop *CriteriaLoopGuard
	workflows    port.WorkflowReader
	roles        port.RoleResolver
	quorum       *ReviewQuorum
	mover        TaskMover
}

type TaskMover interface {
	UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error)
}

func (r *Reconciler) SetReviewQuorum(q *ReviewQuorum, mover TaskMover) {
	r.quorum = q
	r.mover = mover
}

func (r *Reconciler) SetWorkflows(w port.WorkflowReader)   { r.workflows = w }
func (r *Reconciler) SetRoleResolver(rr port.RoleResolver) { r.roles = rr }

type PlanSettler interface {
	GetPlanByRunID(ctx context.Context, runID uuid.UUID) (domain.PlanView, error)
	UpdatePlanStatus(ctx context.Context, planID uuid.UUID, status string) error
	UpdateTaskStatus(ctx context.Context, taskID uuid.UUID, status, result, errMsg string) error
}

// Clamp, not replacement: eighteen missed heartbeats means a dead owner, and more patience only spins the card longer.
const maxRunStale = 18 * runHeartbeat

func NewReconciler(runs port.TaskAgentRunStore, tasks port.BoardTaskStore, dispatcher *Dispatcher, staleAfter time.Duration) *Reconciler {
	if staleAfter <= 0 || staleAfter > maxRunStale {
		staleAfter = maxRunStale
	}
	return &Reconciler{runs: runs, tasks: tasks, dispatcher: dispatcher, staleAfter: staleAfter}
}

func (r *Reconciler) SetPlanSettler(p PlanSettler) {
	if r != nil {
		r.plans = p
	}
}

func (r *Reconciler) SetCriteriaLoopGuard(g *CriteriaLoopGuard) {
	if r != nil {
		r.criteriaLoop = g
	}
}

func (r *Reconciler) Start(ctx context.Context, interval time.Duration) {
	if r == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
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

func (r *Reconciler) Run(ctx context.Context) {
	if r == nil {
		return
	}
	r.run(ctx, time.Now().Add(-r.staleAfter))
}

func (r *Reconciler) run(ctx context.Context, cutoff time.Time) {
	if r == nil || r.runs == nil || r.tasks == nil || r.dispatcher == nil {
		return
	}

	pendingCutoff := time.Now().Add(-pendingStaleAfter)
	if cutoff.After(pendingCutoff) {
		pendingCutoff = cutoff
	}
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
			task, err := r.tasks.GetByID(ctx, run.TaskID)
			found := err == nil
			if err != nil && !errors.Is(err, domain.ErrBoardTaskNotFound) {
				log.Warn().Err(err).Str("run_id", run.ID.String()).Msg("reconciler: reading a stale run's task failed")
				continue
			}
			r.recoverStale(ctx, run, task, found, limit)
		}
	}

	candidates, err := r.tasks.ListDispatchCandidates(ctx, maxConsecutiveFailedRuns)
	if err != nil {
		log.Warn().Err(err).Msg("reconciler: list dispatch candidates failed")
		return
	}
	r.dispatchNeverStarted(ctx, candidates)
}

const pendingStaleAfter = 2 * time.Minute

const maxConsecutiveFailedRuns = 3

// dispatchNeverStarted re-checks the candidate filter the store already applied
// in SQL, so a store that returns more than the predicate selects cannot make
// the reconciler dispatch a parked or finished task.
func (r *Reconciler) dispatchNeverStarted(ctx context.Context, candidates []port.TaskRecentRuns) {
	for _, candidate := range candidates {
		task, runs := candidate.Task, candidate.Runs
		if task.AssigneeAgentID == nil {
			continue
		}
		switch task.Column {
		case domain.TaskColumnDone, domain.TaskColumnReleased,
			domain.TaskColumnBlocked, domain.TaskColumnBacklog:
			continue
		}
		if task.BlockedResource == domain.ResourceWorkOrder {
			continue
		}
		if r.reviveStalledReview(ctx, task, runs) {
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
				if r.criteriaLoop != nil {
					r.criteriaLoop.Hold(ctx, task.RepositoryID, task, runs)
				}
				continue
			}
			reason = "retry_failed_run"
		} else {
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

// A quorum review round waits for every required reviewer. One whose run
// died while another reviewer finished later is invisible to the newest-run
// rule above, so the round is read directly: a reviewer still to decide whose
// last run failed is started again, and once it has failed as often as any
// task may, a round that already holds a rejection is closed on it.
func (r *Reconciler) reviveStalledReview(ctx context.Context, task domain.BoardTask, runs []domain.TaskAgentRun) bool {
	pending, rejected, ok := r.quorum.RoundState(ctx, task)
	if !ok || len(pending) == 0 {
		return false
	}
	waiting := make(map[uuid.UUID]bool, len(pending))
	for _, id := range pending {
		waiting[id] = true
	}
	failures := 0
	lastFailed := false
	seen := make(map[uuid.UUID]bool, len(pending))
	for _, run := range runs {
		if run.Status == domain.TaskAgentRunStatusPending || run.Status == domain.TaskAgentRunStatusRunning {
			return false
		}
		if !waiting[run.AgentID] {
			continue
		}
		failed := run.Status == domain.TaskAgentRunStatusFailed
		if failed {
			failures++
		}
		if !seen[run.AgentID] {
			seen[run.AgentID] = true
			lastFailed = lastFailed || failed
		}
	}
	if failures >= maxConsecutiveFailedRuns {
		if !rejected || r.mover == nil {
			return false
		}
		column := domain.TaskColumnNeedRevision
		if _, err := r.mover.UpdateTask(ctx, task.RepositoryID, task.ID, domain.UpdateBoardTaskRequest{
			Column:       &column,
			SystemReason: domain.MoveReasonReviewRejectedStalled,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).
				Msg("reconciler: closing a stalled review round on its rejection failed")
			return false
		}
		log.Info().Str("task_id", task.ID.String()).
			Msg("reconciler: review round stalled on a reviewer that keeps failing, sent back on the rejection it holds")
		return true
	}
	if !lastFailed {
		return false
	}
	log.Info().Str("task_id", task.ID.String()).Int("pending_reviewers", len(pending)).
		Msg("reconciler: re-dispatching a review round whose pending reviewer's run failed")
	if err := r.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"reconciled":              true,
			"reason":                  "review_quorum_pending",
			domain.EventPayloadActor:  domain.EventActorSystem,
			domain.EventPayloadReason: domain.MoveReasonReconciled,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("reconciler: re-dispatching a stalled review round failed")
	}
	return true
}

func (r *Reconciler) recoverStale(ctx context.Context, run domain.TaskAgentRun, task domain.BoardTask, taskFound bool, cutoff time.Time) {
	summary := run.Summary
	if summary == "" {
		summary = "reconciler: no progress before stale timeout, recovered for re-dispatch"
	}
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

const planRecoveredReason = "run ended before this subtask reported a result"

func (r *Reconciler) settlePlan(ctx context.Context, run domain.TaskAgentRun) {
	if r.plans == nil || run.SessionRunID == nil {
		return
	}
	plan, err := r.plans.GetPlanByRunID(ctx, *run.SessionRunID)
	if err != nil {
		return
	}
	for _, task := range plan.Tasks {
		if task.Status != domain.TaskStatusRunning {
			continue
		}
		if err := r.plans.UpdateTaskStatus(ctx, task.ID, domain.TaskStatusFailed, task.Result, planRecoveredReason); err != nil {
			log.Warn().Err(err).Str("plan_task_id", task.ID.String()).Msg("reconciler: settle plan task failed")
		}
	}
	if plan.Status == domain.PlanStatusRunning {
		if err := r.plans.UpdatePlanStatus(ctx, plan.ID, domain.PlanStatusFailed); err != nil {
			log.Warn().Err(err).Str("plan_id", plan.ID.String()).Msg("reconciler: settle plan failed")
		}
	}
}
