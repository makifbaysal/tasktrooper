package release

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// openStatuses are the statuses a newer merge of the same component must
// take over rather than run beside — domain.ReleaseStatus.Open(), spelled
// out because domain has no "all open statuses" slice to range over.
var openStatuses = []domain.ReleaseStatus{
	domain.ReleasePending, domain.ReleaseDeploying, domain.ReleaseVerifying, domain.ReleaseAwaitingVerdict,
}

// OpenForMerge is called by board.TaskPRService right after a successful
// merge. It never returns an error: a failure to open a release is logged
// and reported in Next ("could not open a release: ...") with the task left
// in done, because the merge itself already happened and cannot be undone
// here.
func (s *Service) OpenForMerge(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, mergeSHA string) domain.ReleaseOpening {
	mergeSHA = strings.TrimSpace(mergeSHA)

	component, name, ok := s.resolveComponent(ctx, repositoryID, task)
	if !ok {
		next := "This task has no component recorded, so nothing knows how it ships. " +
			"The task stays in done until a human assigns it a component and confirms that component's delivery profile on the Deploy tab."
		s.commentOnce(ctx, repositoryID, task.ID, next)
		return domain.ReleaseOpening{Unconfirmed: true, Next: next}
	}

	profile, confirmed := domain.DeliveryConfirmed(component.Delivery)
	if !confirmed {
		mode := domain.DeliveryMode("")
		if component.Delivery.Detected != nil {
			mode = component.Delivery.Detected.Mode
		}
		next := fmt.Sprintf("The delivery profile of %s is not confirmed, so nothing deploys it. "+
			"The task stays in done until a human confirms it on the Deploy tab.", name)
		s.commentOnce(ctx, repositoryID, task.ID, next)
		return domain.ReleaseOpening{Mode: mode, Unconfirmed: true, Next: next}
	}

	switch profile.Mode {
	case domain.DeliveryNone:
		return s.openNone(ctx, repositoryID, task)
	case domain.DeliveryBatch:
		return domain.ReleaseOpening{
			Mode: domain.DeliveryBatch,
			Next: "This component batches releases; a human cuts the next one. " +
				"Release cutting arrives in a later version — the task stays in done until then.",
		}
	case domain.DeliveryOnMerge, domain.DeliveryDispatch:
		return s.openRelease(ctx, repositoryID, task, component, profile, mergeSHA)
	default:
		next := fmt.Sprintf("%s has an unrecognised delivery mode %q, so it is treated as unconfirmed. "+
			"A human must fix the delivery profile on the Deploy tab.", name, profile.Mode)
		s.commentOnce(ctx, repositoryID, task.ID, next)
		return domain.ReleaseOpening{Unconfirmed: true, Next: next}
	}
}

func (s *Service) openNone(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) domain.ReleaseOpening {
	if s.tasks != nil {
		col := domain.TaskColumnReleased
		if _, err := s.tasks.UpdateTask(ctx, repositoryID, task.ID, domain.UpdateBoardTaskRequest{
			Column:       &col,
			SystemReason: domain.MoveReasonMergeReleasedNoDelivery,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release: moving a none-delivery task to released failed")
			return domain.ReleaseOpening{
				Mode: domain.DeliveryNone,
				Next: fmt.Sprintf("could not open a release: moving the task to released failed: %s", err),
			}
		}
	}
	return domain.ReleaseOpening{
		Mode:     domain.DeliveryNone,
		Released: true,
		Next:     "Nothing deploys this component; the merge itself was the release.",
	}
}

func (s *Service) openRelease(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, component domain.Component, profile domain.ComponentDelivery, mergeSHA string) domain.ReleaseOpening {
	componentID := component.ID
	version := domain.ShortSHA(mergeSHA)

	carried := s.supersedeOpenRelease(ctx, repositoryID, componentID, version)

	taskIDs := []uuid.UUID{task.ID}
	for _, id := range carried {
		if id != task.ID {
			taskIDs = append(taskIDs, id)
		}
	}

	now := s.now()
	r := domain.Release{
		RepositoryID: repositoryID,
		ComponentID:  &componentID,
		Version:      version,
		Mode:         profile.Mode,
		Executor:     profile.Executor,
		CommitSHA:    mergeSHA,
		Profile:      profile,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	switch profile.Mode {
	case domain.DeliveryOnMerge:
		r.Status = domain.ReleaseDeploying
		r.DeployStartedAt = &now
	case domain.DeliveryDispatch:
		r.Status = domain.ReleasePending
	}

	created, err := s.store.Create(ctx, r, taskIDs)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("component_id", componentID.String()).
			Msg("release: opening a release at merge failed")
		return domain.ReleaseOpening{Mode: profile.Mode, Next: fmt.Sprintf("could not open a release: %s", err)}
	}

	next := "call watch_release"
	if profile.Mode == domain.DeliveryDispatch {
		next = "call deploy_release, then watch_release"
	}
	id := created.ID
	return domain.ReleaseOpening{Mode: profile.Mode, ReleaseID: &id, Status: created.Status, Next: next}
}

// supersedeOpenRelease marks the component's current open release (if any)
// superseded and returns the task ids it carried, un-parking any card of it
// waiting on release_watch WITHOUT waking it — the new release's card is the
// one that gets woken.
func (s *Service) supersedeOpenRelease(ctx context.Context, repositoryID, componentID uuid.UUID, newVersion string) []uuid.UUID {
	open, err := s.findOpenRelease(ctx, repositoryID, componentID)
	if err != nil {
		if !errors.Is(err, domain.ErrReleaseNotFound) {
			log.Warn().Err(err).Str("component_id", componentID.String()).Msg("release: finding the open release to supersede failed")
		}
		return nil
	}

	expect := open.Status
	open.Status = domain.ReleaseSuperseded
	open.Verdict = "superseded by " + newVersion
	updated, err := s.store.Update(ctx, open, expect)
	if err != nil {
		if !errors.Is(err, domain.ErrReleaseWrongStatus) {
			log.Warn().Err(err).Str("release_id", open.ID.String()).Msg("release: superseding the open release failed")
		}
		return nil
	}

	if s.parked != nil {
		for _, t := range updated.Tasks {
			if _, _, err := s.parked.TakeBlockedResourceTask(ctx, domain.ResourceReleaseWatch, t.ID); err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: un-parking a superseded release's card failed")
			}
		}
	}
	return updated.TaskIDs()
}

func (s *Service) findOpenRelease(ctx context.Context, repositoryID, componentID uuid.UUID) (domain.Release, error) {
	if s.store == nil {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		RepositoryID: &repositoryID,
		ComponentID:  &componentID,
		Statuses:     openStatuses,
		Limit:        1,
	})
	if err != nil {
		return domain.Release{}, err
	}
	if len(releases) == 0 {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	return releases[0], nil
}

// resolveComponent implements §3.1 step 1: the task's own component, else
// the repository's only active component, else the component at path ".",
// else none.
func (s *Service) resolveComponent(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) (domain.Component, string, bool) {
	if s.components == nil {
		return domain.Component{}, "", false
	}
	if task.ComponentID != nil {
		c, err := s.components.GetComponent(ctx, *task.ComponentID)
		if err == nil {
			return c, c.DisplayName(), true
		}
		log.Warn().Err(err).Str("component_id", task.ComponentID.String()).Msg("release: resolving the task's component failed")
	}
	if list, err := s.components.ListComponents(ctx, repositoryID); err == nil {
		if active := activeComponents(list); len(active) == 1 {
			return active[0], active[0].DisplayName(), true
		}
	} else {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("release: listing components for merge resolution failed")
	}
	if c, err := s.components.ComponentByPath(ctx, repositoryID, "."); err == nil {
		return c, c.DisplayName(), true
	}
	return domain.Component{}, "", false
}

func activeComponents(all []domain.Component) []domain.Component {
	out := make([]domain.Component, 0, len(all))
	for _, c := range all {
		if c.Status == domain.ComponentStatusActive {
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) commentOnce(ctx context.Context, repositoryID, taskID uuid.UUID, content string) {
	if s.tasks == nil {
		return
	}
	if _, err := s.tasks.AddComment(ctx, repositoryID, taskID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    content,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("release: posting the unconfirmed-delivery comment failed")
	}
}

// OpenPending is called when a component's delivery override is saved: it
// opens releases for tasks of that component that sit in done, are merged,
// and belong to no release yet.
func (s *Service) OpenPending(ctx context.Context, repositoryID, componentID uuid.UUID) (int, error) {
	if s.tasks == nil || s.store == nil {
		return 0, nil
	}
	component, err := s.componentByID(ctx, componentID)
	if err != nil {
		return 0, err
	}
	profile, confirmed := domain.DeliveryConfirmed(component.Delivery)
	if !confirmed || (profile.Mode != domain.DeliveryOnMerge && profile.Mode != domain.DeliveryDispatch) {
		return 0, nil
	}

	all, err := s.tasks.ListTasks(ctx, repositoryID)
	if err != nil {
		return 0, err
	}

	opened := 0
	for _, task := range all {
		if task.Column != domain.TaskColumnDone {
			continue
		}
		if task.ComponentID == nil || *task.ComponentID != componentID {
			continue
		}
		sha := strings.TrimSpace(task.MergeCommitSHA)
		if sha == "" {
			continue
		}
		if _, err := s.store.ForTask(ctx, task.ID); err == nil {
			continue // already belongs to a release
		} else if !errors.Is(err, domain.ErrReleaseNotFound) {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release: checking for an existing release during OpenPending failed")
			continue
		}
		opening := s.openRelease(ctx, repositoryID, task, component, profile, sha)
		if opening.ReleaseID != nil {
			opened++
		}
	}
	return opened, nil
}

func (s *Service) componentByID(ctx context.Context, componentID uuid.UUID) (domain.Component, error) {
	if s.components == nil {
		return domain.Component{}, fmt.Errorf("no component resolver is configured")
	}
	return s.components.GetComponent(ctx, componentID)
}
