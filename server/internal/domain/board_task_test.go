package domain

import "testing"

func TestOnlyAnAnalysisKeepsItsBranchLocal(t *testing.T) {
	for _, tt := range []TaskType{TaskTypeTask, TaskTypeBug, TaskTypeTechnical} {
		if !tt.PublishesBranch() {
			t.Errorf("%s must publish its branch", tt)
		}
	}
	if TaskTypeAnaliz.PublishesBranch() {
		t.Error("an analysis must never publish its branch")
	}
}

func TestTheGeneratedOrderNoteIsNotABeforeDeployStep(t *testing.T) {
	note := OrderNoteOpen + "\n- Ships after: T-1 (API).\n" + OrderNoteClose
	withSteps := ApplyOrderNote("Run the backfill", note)

	onlyNote := BoardTask{BeforeDeploy: &note}
	if onlyNote.BeforeDeployPending() {
		t.Error("an order note alone must not wait for a human")
	}
	if (ReleaseTaskRef{BeforeDeploy: note}).BeforeDeployPending() {
		t.Error("a release's order note alone must not wait for a human")
	}

	stepped := BoardTask{BeforeDeploy: &withSteps}
	if !stepped.BeforeDeployPending() {
		t.Error("human steps beside the order note must still wait for a confirmation")
	}
	if got := stepped.BeforeDeploySteps(); got != "Run the backfill" {
		t.Errorf("BeforeDeploySteps() = %q, want the human steps only", got)
	}
	if !(ReleaseTaskRef{BeforeDeploy: withSteps}).BeforeDeployPending() {
		t.Error("a release's human steps must still wait for a confirmation")
	}
}
