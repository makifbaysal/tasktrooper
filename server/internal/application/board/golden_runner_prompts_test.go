package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// assertGolden pins fn's output against testdata/golden/<name>.txt, byte for
// byte. Step A (moving this text into catalog/system verbatim) must leave
// every one of these green; Step B (splitting imperatives into the catalog
// agents' own column files) updates the fixture deliberately, in a commit
// that says so.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture %s: %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s: output does not match testdata/golden/%[1]s.txt\n--- got ---\n%s\n--- want ---\n%s", name, got, string(want))
	}
}

func fixedUUID(tail string) uuid.UUID {
	return uuid.MustParse("00000000-0000-0000-0000-" + tail)
}

func TestGoldenColumnInstruction(t *testing.T) {
	assertGolden(t, "col_todo_task", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnTodo, TaskType: "task"}))
	assertGolden(t, "col_todo_analiz", columnInstruction(analizWF, domain.BoardTask{Column: domain.TaskColumnTodo, TaskType: "analiz"}))
	assertGolden(t, "col_in_progress_task", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnInProgress, TaskType: "task"}))
	assertGolden(t, "col_in_progress_analiz", columnInstruction(analizWF, domain.BoardTask{Column: domain.TaskColumnInProgress, TaskType: "analiz"}))
	assertGolden(t, "col_need_revision_task", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnNeedRevision, TaskType: "task"}))
	assertGolden(t, "col_need_revision_analiz", columnInstruction(analizWF, domain.BoardTask{Column: domain.TaskColumnNeedRevision, TaskType: "analiz"}))
	assertGolden(t, "col_code_review", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnCodeReview, TaskType: "task"}))
	assertGolden(t, "col_ready_for_qa", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnReadyForQA, TaskType: "task"}))
	assertGolden(t, "col_in_qa", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnInQA, TaskType: "task"}))
	assertGolden(t, "col_pm_uat", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnPMUAT, TaskType: "task"}))
	assertGolden(t, "col_done_task", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnDone, TaskType: "task"}))
	assertGolden(t, "col_done_analiz", columnInstruction(analizWF, domain.BoardTask{Column: domain.TaskColumnDone, TaskType: "analiz"}))
	assertGolden(t, "col_released", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnReleased, TaskType: "task"}))
	assertGolden(t, "col_default", columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnBacklog, TaskType: "task"}))
}

func TestGoldenReviewCriteriaHeader(t *testing.T) {
	assertGolden(t, "rch_ready_for_qa", reviewCriteriaHeader(domain.TaskColumnReadyForQA))
	assertGolden(t, "rch_in_qa", reviewCriteriaHeader(domain.TaskColumnInQA))
	assertGolden(t, "rch_pm_uat", reviewCriteriaHeader(domain.TaskColumnPMUAT))
	assertGolden(t, "rch_code_review", reviewCriteriaHeader(domain.TaskColumnCodeReview))
	assertGolden(t, "rch_default", reviewCriteriaHeader(domain.TaskColumnTodo))
}

func goldenReviewCriteriaAndChangedSince() ([]domain.AcceptanceCriterion, map[string][]string) {
	sha1 := "1111111111112222222222223333333333334444"
	sha2 := "5555555555556666666666667777777777778888"
	sha3 := "9999999999990000000000001111111111112222"
	manyFiles := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		manyFiles = append(manyFiles, "file"+string(rune('a'+i))+".go")
	}
	criteria := []domain.AcceptanceCriterion{
		{
			ID: fixedUUID("000000000010"), Text: "Approved with changed files", Completed: true,
			Checks: []domain.CriterionCheck{
				{Role: domain.CriterionReviewRoleQA, Approved: true, VerifiedSHA: sha1},
			},
		},
		{
			ID: fixedUUID("000000000011"), Text: "Approved with nothing changed since", Completed: true,
			Checks: []domain.CriterionCheck{
				{Role: domain.CriterionReviewRolePM, Approved: true, VerifiedSHA: sha2},
			},
		},
		{
			ID: fixedUUID("000000000012"), Text: "Rejected with a note",
			Checks: []domain.CriterionCheck{
				{Role: domain.CriterionReviewRoleQA, Approved: false, Note: "Login button missing"},
			},
		},
		{
			ID: fixedUUID("000000000013"), Text: "Rejected with no note",
			Checks: []domain.CriterionCheck{
				{Role: domain.CriterionReviewRolePM, Approved: false},
			},
		},
		{
			ID: fixedUUID("000000000014"), Text: "No verdict at all yet",
		},
		{
			ID: fixedUUID("000000000015"), Text: "Approved with many changed files", Completed: true,
			Checks: []domain.CriterionCheck{
				{Role: domain.CriterionReviewRoleQA, Approved: true, VerifiedSHA: sha3},
			},
		},
	}
	changedSince := map[string][]string{
		sha1: {"a.go", "b.go"},
		sha2: {},
		sha3: manyFiles,
	}
	return criteria, changedSince
}

func TestGoldenCriteriaMessage(t *testing.T) {
	assertGolden(t, "cm_empty", criteriaMessage(taskWF, domain.BoardTask{Column: domain.TaskColumnTodo}, nil, nil))

	openCriteria := []domain.AcceptanceCriterion{
		{ID: fixedUUID("000000000001"), Text: "User can log in"},
		{ID: fixedUUID("000000000002"), Text: "User can log out"},
	}
	assertGolden(t, "cm_open", criteriaMessage(taskWF, domain.BoardTask{Column: domain.TaskColumnTodo}, openCriteria, nil))

	reviewCriteria, changedSince := goldenReviewCriteriaAndChangedSince()
	assertGolden(t, "cm_review", criteriaMessage(taskWF, domain.BoardTask{Column: domain.TaskColumnReadyForQA}, reviewCriteria, changedSince))
}

func TestGoldenBuildTriggerMessage(t *testing.T) {
	taskID := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")
	repoID := uuid.MustParse("00000000-0000-0000-0000-0000000000cc")
	baseTask := domain.BoardTask{
		ID: taskID, Key: "T-1", Title: "Add login", TaskType: "task",
		Column: domain.TaskColumnTodo, Description: "Implement login",
	}
	baseJob := RunJob{
		Task: baseTask, RepositoryID: repoID,
		Event: domain.BoardEvent{EventType: "task_moved"},
	}
	assertGolden(t, "btm_basic", buildTriggerMessage(baseJob, taskWF, nil, nil))

	jobWithColInstr := baseJob
	jobWithColInstr.ColumnInstruction = "Example per-agent column instruction text."
	assertGolden(t, "btm_with_column_instruction", buildTriggerMessage(jobWithColInstr, taskWF, nil, nil))

	reviewCriteria, changedSince := goldenReviewCriteriaAndChangedSince()
	jobWithCriteria := baseJob
	jobWithCriteria.Task.Column = domain.TaskColumnReadyForQA
	assertGolden(t, "btm_with_criteria", buildTriggerMessage(jobWithCriteria, taskWF, reviewCriteria, changedSince))

	jobDone := baseJob
	jobDone.Task.Column = domain.TaskColumnDone
	assertGolden(t, "btm_done_closing", buildTriggerMessage(jobDone, taskWF, nil, nil))
}

func TestGoldenClosingStep(t *testing.T) {
	assertGolden(t, "closing_step_build_verify", closingStep(taskWF, RunJob{Task: domain.BoardTask{Column: domain.TaskColumnTodo}}))
	assertGolden(t, "closing_step_default", closingStep(analizWF, RunJob{Task: domain.BoardTask{Column: domain.TaskColumnAnalizReview, TaskType: domain.TaskTypeAnaliz}}))
	assertGolden(t, "closing_step_review", closingStep(taskWF, RunJob{Task: domain.BoardTask{Column: domain.TaskColumnCodeReview, TaskType: "task"}}))
	assertGolden(t, "closing_step_release", closingStep(taskWF, RunJob{Task: domain.BoardTask{Column: domain.TaskColumnDone, TaskType: "task"}}))
	assertGolden(t, "closing_step_default_analiz_done", closingStep(analizWF, RunJob{Task: domain.BoardTask{Column: domain.TaskColumnDone, TaskType: domain.TaskTypeAnaliz}}))
}

func TestGoldenPrependProjectContext(t *testing.T) {
	out1 := prependProjectContext(nil, "TaskTrooper backend", "Tools note here.")
	assertGolden(t, "ppc_both", out1[0].Content)
	out2 := prependProjectContext(nil, "TaskTrooper backend", "")
	assertGolden(t, "ppc_desc_only", out2[0].Content)
	out3 := prependProjectContext(nil, "", "Tools note here.")
	assertGolden(t, "ppc_tools_only", out3[0].Content)
}

func TestGoldenRevisionCommentsMessage(t *testing.T) {
	comments := []domain.TaskComment{
		{AuthorType: "user", Content: "please also fix the header"},
		{AuthorType: "architect", Content: "1. Missing null check\n2. No test added"},
	}
	assertGolden(t, "revision_comments", revisionCommentsMessage(comments))
}

func TestGoldenPreviousRunFailuresMessage(t *testing.T) {
	currentRunID := uuid.MustParse("00000000-0000-0000-0000-0000000000dd")
	prevRuns := []domain.TaskAgentRun{
		{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000ee"), Status: domain.TaskAgentRunStatusFailed, ErrorPattern: "run_terminal failed 3×, edit_file failed 2×"},
		{ID: currentRunID},
	}
	assertGolden(t, "previous_run_failures", previousRunFailuresMessage(prevRuns, currentRunID))
}

type analysisRefUpdater struct {
	fakeTaskUpdater
	refs []domain.AnalysisReference
}

func (a *analysisRefUpdater) AnalysisReferences(_ context.Context, _ uuid.UUID) ([]domain.AnalysisReference, error) {
	return a.refs, nil
}

func TestGoldenAnalysisContext(t *testing.T) {
	taskID := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")
	refUpdater := &analysisRefUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: domain.BoardTask{ID: taskID}},
		refs: []domain.AnalysisReference{
			{
				TaskID: uuid.MustParse("00000000-0000-0000-0000-0000000000ff"), Key: "A-1", Title: "Login analysis",
				Documents: []domain.TaskDocument{
					{Title: "Spec", Content: "The login flow must support email and password.", Format: domain.DocumentFormatMarkdown},
				},
			},
			{
				TaskID: uuid.MustParse("00000000-0000-0000-0000-000000000011"), Key: "A-2", Title: "Empty analysis",
			},
		},
	}
	r := &Runner{taskUpdater: refUpdater}
	assertGolden(t, "analysis_context", r.analysisContext(context.Background(), RunJob{Task: domain.BoardTask{ID: taskID}}))
}

func TestGoldenGuardReasons(t *testing.T) {
	assertGolden(t, "guard_ungrounded_analysis", ungroundedAnalysisReason)
	assertGolden(t, "guard_ungrounded_qa", ungroundedQAReason)
	assertGolden(t, "guard_no_ui_evidence", noUIEvidenceReason)
	assertGolden(t, "guard_ungrounded_pmuat", ungroundedPMUATReason)
	assertGolden(t, "guard_pm_uncovered_criterion", pmUncoveredCriterionReason)
}

func TestGoldenOutOfBudgetComment(t *testing.T) {
	taskID := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")
	repoID := uuid.MustParse("00000000-0000-0000-0000-0000000000cc")
	currentRunID := uuid.MustParse("00000000-0000-0000-0000-0000000000dd")
	budgetErr := &agent.BudgetExhaustedError{
		Budget:  40,
		Stats:   agent.RunStats{},
		Partial: "Implemented the login form; still need to wire up the API call.",
	}
	obUpdater := &fakeTaskUpdater{task: domain.BoardTask{ID: taskID, TaskType: "task"}}
	obRunner := &Runner{taskUpdater: obUpdater, runs: &countingRunStore{}}
	obJob := RunJob{Task: domain.BoardTask{ID: taskID, TaskType: "task"}, RepositoryID: repoID, Run: domain.TaskAgentRun{ID: currentRunID}}
	_ = obRunner.failRunOutOfBudget(context.Background(), obJob, domain.TaskAgentRun{ID: currentRunID}, "", "", domain.Agent{Name: "backend-developer"}, budgetErr)
	require1(t, len(obUpdater.comments) == 1, "expected exactly one out-of-budget comment")
	assertGolden(t, "out_of_budget_comment", obUpdater.comments[0].Content)
}

var errUpdateBoom = &boomErr{}

type boomErr struct{}

func (b *boomErr) Error() string { return "concurrent modification" }

func TestGoldenHandoffComments(t *testing.T) {
	agentID := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	taskID := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")

	unverifiedTask := domain.BoardTask{ID: taskID, Column: domain.TaskColumnInProgress}
	unverifiedUpdater := &fakeTaskUpdater{task: unverifiedTask}
	unverifiedRunner := handoffRunner(unverifiedUpdater, &handoffGit{diff: "diff --git a/app.tsx b/app.tsx"})
	unverifiedUsage := registry.NewToolUsage()
	unverifiedUsage.Record("edit_file")
	unverifiedRunner.advanceToCodeReview(context.Background(), runJobFor(unverifiedTask, agentID), taskWF, "/w/task-1", unverifiedUsage)
	require1(t, len(unverifiedUpdater.comments) == 1, "expected the unverified-run comment")
	assertGolden(t, "handoff_unverified_run_comment", unverifiedUpdater.comments[0].Content)

	uiTask := domain.BoardTask{ID: taskID, Column: domain.TaskColumnInProgress}
	uiUpdater := &fakeTaskUpdater{task: uiTask}
	uiRunner := handoffRunnerWithProjects(uiUpdater, &handoffGit{diff: "diff --git a/App.tsx b/App.tsx", files: []string{"web/App.tsx"}}, uiKindRepos{kind: "frontend"})
	uiUsage := registry.NewToolUsage()
	uiUsage.Record("run_terminal")
	uiRunner.advanceToCodeReview(context.Background(), runJobFor(uiTask, agentID), taskWF, "/w/task-1", uiUsage)
	require1(t, len(uiUpdater.comments) == 1, "expected the unseen-UI comment")
	assertGolden(t, "handoff_unseen_ui_comment", uiUpdater.comments[0].Content)

	refusedTask := domain.BoardTask{ID: taskID, Column: domain.TaskColumnInProgress}
	refusedUpdater := &fakeTaskUpdater{task: refusedTask, err: errUpdateBoom}
	refusedGit := &handoffGit{diff: "diff --git a/x.go b/x.go"}
	refusedRunner := handoffRunner(refusedUpdater, refusedGit)
	refusedUsage := registry.NewToolUsage()
	refusedUsage.Record("run_terminal")
	refusedRunner.advanceToCodeReview(context.Background(), runJobFor(refusedTask, agentID), taskWF, "/w/task-1", refusedUsage)
	require1(t, len(refusedUpdater.comments) == 1, "expected the code_review handoff refusal comment")
	assertGolden(t, "handoff_code_review_refused_comment", refusedUpdater.comments[0].Content)

	analizRefusedUpdater := &fakeTaskUpdater{task: refusedTask, err: errUpdateBoom}
	analizRefusedRunner := analizRunner(analizRefusedUpdater)
	analizRefusedRunner.advanceToAnalizReview(context.Background(), runJobFor(refusedTask, agentID), analizWF, documentedUsage())
	require1(t, len(analizRefusedUpdater.comments) == 1, "expected the analiz_review handoff refusal comment")
	assertGolden(t, "handoff_analiz_review_refused_comment", analizRefusedUpdater.comments[0].Content)
}

func TestGoldenHandoffRevisionComments(t *testing.T) {
	assertGolden(t, "handoff_code_review_refused_revision_comment",
		handoffCodeReviewRefusedRevisionKey.Render(handoffReasonInput{Reason: "criteria are open"}))
	assertGolden(t, "handoff_analiz_review_refused_revision_comment",
		handoffAnalizReviewRefusedRevisionKey.Render(handoffReasonInput{Reason: "criteria are open"}))
}

func require1(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}
