package board

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestColumnInstructionReadyForQAEntersInQAFirst(t *testing.T) {
	got := columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnReadyForQA})

	if !strings.Contains(got, "in_qa") {
		t.Fatalf("ready_for_qa instruction must send the task into in_qa before testing, got: %s", got)
	}
	for _, want := range []string{"human_uat", "need_revision"} {
		if !strings.Contains(got, want) {
			t.Errorf("ready_for_qa instruction must state the %s exit, got: %s", want, got)
		}
	}
}

func TestColumnInstructionInQADoesNotRePlanTheMove(t *testing.T) {
	got := columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnInQA})

	if !strings.Contains(got, "ALREADY in `in_qa`") {
		t.Fatalf("in_qa instruction must state the task is already there, got: %s", got)
	}
	for _, want := range []string{"human_uat", "need_revision"} {
		if !strings.Contains(got, want) {
			t.Errorf("in_qa instruction must state the %s exit, got: %s", want, got)
		}
	}
}
