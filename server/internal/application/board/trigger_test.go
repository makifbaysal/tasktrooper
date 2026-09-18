package board

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The trigger message used to recite the whole todo -> in_progress ->
// code_review lifecycle regardless of where the task actually was. A task
// already sitting in in_progress was still told to "claim it, move it to
// in_progress", the planner read that as the first deliverable, and the plan
// opened with a step to move the task into the column it was already in.
func TestTriggerMessageDoesNotAskForAMoveIntoTheCurrentColumn(t *testing.T) {
	job := RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnInProgress}}

	msg := buildTriggerMessage(job, nil, nil)

	if strings.Contains(msg, "move it to in_progress") {
		t.Fatalf("in_progress task is still told to move itself to in_progress:\n%s", msg)
	}
	if !strings.Contains(msg, "ALREADY in `in_progress`") {
		t.Fatalf("trigger message does not state the task is already in_progress:\n%s", msg)
	}
}

func TestTriggerMessageKeepsTheClaimAndMoveForATodoTask(t *testing.T) {
	job := RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnTodo}}

	msg := buildTriggerMessage(job, nil, nil)

	if !strings.Contains(msg, "move it to in_progress") {
		t.Fatalf("a todo task must still be told to claim and move:\n%s", msg)
	}
}

// Whatever the column, the agent must be told that the claim/move is not a
// planning step — that is what stopped a whole subtask being spent on it.
func TestTriggerMessageRulesOutBookkeepingAsAStep(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress,
		domain.TaskColumnNeedRevision, domain.TaskColumnCodeReview,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, nil, nil)
		if !strings.Contains(msg, "never a step of its own") {
			t.Errorf("column %s: trigger message allows bookkeeping to become its own step:\n%s", col, msg)
		}
	}
}

// Nobody writes "and it should compile" on a card, so a run could satisfy every
// listed criterion and still leave the branch red or a new function untested.
// The standing three are stated even when the card lists nothing.
func TestTriggerMessageStatesStandingCriteriaForImplementers(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, nil, nil)
		for _, want := range []string{
			"Standing acceptance criteria",
			"The project builds.",
			"The whole test suite passes",
			"New or changed behaviour comes with unit tests",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("column %s: trigger message is missing %q:\n%s", col, want, msg)
			}
		}
	}
}

// An analysis produces documents: it has no build to keep green, and telling it
// to write unit tests is telling it to do the implementer's job.
func TestTriggerMessageOmitsStandingCriteriaForAnaliz(t *testing.T) {
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{
		Title: "t", Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeAnaliz,
	}}, nil, nil)
	if strings.Contains(msg, "Standing acceptance criteria") {
		t.Errorf("analiz run was handed the implementer's build criteria:\n%s", msg)
	}
}

// A run finished the work, the hand-off to code_review was refused with "4
// acceptance criteria incomplete", and the agent had never seen those criteria:
// the task snapshot carries title, description and column only. Listing the open
// ones with their ids is what makes set_criterion_completed callable.
func TestTriggerMessageListsOpenCriteriaForImplementers(t *testing.T) {
	open := []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "Android button links to the Play Store listing"},
	}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, open, nil)
		if !strings.Contains(msg, open[0].Text) {
			t.Errorf("column %s: open criterion is not in the trigger message:\n%s", col, msg)
		}
		if !strings.Contains(msg, open[0].ID.String()) {
			t.Errorf("column %s: criterion id is missing, so set_criterion_completed cannot be called:\n%s", col, msg)
		}
		if !strings.Contains(msg, "set_criterion_completed") {
			t.Errorf("column %s: implementer is not told to tick the criteria:\n%s", col, msg)
		}
	}
}

// QA and PM record their own verdict with review_criterion; the tick is the
// developer's claim and is not theirs to make.
func TestTriggerMessageDoesNotTellReviewersToTickCriteria(t *testing.T) {
	open := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "criterion"}}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA, domain.TaskColumnPMUAT,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, open, nil)
		if !strings.Contains(msg, open[0].Text) {
			t.Errorf("column %s: reviewer should still see the criteria:\n%s", col, msg)
		}
		if strings.Contains(msg, "call set_criterion_completed") {
			t.Errorf("column %s: reviewer is told to tick the developer's claim:\n%s", col, msg)
		}
	}
}

func TestTriggerMessageOmitsTheCriteriaBlockWhenNoneAreOpen(t *testing.T) {
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnInProgress}}, nil, nil)
	if strings.Contains(msg, "Open acceptance criteria") {
		t.Fatalf("a task with nothing open still gets a criteria block:\n%s", msg)
	}
}

// The architect was dispatched on an ANALIZ task and implemented it: it edited
// three files and deleted a fourth, because the snapshot carried no task_type
// and the column-only instruction told every run in `todo`/`in_progress` to
// implement the work and promised a hand-off to code_review that an analiz task
// never gets. The type has to be in the snapshot, and it has to change the
// instruction.
func TestTriggerMessageCarriesTheTaskType(t *testing.T) {
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{
		Title: "t", Column: domain.TaskColumnTodo, TaskType: domain.TaskTypeAnaliz,
	}}, nil, nil)

	if !strings.Contains(msg, `"task_type":"analiz"`) {
		t.Fatalf("task snapshot does not carry the task type:\n%s", msg)
	}
}

func TestAnalizInstructionDoesNotAskForCodeOrACodeReviewHandoff(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		instruction := columnInstruction(domain.BoardTask{Column: col, TaskType: domain.TaskTypeAnaliz})
		if strings.Contains(instruction, "the system moves the task to code_review") {
			t.Errorf("column %s: an analiz run is promised a code_review hand-off it never gets:\n%s", col, instruction)
		}
		if !strings.Contains(instruction, "no automatic hand-off to code_review") {
			t.Errorf("column %s: an analiz run is not told the code_review hand-off does not apply to it:\n%s", col, instruction)
		}
		if !strings.Contains(instruction, "add_task_document") {
			t.Errorf("column %s: an analiz run is not told its deliverable is a document:\n%s", col, instruction)
		}
		if !strings.Contains(instruction, "analiz_review") {
			t.Errorf("column %s: an analiz run is not told where the human gate is:\n%s", col, instruction)
		}
		if !strings.Contains(instruction, "Never write, edit, move or delete a file") {
			t.Errorf("column %s: an analiz run is not told to keep its hands off the repo:\n%s", col, instruction)
		}
	}
}

// analiz_review and done are human moves; the run dispatched after them has a
// different job in each, and neither is implementation.
func TestAnalizInstructionSplitsTheHumanGateFromTheApproval(t *testing.T) {
	waiting := columnInstruction(domain.BoardTask{Column: domain.TaskColumnAnalizReview, TaskType: domain.TaskTypeAnaliz})
	if !strings.Contains(waiting, "Take no action") {
		t.Errorf("analiz_review is a human gate; the agent must stand down:\n%s", waiting)
	}
	approved := columnInstruction(domain.BoardTask{Column: domain.TaskColumnDone, TaskType: domain.TaskTypeAnaliz})
	if !strings.Contains(approved, "released") || !strings.Contains(approved, "list_team") {
		t.Errorf("an approved analiz must be decomposed and released:\n%s", approved)
	}
}

// The type branch must not leak into implementation work: a task/bug keeps the
// implementer instruction exactly as it was.
func TestImplementationTasksKeepTheColumnInstruction(t *testing.T) {
	for _, typ := range []domain.TaskType{domain.TaskTypeTask, domain.TaskTypeBug, ""} {
		instruction := columnInstruction(domain.BoardTask{Column: domain.TaskColumnInProgress, TaskType: typ})
		if !strings.Contains(instruction, "code_review") {
			t.Errorf("task type %q lost the implementer hand-off:\n%s", typ, instruction)
		}
	}
}

// The implementer instruction must name the gate, not just the hand-off: a run
// that reads "the system moves it for you" and stops has left the task parked.
func TestImplementerInstructionNamesTheCriteriaGate(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		instruction := columnInstruction(domain.BoardTask{Column: col})
		if !strings.Contains(instruction, "set_criterion_completed") {
			t.Errorf("column %s: instruction does not mention ticking criteria:\n%s", col, instruction)
		}
	}
}
