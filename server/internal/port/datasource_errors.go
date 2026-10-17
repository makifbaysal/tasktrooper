package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrorsQuerier is a kind that lists error groups (Sentry issues), for the
// errors inbox. Secrets are passed as for MetricsQuerier.Series; no group is
// not an error.
type ErrorsQuerier interface {
	Errors(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.ErrorsQuery) ([]domain.RuntimeErrorGroup, error)
}

// BoundErrorReader reads component error groups through their confirmed
// errors.issues bindings, for the errors inbox.
type BoundErrorReader interface {
	// ErrorBindings answers the confirmed errors.issues binding of every
	// component of env that is not dismissed, on a source whose kind lists
	// error groups.
	ErrorBindings(ctx context.Context, env domain.DeployEnvironment) ([]domain.SignalBindingView, error)
	// ReadErrors renders the binding's query and reads q through it; q.Query
	// is ignored.
	ReadErrors(ctx context.Context, b domain.SignalBinding, q domain.ErrorsQuery) ([]domain.RuntimeErrorGroup, error)
}
