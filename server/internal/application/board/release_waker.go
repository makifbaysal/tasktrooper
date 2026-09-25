package board

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ReleaseWaker adapts Dispatcher to application/release's Waker interface.
// release cannot import board (board already needs to import release for
// ReleaseOpener in taskpr_merge.go), so the direction is inverted here: the
// integrator hands *ReleaseWaker to release.Deps.Waker instead of release
// depending on board directly.
type ReleaseWaker struct {
	dispatcher *Dispatcher
}

func NewReleaseWaker(dispatcher *Dispatcher) *ReleaseWaker {
	return &ReleaseWaker{dispatcher: dispatcher}
}

// Wake dispatches the release engineer's resume exactly the way
// deploy_sweeper.go wakes a deploy-watch card, keyed on release_watch
// instead so dispatcher.go's deployWatchWake carve-out fires.
func (w *ReleaseWaker) Wake(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, releaseStatus domain.ReleaseStatus) error {
	if w == nil || w.dispatcher == nil {
		return nil
	}
	return w.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: repositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"resumed":                          "release_settled",
			"resource":                         domain.ResourceReleaseWatch,
			"release_status":                   string(releaseStatus),
			domain.EventPayloadResumedResource: domain.ResourceReleaseWatch,
			domain.EventPayloadActor:           domain.EventActorSystem,
			domain.EventPayloadReason:          domain.MoveReasonResourceFree,
		},
	})
}
