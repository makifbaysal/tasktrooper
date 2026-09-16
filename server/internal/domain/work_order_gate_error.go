package domain

// WorkOrderGateBlocker is one unfinished task standing in front of a move,
// carried without the raw relation row so a human-facing rendering never has
// to reach back into task_relations for a label.
type WorkOrderGateBlocker struct {
	Key   string
	Title string
}

// WorkOrderGateError is what validateMoveAllowed returns when a manual move
// into todo or in_progress is refused because the task still has an open
// `blocks` relation. Shaped like CriteriaGateError on purpose: same fields
// broken out for callers that need them (the HTTP layer), same Error()
// sentence for callers that only read English.
type WorkOrderGateError struct {
	Target   TaskColumn
	Blockers []WorkOrderGateBlocker
	message  string
}

func NewWorkOrderGateError(target TaskColumn, blockers []WorkOrderGateBlocker, message string) *WorkOrderGateError {
	return &WorkOrderGateError{Target: target, Blockers: blockers, message: message}
}

func (e *WorkOrderGateError) Error() string { return e.message }
