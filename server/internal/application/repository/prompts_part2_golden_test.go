package repository

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// TestGolden_RepositoryGuardWordingPart2 pins the exact byte output of the
// guard-rejection strings the whole-server sweep (part 2 of the prompt
// migration) found still living as Go string literals in service.go: they
// reach the agent as a tool-call error whenever create_task/cancel_criterion/
// review_criterion/move_board_task/write_document is rejected, so they are
// model-facing exactly like any other guard.
func TestGolden_RepositoryGuardWordingPart2(t *testing.T) {
	assert := func(got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	assert(prompt.Text(criterionCancelReasonRequiredKey),
		"cancelling a criterion requires a reason: say why it is not being done (out of scope, superseded, impossible as written)")

	assert(prompt.Text(criterionRejectNoteRequiredKey),
		"a rejected criterion needs a note explaining what failed and how it was observed")

	assert(prompt.Text(documentTooLargeHintKey),
		"keep the report scannable: summarize, link to code instead of pasting it")

	assert(prompt.Text(noRepositoriesKey),
		"no repositories; add a repository before creating board tasks")

	assert(prompt.Text(relationTargetRequiredKey),
		"relation requires target_task_id or target_key")

	assert(stageNotConfiguredKey.Render(stageNotConfiguredInput{TaskType: "mobile", Column: "analiz_review"}),
		"mobile tasks don't use the analiz_review column — this task type's workflow has no stage configured for it")
}
