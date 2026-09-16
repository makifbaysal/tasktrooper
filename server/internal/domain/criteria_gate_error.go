package domain

import "github.com/google/uuid"

// CriteriaGateReason names why a move was blocked by an acceptance-criteria
// gate: criteria left open (never ticked or cancelled), left unreviewed by
// the role whose phase is ending, or reviewed and rejected.
type CriteriaGateReason string

const (
	CriteriaGateReasonIncomplete CriteriaGateReason = "incomplete"
	CriteriaGateReasonUnchecked  CriteriaGateReason = "unchecked"
	CriteriaGateReasonRejected   CriteriaGateReason = "rejected"
)

// CriteriaGateCriterion is one blocking criterion, carried without the id so
// a human-facing rendering never has to strip one back out.
type CriteriaGateCriterion struct {
	ID   uuid.UUID
	Text string
}

// CriteriaGateError is what criteriaGate/criteriaReviewGate return when a
// move is refused for unsettled acceptance criteria.
//
// Error() stays the sentence those gates always produced — ids included,
// because the agent reading a tool refusal needs one to call
// review_criterion or set_criterion_completed. Target/Reason/Criteria are the
// same refusal broken into fields, for a caller (the HTTP layer, moves made
// by a human) that has to say the same thing without a raw id in it.
type CriteriaGateError struct {
	Target   TaskColumn
	Reason   CriteriaGateReason
	Criteria []CriteriaGateCriterion
	message  string
}

func NewCriteriaGateError(target TaskColumn, reason CriteriaGateReason, criteria []CriteriaGateCriterion, message string) *CriteriaGateError {
	return &CriteriaGateError{Target: target, Reason: reason, Criteria: criteria, message: message}
}

func (e *CriteriaGateError) Error() string { return e.message }
