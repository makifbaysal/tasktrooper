package signals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrUnbound is a signal with no confirmed binding in the environment.
var ErrUnbound = errors.New("the signal has no confirmed binding")

func (s *Service) Signal(id string) (domain.Signal, bool) {
	sig, ok := s.signalIndex[id]
	return sig, ok
}

// ReadSignal reads the confirmed binding of one metric signal of a component
// environment over [from, to] at step, bounded by the query timeout.
// quantile is what $__quantile becomes (zero: 0.95). ErrUnbound when nothing
// is confirmed; a binding still missing a recipe param is ErrInvalidInput.
func (s *Service) ReadSignal(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, signal string, quantile float64, from, to time.Time, step time.Duration) ([]domain.Series, error) {
	sig, ok := s.signalIndex[signal]
	switch {
	case !ok:
		return nil, fmt.Errorf("%q is not a catalog signal: %w", signal, ErrInvalidInput)
	case sig.Kind != domain.SignalMetric:
		return nil, fmt.Errorf("%s is a %s signal, not a metric: %w", signal, sig.Kind, datasource.ErrUnsupported)
	}
	bindings, err := s.bindings.ListSignalBindings(ctx, componentID, env)
	if err != nil {
		return nil, err
	}
	for _, b := range bindings {
		if b.Signal != signal || b.Status != domain.SignalBindingConfirmed {
			continue
		}
		return within(ctx, s.queryTimeout, func(ctx context.Context) ([]domain.Series, error) {
			series, _, err := s.read(ctx, b, quantile, from, to, step)
			return series, err
		})
	}
	return nil, ErrUnbound
}
