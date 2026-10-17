package port

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrSignalBindingConflict is a write that lost to another one on the same
// (component, environment, signal).
var ErrSignalBindingConflict = errors.New("the signal binding was changed by another request")

// SignalBindingStore persists where each signal of a component environment
// is read (migration 188). Writes to one (component, environment, signal)
// slot are serialized, so two confirms or puts of a slot never interleave.
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
	// DismissSignalBinding dismisses a binding only while it is suggested;
	// any other row is answered as it stands, so the caller can tell a
	// confirmed one apart. A missing one is ErrNotFound.
	DismissSignalBinding(ctx context.Context, id uuid.UUID) (domain.SignalBinding, error)
	// ReplaceDetectedSuggestions swaps a component environment's pending
	// detected suggestions for these in one transaction. A pending row with
	// the signal and target of one of them is kept, id and all; any other
	// pending detected row is deleted only when its data source is in
	// sources, the ones the detector heard back from. Confirmed, dismissed and
	// human or AI rows are left alone. Answers the pending detected rows after.
	ReplaceDetectedSuggestions(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, sources []uuid.UUID, suggestions []domain.SignalBinding) ([]domain.SignalBinding, error)
	// AddSignalBindingSuggestion stores b as a pending suggestion of b.Source
	// under its slot's lock. A row of that slot with the same target, in any
	// status, is answered as it stands instead: a dismissed target is never
	// offered again, and a repeat proposal adds nothing.
	AddSignalBindingSuggestion(ctx context.Context, b domain.SignalBinding) (domain.SignalBinding, error)
	DeleteSignalBinding(ctx context.Context, id uuid.UUID) error
}
