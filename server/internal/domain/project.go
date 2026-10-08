package domain

type TaskColumn string

const (
	TaskColumnBacklog      TaskColumn = "backlog"
	TaskColumnTodo         TaskColumn = "todo"
	TaskColumnInProgress   TaskColumn = "in_progress"
	TaskColumnAnalizReview TaskColumn = "analiz_review"
	TaskColumnCodeReview   TaskColumn = "code_review"
	TaskColumnReadyForQA   TaskColumn = "ready_for_qa"
	TaskColumnInQA         TaskColumn = "in_qa"
	TaskColumnNeedRevision TaskColumn = "need_revision"
	TaskColumnPMUAT        TaskColumn = "pm_uat"
	TaskColumnHumanUAT     TaskColumn = "human_uat"
	// TaskColumnBlocked parks a task whose agent asked a question and cannot
	// proceed. It is a real column rather than a flag so blocked work drops out
	// of the in-progress KPIs instead of inflating them: a task nobody can move
	// is not work in flight. The column the task came from is kept on the task
	// so answering returns it to exactly where it stopped.
	TaskColumnBlocked  TaskColumn = "blocked"
	TaskColumnDone     TaskColumn = "done"
	TaskColumnReleased TaskColumn = "released"
)

var BoardColumns = []TaskColumn{
	TaskColumnTodo,
	TaskColumnInProgress,
	TaskColumnAnalizReview,
	TaskColumnCodeReview,
	TaskColumnReadyForQA,
	TaskColumnInQA,
	TaskColumnNeedRevision,
	TaskColumnPMUAT,
	TaskColumnHumanUAT,
	TaskColumnBlocked,
	TaskColumnDone,
	TaskColumnReleased,
}

func ValidTaskColumn(c TaskColumn) bool {
	switch c {
	case TaskColumnBacklog, TaskColumnTodo, TaskColumnInProgress, TaskColumnAnalizReview,
		TaskColumnCodeReview, TaskColumnReadyForQA, TaskColumnInQA, TaskColumnNeedRevision,
		TaskColumnPMUAT, TaskColumnHumanUAT, TaskColumnBlocked, TaskColumnDone, TaskColumnReleased:
		return true
	default:
		return false
	}
}

// AssigneeOnlyColumn reports whether col's work goes to the task's assignee
// alone. Every developer watches these queues, so an unassigned card there
// waits for an assignee rather than waking each of them.
func AssigneeOnlyColumn(col TaskColumn) bool {
	return col == TaskColumnTodo || col == TaskColumnNeedRevision
}
