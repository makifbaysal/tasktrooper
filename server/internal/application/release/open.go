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
		return s.openBatch(ctx, repositoryID, task, component, name, profile)
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

// openRelease opens a release for one freshly-merged task.
func (s *Service) openRelease(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, component domain.Component, profile domain.ComponentDelivery, mergeSHA string) domain.ReleaseOpening {
	return s.openReleaseForTasks(ctx, repositoryID, task, nil, component, profile, mergeSHA)
}

// openReleaseForTasks opens (or takes over) a release carrying `primary` plus
// any `extraTaskIDs` — OpenPending's other waiting tasks of the same
// component, collected so one delivery-confirmation catch-up never opens more
// than one release. The release row is created BEFORE the component's
// currently open release (if any) is marked superseded: if Create itself
// fails, nothing about the old release changed and its tasks are exactly
// where they were — superseding first and then failing to create would have
// orphaned them.
func (s *Service) openReleaseForTasks(ctx context.Context, repositoryID uuid.UUID, primary domain.BoardTask, extraTaskIDs []uuid.UUID, component domain.Component, profile domain.ComponentDelivery, mergeSHA string) domain.ReleaseOpening {
	componentID := component.ID
	version := domain.ShortSHA(mergeSHA)

	open, hasOpen := s.openReleaseToSupersede(ctx, repositoryID, componentID, mergeSHA)

	// carried (older) tasks first, the primary task last, each getting a
	// strictly later added_at than the one before it (postgres Create uses
	// clock_timestamp() for that) — revert order and "the newest task" both
	// depend on this.
	seen := map[uuid.UUID]bool{primary.ID: true}
	taskIDs := make([]uuid.UUID, 0, len(extraTaskIDs)+2)
	if hasOpen {
		for _, id := range open.TaskIDs() {
			if !seen[id] {
				seen[id] = true
				taskIDs = append(taskIDs, id)
			}
		}
	}
	for _, id := range extraTaskIDs {
		if !seen[id] {
			seen[id] = true
			taskIDs = append(taskIDs, id)
		}
	}
	taskIDs = append(taskIDs, primary.ID)

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
		log.Warn().Err(err).Str("task_id", primary.ID.String()).Str("component_id", componentID.String()).
			Msg("release: opening a release at merge failed")
		return domain.ReleaseOpening{Mode: profile.Mode, Next: fmt.Sprintf("could not open a release: %s", err)}
	}

	if hasOpen {
		s.supersedeRelease(ctx, open, created.ID, version)
	}

	next := "call watch_release"
	if profile.Mode == domain.DeliveryDispatch {
		next = "call deploy_release, then watch_release"
	}
	id := created.ID
	return domain.ReleaseOpening{Mode: profile.Mode, ReleaseID: &id, Status: created.Status, Next: next}
}

// openReleaseToSupersede finds the component's currently open release and
// reports whether it should be taken over: never when the new merge's commit
// is not a git descendant of it — chaining onto an unrelated or older
// commit would misreport what shipped, and the older release would be judged
// for a deploy it never carried. Git == nil, or the repository's root path
// cannot be resolved, allows the take-over exactly as before the check
// existed — the ancestry check is a safety net, not a hard requirement.
func (s *Service) openReleaseToSupersede(ctx context.Context, repositoryID, componentID uuid.UUID, mergeSHA string) (domain.Release, bool) {
	open, err := s.findOpenRelease(ctx, repositoryID, componentID)
	if err != nil {
		if !errors.Is(err, domain.ErrReleaseNotFound) {
			log.Warn().Err(err).Str("component_id", componentID.String()).Msg("release: finding the open release to supersede failed")
		}
		return domain.Release{}, false
	}
	if s.git == nil || open.CommitSHA == "" || mergeSHA == "" {
		return open, true
	}
	rootPath, ok := s.repoRootPath(ctx, repositoryID)
	if !ok {
		return open, true
	}
	isDescendant, err := s.git.IsAncestor(ctx, rootPath, open.CommitSHA, mergeSHA)
	if err != nil {
		log.Warn().Err(err).Str("release_id", open.ID.String()).Msg("release: checking whether the merge descends from the open release failed")
		return open, true
	}
	if !isDescendant {
		log.Warn().Str("release_id", open.ID.String()).Str("open_commit", open.CommitSHA).Str("merge_sha", mergeSHA).
			Msg("release: not superseding an open release whose commit is not an ancestor of the new merge")
		return domain.Release{}, false
	}
	return open, true
}

func (s *Service) repoRootPath(ctx context.Context, repositoryID uuid.UUID) (string, bool) {
	if s.repos == nil {
		return "", false
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("release: resolving the repository root path failed")
		return "", false
	}
	return repo.RootPath, true
}

// supersedeRelease marks `open` superseded now that `newReleaseID` already
// carries its tasks (L5: Create ran first). A conditional Update losing the
// race means something else already moved `open` on (another supersede, a
// sweep settling it) — re-read what it carries now and fold those tasks into
// the new release too, so a task added to it in that window is not left
// behind, without touching whatever `open`'s own status raced to.
func (s *Service) supersedeRelease(ctx context.Context, open domain.Release, newReleaseID uuid.UUID, newVersion string) {
	expect := open.Status
	open.Status = domain.ReleaseSuperseded
	open.Verdict = "superseded by " + newVersion
	updated, err := s.store.Update(ctx, open, expect)
	if err != nil {
		if errors.Is(err, domain.ErrReleaseWrongStatus) {
			if latest, gerr := s.store.Get(ctx, open.ID); gerr == nil {
				if aerr := s.store.AddTasks(ctx, newReleaseID, latest.TaskIDs()); aerr != nil {
					log.Warn().Err(aerr).Str("release_id", open.ID.String()).Msg("release: carrying a raced-superseded release's tasks forward failed")
				}
			} else {
				log.Warn().Err(gerr).Str("release_id", open.ID.String()).Msg("release: re-reading a raced-superseded release failed")
			}
		} else {
			log.Warn().Err(err).Str("release_id", open.ID.String()).Msg("release: superseding the open release failed")
		}
		return
	}
	if s.parked != nil {
		for _, t := range updated.Tasks {
			if _, _, err := s.parked.TakeBlockedResourceTask(ctx, domain.ResourceReleaseWatch, t.ID); err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: un-parking a superseded release's card failed")
			}
		}
	}
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

// OpenPending is called when a component's delivery override is saved. It:
//   - wakes the component's done tasks that never merged — MergeGate
//     refuses their merge while the delivery profile is unconfirmed, and
//     nothing else would retry it for them once it is;
//   - opens ONE release covering every already-merged, still-unreleased done
//     task of the component at once, at the remote default-branch head —
//     opening one release per task in board order could ship an older commit
//     after a newer one, and would wake the agent once per task instead of
//     once.
func (s *Service) OpenPending(ctx context.Context, repositoryID, componentID uuid.UUID) (int, error) {
	if s.tasks == nil || s.store == nil {
		return 0, nil
	}
	component, err := s.componentByID(ctx, componentID)
	if err != nil {
		return 0, err
	}
	profile, confirmed := domain.DeliveryConfirmed(component.Delivery)
	if !confirmed {
		return 0, nil
	}

	all, err := s.tasks.ListTasks(ctx, repositoryID)
	if err != nil {
		return 0, err
	}

	var waiting []domain.BoardTask
	for _, task := range all {
		if task.Column != domain.TaskColumnDone {
			continue
		}
		if owner, _, ok := s.resolveComponent(ctx, repositoryID, task); !ok || owner.ID != componentID {
			continue
		}
		sha := strings.TrimSpace(task.MergeCommitSHA)
		if sha == "" {
			if err := s.WakeTask(ctx, repositoryID, task); err != nil {
				log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release: waking an unmerged task after a delivery confirmation failed")
			}
			continue
		}
		if !s.taskNeedsARelease(ctx, task, profile) {
			continue
		}
		waiting = append(waiting, task)
	}
	if len(waiting) == 0 {
		return 0, nil
	}

	switch profile.Mode {
	case domain.DeliveryNone:
		opened := 0
		for _, task := range waiting {
			if opening := s.openNone(ctx, repositoryID, task); opening.Released {
				opened++
			}
		}
		return opened, nil
	case domain.DeliveryBatch:
		opened := 0
		for _, task := range waiting {
			opening := s.openBatch(ctx, repositoryID, task, component, component.DisplayName(), profile)
			if opening.ReleaseID != nil {
				opened++
			}
		}
		return opened, nil
	}

	mergeSHA := s.remoteHeadOrNewestMerge(ctx, repositoryID, waiting)
	primary := waiting[len(waiting)-1]
	extra := make([]uuid.UUID, 0, len(waiting)-1)
	for _, t := range waiting[:len(waiting)-1] {
		extra = append(extra, t.ID)
	}
	opening := s.openReleaseForTasks(ctx, repositoryID, primary, extra, component, profile, mergeSHA)
	if opening.ReleaseID == nil {
		return 0, nil
	}
	// No agent run is alive for tasks merged long ago: a dispatch release
	// would sit in pending forever without a wake.
	if opening.Status == domain.ReleasePending && s.waker != nil {
		if err := s.waker.Wake(ctx, repositoryID, primary, domain.ReleasePending); err != nil {
			log.Warn().Err(err).Str("task_id", primary.ID.String()).Msg("release: waking the release engineer for a pending release failed")
		}
	}
	return len(waiting), nil
}

// taskNeedsARelease reports a done, merged task with no current release. M8:
// a task whose only release is a draft left behind by its component leaving
// batch mode is pulled out of that draft (RemoveTask) and treated as
// unreleased; a draft emptied that way is marked superseded.
func (s *Service) taskNeedsARelease(ctx context.Context, task domain.BoardTask, profile domain.ComponentDelivery) bool {
	existing, err := s.store.ForTask(ctx, task.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrReleaseNotFound) {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release: checking for an existing release during OpenPending failed")
			return false
		}
		return true
	}
	if existing.Status != domain.ReleaseDraft || profile.Mode == domain.DeliveryBatch {
		return false
	}
	if err := s.store.RemoveTask(ctx, existing.ID, task.ID); err != nil {
		log.Warn().Err(err).Str("release_id", existing.ID.String()).Str("task_id", task.ID.String()).
			Msg("release: pulling a stranded draft task out failed")
		return false
	}
	s.supersedeIfEmptyDraft(ctx, existing.ID)
	return true
}

func (s *Service) supersedeIfEmptyDraft(ctx context.Context, releaseID uuid.UUID) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		log.Warn().Err(err).Str("release_id", releaseID.String()).Msg("release: re-reading a draft after pulling a stranded task failed")
		return
	}
	if len(r.Tasks) > 0 || r.Status != domain.ReleaseDraft {
		return
	}
	expect := r.Status
	r.Status = domain.ReleaseSuperseded
	r.Verdict = "emptied: its component left batch mode"
	if _, err := s.store.Update(ctx, r, expect); err != nil && !errors.Is(err, domain.ErrReleaseWrongStatus) {
		log.Warn().Err(err).Str("release_id", releaseID.String()).Msg("release: superseding an emptied draft failed")
	}
}

func (s *Service) remoteHeadOrNewestMerge(ctx context.Context, repositoryID uuid.UUID, waiting []domain.BoardTask) string {
	if s.git != nil {
		if rootPath, ok := s.repoRootPath(ctx, repositoryID); ok {
			if head, err := s.git.RemoteHead(ctx, rootPath); err == nil {
				if head = strings.TrimSpace(head); head != "" {
					return head
				}
			} else {
				log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("release: reading the default branch's remote head failed")
			}
		}
	}
	return strings.TrimSpace(waiting[len(waiting)-1].MergeCommitSHA)
}

func (s *Service) componentByID(ctx context.Context, componentID uuid.UUID) (domain.Component, error) {
	if s.components == nil {
		return domain.Component{}, fmt.Errorf("no component resolver is configured")
	}
	return s.components.GetComponent(ctx, componentID)
}
