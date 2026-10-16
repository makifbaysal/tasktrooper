package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SignalBindingStore persists where each signal of a component environment
// is read (migration 188).
type SignalBindingStore interface {
	// ListSignalBindings answers every status, oldest first.
	ListSignalBindings(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) ([]domain.SignalBinding, error)
	ListConfirmedSignalBindings(ctx context.Context, env domain.DeployEnvironment) ([]domain.SignalBinding, error)
	GetSignalBinding(ctx context.Context, id uuid.UUID) (domain.SignalBinding, error)
	// PutConfirmedSignalBinding makes b the confirmed binding of its
	// (component, environment, signal), replacing the one there in place (its
	// id and created_at are kept), and drops that slot's pending suggestions.
	PutConfirmedSignalBinding(ctx context.Context, b domain.SignalBinding) (domain.SignalBinding, error)
	// ConfirmSignalBinding promotes a suggestion (or a dismissed one) in one
	// transaction: the slot's other confirmed binding and its other pending
	// suggestions are deleted. Confirming the confirmed binding is a no-op.
	ConfirmSignalBinding(ctx context.Context, id uuid.UUID) (domain.SignalBinding, error)
	SetSignalBindingStatus(ctx context.Context, id uuid.UUID, status domain.SignalBindingStatus) (domain.SignalBinding, error)
	// ReplaceDetectedSuggestions swaps a component environment's pending
	// detected suggestions for these in one transaction; confirmed, dismissed
	// and human or AI rows are left alone.
	ReplaceDetectedSuggestions(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, suggestions []domain.SignalBinding) ([]domain.SignalBinding, error)
	DeleteSignalBinding(ctx context.Context, id uuid.UUID) error
}
