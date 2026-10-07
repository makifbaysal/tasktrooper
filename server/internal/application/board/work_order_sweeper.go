package board

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type WorkOrderResourceLister interface {
	ListBlockedByResource(ctx context.Context, resource string, limit int) ([]domain.BoardTask, error)
	ClearWorkOrderWaiting(ctx context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error)
}

// WorkOrderSweeperInterval is only the backstop: a blocker finishing wakes its
// dependents itself (WakeDependentsOf), and SweepSoon covers a blocker deleted
// and a park that raced its blocker finishing.
const WorkOrderSweeperInterval = 5 * time.Minute

const workOrderSweepBatch = 100

// A work-order park is per-TASK: blockers land out of order, so list without claiming and take only what is free.
type WorkOrderSweeper struct {
	tasks      WorkOrderResourceLister
	relations  BlockerReader
	dispatcher *Dispatcher
	dependents DependentsReader
	comments   TaskCommenter
	soon       chan struct{}
}

func NewWorkOrderSweeper(tasks WorkOrderResourceLister, relations BlockerReader, dispatcher *Dispatcher) *WorkOrderSweeper {
	return &WorkOrderSweeper{tasks: tasks, relations: relations, dispatcher: dispatcher, soon: make(chan struct{}, 1)}
}

type DependentsReader interface {
	ListBySource(ctx context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error)
}

func (s *WorkOrderSweeper) SetDependents(d DependentsReader) {
	if s != nil {
		s.dependents = d
	}
}

func (s *WorkOrderSweeper) SetCommenter(c TaskCommenter) {
	if s != nil {
		s.comments = c
	}
}

func (s *WorkOrderSweeper) WakeDependentsOf(ctx context.Context, blockerTaskID uuid.UUID) {
	if s == nil || s.dependents == nil {
		return
	}
	rels, err := s.dependents.ListBySource(ctx, blockerTaskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", blockerTaskID.String()).
			Msg("work order sweeper: reading dependents failed")
		return
	}
	for _, rel := range rels {
		if rel.RelationType != domain.TaskRelationBlocks {
			continue
		}
		s.resumeIfClear(ctx, domain.BoardTask{ID: rel.TargetTaskID})
	}
}

// SweepSoon asks for a pass now instead of at the next interval. It never
// blocks; requests made while one is pending collapse into it.
func (s *WorkOrderSweeper) SweepSoon() {
	if s == nil {
		return
	}
	select {
	case s.soon <- struct{}{}:
	default:
	}
}

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
			case <-s.soon:
				s.sweep(ctx)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("work order sweeper started")
}

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
		return
	}
	if err := s.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"resumed":                          "work_order_clear",
			"resource":                         domain.ResourceWorkOrder,
			domain.EventPayloadResumedResource: domain.ResourceWorkOrder,
			domain.EventPayloadActor:           domain.EventActorSystem,
			domain.EventPayloadReason:          domain.MoveReasonResourceFree,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("work order sweeper: redispatch failed")
		return
	}
	if s.comments != nil {
		if _, cerr := s.comments.AddComment(ctx, task.RepositoryID, task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    prompt.Text(workOrderResumedKey),
		}); cerr != nil {
			log.Warn().Err(cerr).Str("task_id", task.ID.String()).Msg("work order sweeper: resume comment failed")
		}
	}
	log.Info().Str("task_id", task.ID.String()).
		Msg("parked task resumed: everything it was waiting for is done")
}
