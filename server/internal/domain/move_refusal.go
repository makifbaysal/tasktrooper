package domain

import "errors"

// MoveRefusedError marks a rule-based refusal of a column move that has no
// type of its own (a missing test case, a transition the board does not
// allow). Error() is the wrapped sentence unchanged.
type MoveRefusedError struct{ Err error }

func (e *MoveRefusedError) Error() string { return e.Err.Error() }
func (e *MoveRefusedError) Unwrap() error { return e.Err }

func RefuseMove(err error) error {
	if err == nil {
		return nil
	}
	return &MoveRefusedError{Err: err}
}

// IsMoveRefusal reports whether err is the workflow or one of its gates
// declining a move, as opposed to a store or infrastructure failure: only
// the former repeats identically on a retry and so is worth acting on.
func IsMoveRefusal(err error) bool {
	if err == nil {
		return false
	}
	var refused *MoveRefusedError
	var criteria *CriteriaGateError
	var workOrder *WorkOrderGateError
	var stage *StageNotOnWorkflowError
	return errors.As(err, &refused) || errors.As(err, &criteria) ||
		errors.As(err, &workOrder) || errors.As(err, &stage) ||
		errors.Is(err, ErrReviewChainIncomplete) ||
		errors.Is(err, ErrReviewStageRejected) ||
		errors.Is(err, ErrReviewStageSkipped)
}
