package release

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Watch reports a release the sweeper is still advancing as a ResourceBlock
// so the caller's tool result parks the card exactly the way
// get_task_deploy_status parks it on deploy_watch today.
func (s *Service) Watch(ctx context.Context, releaseID uuid.UUID) (domain.Release, *domain.ResourceBlock, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, nil, err
	}
	if !r.Status.Watched() {
		return r, nil, nil
	}
	// Re-read right before reporting the park: the sweeper can settle a
	// release between the read above and here, and parking it after that
	// would strand the card until the watchdog's re-wake caught it.
	fresh, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, nil, err
	}
	if !fresh.Status.Watched() {
		return fresh, nil, nil
	}
	return fresh, &domain.ResourceBlock{
		Resource: domain.ResourceReleaseWatch,
		Detail:   fmt.Sprintf("release %s is %s — the sweeper is watching it and will wake this task when it needs a verdict or has failed", fresh.Version, fresh.Status),
	}, nil
}
