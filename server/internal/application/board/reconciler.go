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
