package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// RuntimeReader is one environment's runtime reads (cloud.Service): the seam
// the cross-component Operations queries fan out over, each call keeping the
// per-environment cache and error classification it already has.
type RuntimeReader interface {
	Errors(ctx context.Context, envID uuid.UUID, since time.Time) ([]domain.RuntimeErrorGroup, error)
	Logs(ctx context.Context, envID uuid.UUID, q domain.RuntimeLogQuery) (domain.RuntimeLogPage, error)
	// ErrorsSupported makes no call; false means the environment has nothing
	// to read errors from, which is not a failure.
	ErrorsSupported(env domain.ComponentEnvironment) bool
}
