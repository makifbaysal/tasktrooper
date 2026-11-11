package board

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// This file pins the exact, current byte output of every board_context
// prompt-producing function before its text moves into catalog/system — see
// server/internal/application/board/prompts_context.go once that lands.
// Each rendered string must stay byte-identical after the move.

var (
	goldenCriterionID1 = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	goldenCriterionID2 = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

func TestGolden_CommitMessageSystemPrompt(t *testing.T) {
	assert.Equal(t,
		"You write git commit messages for an automated engineering agent.\n"+
			"Reply with the commit message and nothing else: no code fences, no quotes, no commentary.\n"+
			"ALWAYS write in English, even when the task and the summary are in another language — translate them.\n"+
			"First line: Conventional Commits (`type(scope): summary`), imperative mood, lowercase after the colon, at most 72 characters.\n"+
			"Then, only if it adds something the subject does not, a blank line and at most three short body lines saying what changed and why.\n"+
			"Describe only what the input says was done. Never invent changes, files or reasons.",
		prompt.Text(commitMessageSystemKey))
}

func TestGolden_CommitMessageUserInput(t *testing.T) {
	assert.Equal(t, "Task title: Add the android app link", commitMessageUserInput("Add the android app link", ""))

	assert.Equal(t,
		"Task title: Add the android app link\n\nWhat the agent reports it did:\nAdded the link, dropped the wishlist section.",
		commitMessageUserInput("Add the android app link", "Added the link, dropped the wishlist section."))

	assert.Equal(t,
		"Task title: Fix the deploy target lookup\n\nWhat the agent reports it did:\nRewrote resolveDeployTarget to fall back to the default environment when the repository names none, added a regression test.",
		commitMessageUserInput("Fix the deploy target lookup", "Rewrote resolveDeployTarget to fall back to the default environment when the repository names none, added a regression test."))
}

func TestGolden_CriteriaSweepPrompt(t *testing.T) {
	one := []domain.AcceptanceCriterion{{ID: goldenCriterionID1, Text: "Export includes the archived rows"}}
	assert.Equal(t,
		"Before this run is closed, settle its acceptance criteria. These are still open:\n"+
			"- [00000000-0000-0000-0000-000000000001] Export includes the archived rows\n"+
			"\nFor EACH id above, do exactly one of three things now:\n"+
			"1. You implemented it in this run → call set_criterion_completed with that id.\n"+
			"2. It is deliberately NOT being done (out of scope, superseded, impossible as written) → call cancel_criterion with that id and a concrete reason. "+
			"That reason is stored on the criterion and posted as a task comment, so say it in a sentence a person can act on.\n"+
			"3. You overlooked it, or ran out of time → DO THE WORK NOW, in this run, then tick it with set_criterion_completed.\n"+
			"Do not tick anything you did not implement. "+
			"The hand-off to code_review is refused while any criterion is open, so a criterion you silently skip parks your finished work in this column.",
		criteriaSweepPrompt(one, 1))

	two := []domain.AcceptanceCriterion{
		{ID: goldenCriterionID1, Text: "Export includes the archived rows"},
		{ID: goldenCriterionID2, Text: "CSV download link shows on the task"},
	}
	assert.Equal(t,
		"Before this run is closed, settle its acceptance criteria. These are still open:\n"+
			"- [00000000-0000-0000-0000-000000000001] Export includes the archived rows\n"+
			"- [00000000-0000-0000-0000-000000000002] CSV download link shows on the task\n"+
			"\nFor EACH id above, do exactly one of three things now:\n"+
			"1. You implemented it in this run → call set_criterion_completed with that id.\n"+
			"2. It is deliberately NOT being done (out of scope, superseded, impossible as written) → call cancel_criterion with that id and a concrete reason. "+
			"That reason is stored on the criterion and posted as a task comment, so say it in a sentence a person can act on.\n"+
			"3. You overlooked it, or ran out of time → DO THE WORK NOW, in this run, then tick it with set_criterion_completed.\n"+
			"Do not tick anything you did not implement. "+
			"The hand-off to code_review is refused while any criterion is open, so a criterion you silently skip parks your finished work in this column.",
		criteriaSweepPrompt(two, 1))

	assert.Equal(t,
		"These acceptance criteria are STILL open after round 2 of this check:\n"+
			"- [00000000-0000-0000-0000-000000000001] Export includes the archived rows\n"+
			"\nFor EACH id above, do exactly one of three things now:\n"+
			"1. You implemented it in this run → call set_criterion_completed with that id.\n"+
			"2. It is deliberately NOT being done (out of scope, superseded, impossible as written) → call cancel_criterion with that id and a concrete reason. "+
			"That reason is stored on the criterion and posted as a task comment, so say it in a sentence a person can act on.\n"+
			"3. You overlooked it, or ran out of time → DO THE WORK NOW, in this run, then tick it with set_criterion_completed.\n"+
			"Answer 3 is the expected one at this point: you have already had a round to say the criterion was out of scope, "+
			"and you did not. Implement what is missing and tick it, or cancel it with a reason — an unanswered criterion parks this task "+
			"and a human has to come and find out why. Do not reply with a summary of what you would do; make the change.",
		criteriaSweepPrompt(one, 3))
}

func TestGolden_ReviewVerdictAskPrompt(t *testing.T) {
	assert.Equal(t,
		"Answer with ONE word and nothing else — no explanation, no tool call.\n"+
			"Based on the review you just completed: `APPROVE` if everything you required is satisfied and the work should move on to `ready_for_qa`, `REVISE` if anything you flagged still needs work.\n"+
			"This answer is recorded as your verdict and the board move is made from it, so it must match the review you wrote above.",
		reviewVerdictAskPrompt(domain.TaskColumnReadyForQA))

	assert.Equal(t,
		"Answer with ONE word and nothing else — no explanation, no tool call.\n"+
			"Based on the review you just completed: `APPROVE` if everything you required is satisfied and the work should move on to `done`, `REVISE` if anything you flagged still needs work.\n"+
			"This answer is recorded as your verdict and the board move is made from it, so it must match the review you wrote above.",
		reviewVerdictAskPrompt(domain.TaskColumnDone))

	assert.Equal(t,
		"Answer with ONE word and nothing else — no explanation, no tool call.\n"+
			"Based on the review you just completed: `APPROVE` if everything you required is satisfied and the work should move on to `human_uat`, `REVISE` if anything you flagged still needs work.\n"+
			"This answer is recorded as your verdict and the board move is made from it, so it must match the review you wrote above.",
		reviewVerdictAskPrompt(domain.TaskColumnHumanUAT))
}

func TestGolden_ReviewVerdictSweepPrompt(t *testing.T) {
	assert.Equal(t,
		"Your review is finished but the task is still in `code_review` — you did not record where it goes, so the board shows it as still under review and nobody picks it up.\n"+
			"\nThen leave the column, based on the verdict you just gave:\n"+
			"1. Everything you required is satisfied → call move_board_task to `ready_for_qa`.\n"+
			"2. Anything you flagged still needs work → call move_board_task to `need_revision`, and make sure your findings are on the task as a numbered comment.\n"+
			"If the move is refused, read the error: it names exactly what is missing, and fixing that and retrying the move is part of this run. Do not re-review, do not start new testing, and do not change your verdict.",
		reviewVerdictSweepPrompt(domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, "", nil))

	missing := []domain.AcceptanceCriterion{{ID: goldenCriterionID1, Text: "Export includes archived rows"}}
	assert.Equal(t,
		"Your review is finished but the task is still in `in_qa` — you did not record where it goes, so the board shows it as still under review and nobody picks it up.\n"+
			"\nFirst, the acceptance criteria you have not ruled on. The forward move is REFUSED while any of these lacks your qa verdict — that refusal is what you hit if you already tried to move the task:\n"+
			"- [00000000-0000-0000-0000-000000000001] Export includes archived rows\n"+
			"For EACH id above call review_criterion now, from what you executed in this run: approve it when your own run covered it, reject it with an expected-vs-actual note when it failed or you could not exercise it. Do not approve anything you did not observe.\n"+
			"\nThen leave the column, based on the verdict you just gave:\n"+
			"1. Everything you required is satisfied → call move_board_task to `done`.\n"+
			"2. Anything you flagged still needs work → call move_board_task to `need_revision`, and make sure your findings are on the task as a numbered comment.\n"+
			"If the move is refused, read the error: it names exactly what is missing, and fixing that and retrying the move is part of this run. Do not re-review, do not start new testing, and do not change your verdict.",
		reviewVerdictSweepPrompt(domain.TaskColumnInQA, domain.TaskColumnDone, domain.CriterionReviewRoleQA, missing))

	missing2 := []domain.AcceptanceCriterion{
		{ID: goldenCriterionID1, Text: "First criterion"},
		{ID: goldenCriterionID2, Text: "Second criterion"},
	}
	assert.Equal(t,
		"Your review is finished but the task is still in `human_uat` — you did not record where it goes, so the board shows it as still under review and nobody picks it up.\n"+
			"\nFirst, the acceptance criteria you have not ruled on. The forward move is REFUSED while any of these lacks your qa verdict — that refusal is what you hit if you already tried to move the task:\n"+
			"- [00000000-0000-0000-0000-000000000001] First criterion\n"+
			"- [00000000-0000-0000-0000-000000000002] Second criterion\n"+
			"For EACH id above call review_criterion now, from what you executed in this run: approve it when your own run covered it, reject it with an expected-vs-actual note when it failed or you could not exercise it. Do not approve anything you did not observe.\n"+
			"\nThen leave the column, based on the verdict you just gave:\n"+
			"1. Everything you required is satisfied → call move_board_task to `released`.\n"+
			"2. Anything you flagged still needs work → call move_board_task to `need_revision`, and make sure your findings are on the task as a numbered comment.\n"+
			"If the move is refused, read the error: it names exactly what is missing, and fixing that and retrying the move is part of this run. Do not re-review, do not start new testing, and do not change your verdict.",
		reviewVerdictSweepPrompt(domain.TaskColumnHumanUAT, domain.TaskColumnReleased, domain.CriterionReviewRoleQA, missing2))
}

func TestGolden_VerifyFixPrompt(t *testing.T) {
	assert.Equal(t,
		"Automated verification failed in the task workspace. Fix these errors, then re-check your work. "+
			"Do not post an add_task_comment about the fix or the task being done — the system publishes your closing summary to the card once these checks pass:\n\n"+
			"$ go build ./...\n# github.com/acme/thing\nundefined: Foo",
		verifyFixPrompt("$ go build ./...\n# github.com/acme/thing\nundefined: Foo"))

	assert.Equal(t,
		"Automated verification failed in the task workspace. Fix these errors, then re-check your work. "+
			"Do not post an add_task_comment about the fix or the task being done — the system publishes your closing summary to the card once these checks pass:\n\n"+
			"$ npm test\n3 failing",
		verifyFixPrompt("$ npm test\n3 failing"))

	assert.Equal(t,
		"Automated verification failed in the task workspace. Fix these errors, then re-check your work. "+
			"Do not post an add_task_comment about the fix or the task being done — the system publishes your closing summary to the card once these checks pass:\n\n",
		verifyFixPrompt(""))
}

func TestGolden_CoverageOverallWarning(t *testing.T) {
	assert.Equal(t,
		"[coverage warning] overall 48.5% is below the 80% this repository asks for.\n"+
			"What is missing is coverage of code this task did not touch, so treat it as a note for whoever reads this run: "+
			"if untested paths sit next to your change — the branches that handle errors and edge cases — covering them is worth a few minutes. "+
			"It does not hold the task: the hand-off proceeds either way.",
		coverageOverallWarning(48.5, 80))

	assert.Equal(t,
		"[coverage warning] overall 0.0% is below the 90% this repository asks for.\n"+
			"What is missing is coverage of code this task did not touch, so treat it as a note for whoever reads this run: "+
			"if untested paths sit next to your change — the branches that handle errors and edge cases — covering them is worth a few minutes. "+
			"It does not hold the task: the hand-off proceeds either way.",
		coverageOverallWarning(0, 90))

	assert.Equal(t,
		"[coverage warning] overall 89.9% is below the 90% this repository asks for.\n"+
			"What is missing is coverage of code this task did not touch, so treat it as a note for whoever reads this run: "+
			"if untested paths sit next to your change — the branches that handle errors and edge cases — covering them is worth a few minutes. "+
			"It does not hold the task: the hand-off proceeds either way.",
		coverageOverallWarning(89.9, 90))
}

func TestGolden_OverallCoverageNote(t *testing.T) {
	assert.Equal(t, "[coverage] overall 48.5% (threshold 80%, reported — not enforced)",
		overallCoverageNote(domain.Repository{RequireOverallCoverage: true, CoverageThreshold: 80}, "", 48.5))
	assert.Equal(t, "[coverage] overall 90.0% (threshold 90%, reported — not enforced)",
		overallCoverageNote(domain.Repository{RequireOverallCoverage: true, CoverageThreshold: 90}, "", 90))
	assert.Equal(t, "[coverage] overall 48.5% (reported only — this repository sets no overall bar)",
		overallCoverageNote(domain.Repository{}, "", 48.5))
}

func TestGolden_MutationNoteWarningAndGateMet(t *testing.T) {
	assert.Equal(t,
		"[mutation] 72.3% of mutants killed. Coverage says which lines ran; this says whether a test would have "+
			"noticed them behaving differently. A low score with high coverage means assertions are missing, not lines.",
		mutationNote(72.3))
	assert.Equal(t,
		"[mutation] 100.0% of mutants killed. Coverage says which lines ran; this says whether a test would have "+
			"noticed them behaving differently. A low score with high coverage means assertions are missing, not lines.",
		mutationNote(100))

	assert.Equal(t,
		"\n[mutation warning] 55.0% is below the 60% this repository asks for. Say so in your hand-off; it does not hold the task.",
		mutationWarning(55, 60))
	assert.Equal(t,
		"\n[mutation warning] 0.0% is below the 70% this repository asks for. Say so in your hand-off; it does not hold the task.",
		mutationWarning(0, 70))

	assert.Equal(t, " (threshold 60%, met)", mutationGateMet(60))
	assert.Equal(t, " (threshold 100%, met)", mutationGateMet(100))
}

func TestGolden_RenderNewCodeCoverage(t *testing.T) {
	assert.Equal(t,
		"[new-code coverage] 2 of 3 changed lines covered — too few to read anything into.",
		renderNewCodeCoverage(NewCodeCoverage{Percent: 66.66, Covered: 2, Total: 3}))

	assert.Equal(t,
		"[new-code coverage] 100.0% (10/10 changed lines, threshold 90%)",
		renderNewCodeCoverage(NewCodeCoverage{Percent: 100, Covered: 10, Total: 10}))

	assert.Equal(t,
		"[coverage warning] new-code coverage 50.0% (3/6 changed lines) is below the 90% this change should leave behind.\n"+
			"This is the coverage of the lines YOUR diff added or changed, not the repository's overall figure — it is about your change alone, and no amount of pre-existing untested code affects it.\n"+
			"Uncovered lines you wrote:\n"+
			"  a.go:4\n"+
			"  a.go:5\n"+
			"  …and 1 more.\n"+
			"Tests that execute them — the error and edge-case branches, not more assertions on the happy path — are worth adding while the code is still fresh. This is a warning only: the task moves on either way.",
		renderNewCodeCoverage(NewCodeCoverage{Percent: 50, Covered: 3, Total: 6, Uncovered: []string{"a.go:4", "a.go:5"}}))

	assert.Equal(t,
		"[coverage warning] new-code coverage 40.0% (4/10 changed lines) is below the 90% this change should leave behind.\n"+
			"This is the coverage of the lines YOUR diff added or changed, not the repository's overall figure — it is about your change alone, and no amount of pre-existing untested code affects it.\n"+
			"Uncovered lines you wrote:\n"+
			"  a.go:4\n"+
			"  …and 5 more.\n"+
			"Tests that execute them — the error and edge-case branches, not more assertions on the happy path — are worth adding while the code is still fresh. This is a warning only: the task moves on either way.",
		renderNewCodeCoverage(NewCodeCoverage{Percent: 40, Covered: 4, Total: 10, Uncovered: []string{"a.go:4"}}))
}

func TestGolden_HumanRequirementsHeader(t *testing.T) {
	assert.Equal(t,
		"## The human's requirements written on this task (authoritative — they amend the description and acceptance criteria)\n"+
			"The person who owns this task wrote these comments on it, oldest first. Each one is part of what the task asks for: "+
			"where it adds to, changes or contradicts the description, its out-of-scope list or an acceptance criterion, the comment wins, "+
			"and a later comment wins over an earlier one. Work that implements them is IN scope — build it, review it, test it and accept it "+
			"against them exactly as you would an acceptance criterion; never flag it as scope creep, and never ask for it to be reverted or split into another task.\n",
		humanRequirementsHeader())
}

func TestGolden_ReviewPRContextMessage(t *testing.T) {
	assert.Equal(t,
		"## Pull request under review: https://github.com/acme/acme-web/pull/7\n"+
			"The diff below is exactly what this PR changes. Review those changes — read the rest of the repository whenever you need it to judge them, but never review files the PR does not touch.",
		reviewPRContextMessage("https://github.com/acme/acme-web/pull/7"))

	assert.Equal(t,
		"## Pull request under review: https://github.com/acme/acme-web/pull/128\n"+
			"The diff below is exactly what this PR changes. Review those changes — read the rest of the repository whenever you need it to judge them, but never review files the PR does not touch.",
		reviewPRContextMessage("https://github.com/acme/acme-web/pull/128"))

	assert.Equal(t,
		"## Pull request under review: \n"+
			"The diff below is exactly what this PR changes. Review those changes — read the rest of the repository whenever you need it to judge them, but never review files the PR does not touch.",
		reviewPRContextMessage(""))
}

func TestGolden_RevisionPRCommentsHeader(t *testing.T) {
	assert.Equal(t,
		"## Pull request review comments (https://github.com/acme/acme-web/pull/7)\n"+
			"These are the reviewer's notes on the PR itself — they are part of the revision feedback, not a separate topic. Fix what they point at in this run, and answer anything you disagree with using comment_on_pull_request.\n",
		revisionPRCommentsHeader("https://github.com/acme/acme-web/pull/7"))

	assert.Equal(t,
		"## Pull request review comments (https://github.com/acme/acme-web/pull/42)\n"+
			"These are the reviewer's notes on the PR itself — they are part of the revision feedback, not a separate topic. Fix what they point at in this run, and answer anything you disagree with using comment_on_pull_request.\n",
		revisionPRCommentsHeader("https://github.com/acme/acme-web/pull/42"))

	assert.Equal(t,
		"## Pull request review comments ()\n"+
			"These are the reviewer's notes on the PR itself — they are part of the revision feedback, not a separate topic. Fix what they point at in this run, and answer anything you disagree with using comment_on_pull_request.\n",
		revisionPRCommentsHeader(""))
}

func TestGolden_ReviewDiffMessageHeadings(t *testing.T) {
	assert.Equal(t, "## Task branch diff (changes made for this task so far)\n```diff\n+ line\n```",
		reviewDiffMessage(taskWF, domain.TaskColumnInProgress, "+ line"))

	assert.Equal(t, "## Pull request diff — the complete change you are reviewing (task branch vs its base)\n```diff\n+ line\n```",
		reviewDiffMessage(taskWF, domain.TaskColumnCodeReview, "+ line"))

	assert.Equal(t, "## Pull request diff — the complete change you are reviewing (task branch vs its base)\n```diff\n+ other\n```",
		reviewDiffMessage(taskWF, domain.TaskColumnInQA, "+ other"))
}

func TestGolden_RenderReviewAnnotationsHeader(t *testing.T) {
	assert.Equal(t,
		"## Review comments on your analysis document\n"+
			"The human reviewed your analysis and sent back 1 comment(s), each anchored to a passage of the document. This review IS the revision request: address EVERY comment at its root, in the SAME document (document_id doc-1) — never attach a second document.\n"+
			"1. Read the current source: list_task_documents with task_id A-7, document_id, raw: true (follow next_offset until you have all of it).\n"+
			"2. Revise it with update_task_document on that document_id — `edits` for targeted passages, `content` for a full rewrite. Keep the report's structure, section ids and styling; re-read the code where a comment questions a fact.\n"+
			"3. Then call resolve_document_annotations ONCE with {id, reply} for every comment below — the reply is one line saying what changed and where, or why you deliberately kept it.\n"+
			"When this run ends with the document revised, the system moves the task back to analiz_review — do not move it yourself.\n",
		renderReviewAnnotationsHeader(1, []string{"doc-1"}, "A-7"))

	assert.Equal(t,
		"## Review comments on your analysis document\n"+
			"The human reviewed your analysis and sent back 2 comment(s), each anchored to a passage of the document. This review IS the revision request: address EVERY comment at its root, in the SAME document (document_id doc-1, doc-2) — never attach a second document.\n"+
			"1. Read the current source: list_task_documents with task_id A-9, document_id, raw: true (follow next_offset until you have all of it).\n"+
			"2. Revise it with update_task_document on that document_id — `edits` for targeted passages, `content` for a full rewrite. Keep the report's structure, section ids and styling; re-read the code where a comment questions a fact.\n"+
			"3. Then call resolve_document_annotations ONCE with {id, reply} for every comment below — the reply is one line saying what changed and where, or why you deliberately kept it.\n"+
			"When this run ends with the document revised, the system moves the task back to analiz_review — do not move it yourself.\n",
		renderReviewAnnotationsHeader(2, []string{"doc-1", "doc-2"}, "A-9"))

	assert.Equal(t,
		"## Review comments on your analysis document\n"+
			"The human reviewed your analysis and sent back 0 comment(s), each anchored to a passage of the document. This review IS the revision request: address EVERY comment at its root, in the SAME document (document_id ) — never attach a second document.\n"+
			"1. Read the current source: list_task_documents with task_id A-1, document_id, raw: true (follow next_offset until you have all of it).\n"+
			"2. Revise it with update_task_document on that document_id — `edits` for targeted passages, `content` for a full rewrite. Keep the report's structure, section ids and styling; re-read the code where a comment questions a fact.\n"+
			"3. Then call resolve_document_annotations ONCE with {id, reply} for every comment below — the reply is one line saying what changed and where, or why you deliberately kept it.\n"+
			"When this run ends with the document revised, the system moves the task back to analiz_review — do not move it yourself.\n",
		renderReviewAnnotationsHeader(0, nil, "A-1"))
}

func TestGolden_ReviewAnnotationsOmittedNote(t *testing.T) {
	assert.Equal(t,
		"\n…3 more comment(s) are not shown here because this message is capped at 20000 bytes. Call list_document_annotations with task_id A-7 and status \"submitted\" to read ALL of them before you revise — every one needs an answer.\n",
		reviewAnnotationsOmittedNote(3, "A-7"))

	assert.Equal(t,
		"\n…1 more comment(s) are not shown here because this message is capped at 20000 bytes. Call list_document_annotations with task_id A-9 and status \"submitted\" to read ALL of them before you revise — every one needs an answer.\n",
		reviewAnnotationsOmittedNote(1, "A-9"))

	assert.Equal(t,
		"\n…10 more comment(s) are not shown here because this message is capped at 20000 bytes. Call list_document_annotations with task_id A-100 and status \"submitted\" to read ALL of them before you revise — every one needs an answer.\n",
		reviewAnnotationsOmittedNote(10, "A-100"))
}

func TestGolden_PipelineFailureComments(t *testing.T) {
	assert.Equal(t,
		"Pipeline failed — build:\n\n```\nexit status 1\n```",
		pipelineFailureComment("build", "exit status 1"))

	assert.Equal(t,
		"Pipeline failed — deploy:\n\n```\ntimeout\n```",
		pipelineFailureComment("deploy", "timeout"))

	assert.Equal(t,
		"Deploy could not run — deploy:\n\n```\nGitHub Actions billing limit reached\n```\n\n"+
			"GitHub Actions is unavailable for this repository (billing, spending limit or Actions disabled), so this is not a code problem and the task is NOT being sent back to need_revision. "+
			"Deploy it the way this repository documents doing it locally, or move the task to `blocked` if it has no local deploy path.",
		pipelineFailureBlockedCIComment("deploy", "GitHub Actions billing limit reached"))

	assert.Equal(t,
		"Deploy could not run — prod-deploy:\n\n```\nActions disabled for this repository\n```\n\n"+
			"GitHub Actions is unavailable for this repository (billing, spending limit or Actions disabled), so this is not a code problem and the task is NOT being sent back to need_revision. "+
			"Deploy it the way this repository documents doing it locally, or move the task to `blocked` if it has no local deploy path.",
		pipelineFailureBlockedCIComment("prod-deploy", "Actions disabled for this repository"))
}
