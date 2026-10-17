package signals

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Propose stores an AI suggestion for one signal: checked like a person's
// binding, but left suggested for a person to confirm or dismiss. A target
// already offered for the slot, or dismissed there, is answered as it stands.
func (s *Service) Propose(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, signal string, req BindingRequest) (domain.SignalBindingView, error) {
	if !env.Valid() {
		return domain.SignalBindingView{}, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	if _, err := s.components.GetComponent(ctx, componentID); err != nil {
		return domain.SignalBindingView{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	b, err := s.validateTarget(ctx, l, signal, req)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	b.ComponentID, b.Environment, b.Source = componentID, env, domain.SignalBindingAI
	saved, err := s.bindings.AddSignalBindingSuggestion(ctx, b)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	return s.view(ctx, l, saved), nil
}
