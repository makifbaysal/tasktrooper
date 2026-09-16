package board

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// WorkOrderResourceLister is BlockedResourceLister's listing half plus the
// work_order resource's own release, ClearWorkOrderWaiting — which, unlike
// TakeBlockedResourceTask, does not restore board_column because this park
// never moved it in the first place.
type WorkOrderResourceLister interface {
	ListBlockedByResource(ctx context.Context, resource string, limit int) ([]domain.BoardTask, error)
	ClearWorkOrderWaiting(ctx context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error)
}

// WorkOrderSweeperInterval is how often a task parked behind another task asks
// whether that task has landed.
//
// One minute, the shortest of the four sweeps, because what it asks is the
// cheapest thing any of them ask: one indexed query against a table in the same
// database, where the device sweeper crosses a home tunnel and the deploy
// sweeper calls GitHub. The thing being waited for is also the fastest to
// matter — the blocker reaching `done` is the moment its dependent should be
// picked up, and a developer agent idle for ten minutes after its prerequisite
// landed is ten minutes of a queue nobody is working.
const WorkOrderSweeperInterval = time.Minute

// workOrderSweepBatch bounds one pass. A board can legitimately have a lot of
// tasks queued behind one big migration, so this is larger than the deploy
// sweeper's — but it is still a ceiling, and the next pass takes the rest.
const workOrderSweepBatch = 100

// WorkOrderSweeper releases tasks parked on domain.ResourceWorkOrder once the
// tasks they were waiting for are finished.
//
// It is shaped like DeploySweeper and not like DeviceSweeper, for the reason
// that split those two: a work-order park is per-TASK. Two parked tasks are
// waiting for two different blockers, and the one parked most recently may be
// the one whose blocker lands first — so this lists without claiming, asks about
// each, and takes only the ones that are actually free.
//
// What it asks is the relation graph itself: ListBlockingSources returns the
// UNFINISHED sources of a task's blocks rows, so an empty answer means every
// blocker reached done or released. That one query also covers the two ways a
// blocker can stop existing rather than finish:
//
//	deleted — task_relations cascades on board_tasks delete (migration 022), so
//	  the edge is gone and the query returns nothing. A cancelled blocker
//	  releases its dependents without anybody having to remember to.
//	moved back — a blocker that returns to in_progress makes the query answer
//	  with it again, and the dependent stays parked. The park is a standing
//	  question, not a one-off verdict.
type WorkOrderSweeper struct {
	tasks      WorkOrderResourceLister
	relations  BlockerReader
	dispatcher *Dispatcher
}

func NewWorkOrderSweeper(tasks WorkOrderResourceLister, relations BlockerReader, dispatcher *Dispatcher) *WorkOrderSweeper {
	return &WorkOrderSweeper{tasks: tasks, relations: relations, dispatcher: dispatcher}
}

// Start runs the sweep on interval until ctx ends.
//
// It sweeps immediately on boot, for the reason the quota and deploy sweepers
// do and the device sweeper does not: what it reads is a fact recorded in this
// database, unaffected by which pod is running or what leases it holds. A
// blocker that reached done while the pod was being replaced would otherwise
// hold its dependents for a full interval on top of the restart.
func (s *WorkOrderSweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.tasks == nil || s.relations == nil || s.dispatcher == nil {
		return
	}
	if interval <= 0 {
		interval = WorkOrderSweeperInterval
	}
	go func() {
		s.sweep(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweep(ctx)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("work order sweeper started")
}

// sweep resumes every parked task whose blockers have all landed. Exposed
// separately from Start so tests can drive it without a clock.
func (s *WorkOrderSweeper) sweep(ctx context.Context) {
	parked, err := s.tasks.ListBlockedByResource(ctx, domain.ResourceWorkOrder, workOrderSweepBatch)
	if err != nil {
		log.Warn().Err(err).Msg("work order sweeper: listing parked tasks failed")
		return
	}
	for _, task := range parked {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.resumeIfClear(ctx, task)
	}
}

func (s *WorkOrderSweeper) resumeIfClear(ctx context.Context, parked domain.BoardTask) {
	blockers, err := s.relations.ListBlockingSources(ctx, parked.ID)
	if err != nil {
		// Same rule as the deploy sweeper: a question that cannot be answered
		// leaves the task parked. Resuming on an error would spend an agent run
		// to be told what the sweep could not find out, and do it again next
		// pass.
		log.Warn().Err(err).Str("task_id", parked.ID.String()).Msg("work order sweeper: reading blockers failed")
		return
	}
	if len(blockers) > 0 {
		return
	}
	task, ok, err := s.tasks.ClearWorkOrderWaiting(ctx, parked.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", parked.ID.String()).Msg("work order sweeper: claiming a parked task failed")
		return
	}
	if !ok {
		// Claimed between the list and the take — another pod sweeping, or a
		// human dragging the card out of blocked. Both are fine.
		return
	}
	if err := s.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"resumed":  "work_order_clear",
			"resource": domain.ResourceWorkOrder,
			// The marker workOrderGateApplies reads to let this one dispatch
			// through without re-asking the question the sweep just answered.
			domain.EventPayloadResumedResource: domain.ResourceWorkOrder,
			// The resume is the control plane's move, not a human's; without
			// these the board history renders an empty "Moved by User" row for
			// a move nobody made.
			domain.EventPayloadActor:  domain.EventActorSystem,
			domain.EventPayloadReason: domain.MoveReasonResourceFree,
		},
	}); err != nil {
		// The block is already cleared, so the task is back in its column
		// either way; the reconciler picks up a task that never started.
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("work order sweeper: redispatch failed")
		return
	}
	log.Info().Str("task_id", task.ID.String()).
		Msg("parked task resumed: everything it was waiting for is done")
}
