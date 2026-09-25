package release

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Finish records the release engineer's (or a human's) verdict that a
// release is good and moves every one of its tasks to released.
// awaiting_verdict is open to either actor; failed only to a human
// ("ship it anyway" overrides the machine's own refusal).
func (s *Service) Finish(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, note string) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	switch {
	case r.Status == domain.ReleaseAwaitingVerdict:
	case r.Status == domain.ReleaseFailed && actor == domain.ReleaseActorHuman:
	default:
		return domain.Release{}, fmt.Errorf(
			"%w: finish_release only applies to a release awaiting a verdict, or a failed one a human confirms anyway (this one is %s, actor %s)",
			domain.ErrReleaseWrongStatus, r.Status, actor)
	}

	expect := r.Status
	now := s.now()
	r.Status = domain.ReleaseReleased
	r.Verdict = note
	r.FinishedAt = &now
	updated, err := s.store.Update(ctx, r, expect)
	if err != nil {
		return domain.Release{}, err
	}
	s.releaseTasks(ctx, updated)
	s.postAfterDeployComments(ctx, updated)
	s.wakeDeployDependents(ctx, updated)
	return updated, nil
}

// postAfterDeployComments tells whoever reads the task what to do now that
// it shipped — one comment per task that carries after-deploy steps, posted
// once the tasks have already moved to released.
func (s *Service) postAfterDeployComments(ctx context.Context, r domain.Release) {
	if s.tasks == nil {
		return
	}
	for _, t := range r.Tasks {
		text := strings.TrimSpace(t.AfterDeploy)
		if text == "" {
			continue
		}
		if _, err := s.tasks.AddComment(ctx, r.RepositoryID, t.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    fmt.Sprintf("Released — do these after-deploy steps now: %s", text),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: posting an after-deploy comment failed")
		}
	}
}

// releaseTasks moves every task to released, but only the ones still sitting
// where the release left them (L4): a task already released is a no-op, and
// a task a human moved elsewhere (need_revision, blocked) is left alone — the
// release's verdict is not license to override that move.
func (s *Service) releaseTasks(ctx context.Context, r domain.Release) {
	if s.tasks == nil {
		return
	}
	for _, t := range r.Tasks {
		if s.parked != nil {
			if _, _, err := s.parked.TakeBlockedResourceTask(ctx, domain.ResourceReleaseWatch, t.ID); err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: claiming a parked card before releasing it failed")
			}
		}
		current, err := s.tasks.GetTask(ctx, r.RepositoryID, t.ID)
		if err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: reading a task before releasing it failed")
			continue
		}
		if current.Column == domain.TaskColumnReleased {
			continue
		}
		if current.Column != domain.TaskColumnDone {
			continue
		}
		col := domain.TaskColumnReleased
		if _, err := s.tasks.UpdateTask(ctx, r.RepositoryID, t.ID, domain.UpdateBoardTaskRequest{
			Column:       &col,
			SystemReason: domain.MoveReasonReleaseVerified,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: moving a finished release's task to released failed")
		}
	}
}
