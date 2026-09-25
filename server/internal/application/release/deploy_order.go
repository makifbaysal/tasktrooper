package release

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DeployOrder reads deploy_depends_on edges in both directions: a task's own
// dependencies (it is the source) and the tasks waiting on a set of tasks
// (they are the targets).
type DeployOrder interface {
	ListBySource(ctx context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error)
	ListDeployDependents(ctx context.Context, targetTaskIDs []uuid.UUID) ([]domain.TaskRelation, error)
}

// TaskLocator resolves the repository of a task; a deploy dependency may
// live in another repository (a backend API a frontend waits on).
type TaskLocator interface {
	FindTaskRepositoryID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error)
}

// pendingDeployDependencies lists, for the given tasks, every deploy
// dependency that is neither released nor shipping in the same set. A
// dependency that cannot be read counts as pending: shipping out of order
// cannot be undone, waiting can.
func (s *Service) pendingDeployDependencies(ctx context.Context, taskIDs []uuid.UUID) []string {
	if s.deployOrder == nil || s.tasks == nil {
		return nil
	}
	inSet := make(map[uuid.UUID]bool, len(taskIDs))
	for _, id := range taskIDs {
		inSet[id] = true
	}
	var pending []string
	seen := map[uuid.UUID]bool{}
	for _, id := range taskIDs {
		rels, err := s.deployOrder.ListBySource(ctx, id)
		if err != nil {
			log.Warn().Err(err).Str("task_id", id.String()).Msg("release: reading deploy dependencies failed")
			pending = append(pending, id.String()+" (its deploy dependencies could not be read)")
			continue
		}
		for _, rel := range rels {
			if rel.RelationType != domain.TaskRelationDeployDependsOn || inSet[rel.TargetTaskID] || seen[rel.TargetTaskID] {
				continue
			}
			seen[rel.TargetTaskID] = true
			label := domain.RelationLabel(rel.TargetKey, rel.TargetTitle, rel.TargetTaskID)
			target, err := s.locateTask(ctx, rel.TargetTaskID)
			if err != nil {
				pending = append(pending, label+" (cannot be read)")
				continue
			}
			if target.Column != domain.TaskColumnReleased {
				pending = append(pending, fmt.Sprintf("%s [%s]", label, target.Column))
			}
		}
	}
	return pending
}

func (s *Service) locateTask(ctx context.Context, taskID uuid.UUID) (domain.BoardTask, error) {
	if s.locator == nil {
		return domain.BoardTask{}, fmt.Errorf("no task locator is configured")
	}
	repoID, err := s.locator.FindTaskRepositoryID(ctx, taskID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	return s.tasks.GetTask(ctx, repoID, taskID)
}

func deployDependencyError(pending []string) error {
	return fmt.Errorf("%w: it must ship after %s. Nothing was deployed. Do not retry — you will be woken when they are released",
		domain.ErrDeployDependencyPending, strings.Join(pending, ", "))
}

func (s *Service) commentDeployDependencies(ctx context.Context, repositoryID, taskID uuid.UUID, pending []string) {
	s.commentOnce(ctx, repositoryID, taskID, "Waiting to ship: this task declares it deploys after "+
		strings.Join(pending, ", ")+", which is not in production yet. It ships as soon as that is released "+
		"(or drop the dependency if the order no longer applies).")
}

// wakeDeployDependents resumes tasks in done that were held back only by the
// tasks this release just shipped.
func (s *Service) wakeDeployDependents(ctx context.Context, r domain.Release) {
	s.wakeDeployDependentsOfTasks(ctx, r.TaskIDs())
}

// WakeDeployDependentsOf is N7's entry point for board.Dispatcher's
// SetTaskReleasedHook: a task can reach `released` with no release row at
// all (delivery mode none, or a human moving the card directly), and its
// deploy_depends_on waiters must still wake regardless of what moved it —
// Finish is not the only door into released.
func (s *Service) WakeDeployDependentsOf(ctx context.Context, task domain.BoardTask) {
	s.wakeDeployDependentsOfTasks(ctx, []uuid.UUID{task.ID})
}

func (s *Service) wakeDeployDependentsOfTasks(ctx context.Context, taskIDs []uuid.UUID) {
	if s.deployOrder == nil || s.waker == nil || len(taskIDs) == 0 {
		return
	}
	rels, err := s.deployOrder.ListDeployDependents(ctx, taskIDs)
	if err != nil {
		log.Warn().Err(err).Msg("release: listing deploy dependents failed")
		return
	}
	woken := map[uuid.UUID]bool{}
	for _, rel := range rels {
		if woken[rel.SourceTaskID] {
			continue
		}
		woken[rel.SourceTaskID] = true
		task, err := s.locateTask(ctx, rel.SourceTaskID)
		if err != nil || task.Column != domain.TaskColumnDone {
			continue
		}
		if len(s.pendingDeployDependencies(ctx, []uuid.UUID{task.ID})) > 0 {
			continue
		}
		if err := s.WakeTask(ctx, task.RepositoryID, task); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release: waking a deploy dependent failed")
		}
	}
}
