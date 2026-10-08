package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGateMessageGoldenBeforeMove pins the exact byte output of every
// LLM-facing string the repository gates (service.go, test_cases.go) build
// today, before it moves into catalog/system/{guards,prompts/repository}/**.
// The move must keep every one of these assertions passing unchanged.
func TestGateMessageGoldenBeforeMove(t *testing.T) {
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	t.Run("criteriaGate", func(t *testing.T) {
		cases := []struct {
			name   string
			items  []domain.AcceptanceCriterion
			target domain.TaskColumn
			want   string
		}{
			{
				"one open criterion",
				[]domain.AcceptanceCriterion{{ID: id1, Text: "android link is on the page"}},
				domain.TaskColumnReadyForQA,
				"cannot move to ready_for_qa: 1 acceptance criteria incomplete: [00000000-0000-0000-0000-000000000001] android link is on the page — if you implemented them, tick each with set_criterion_completed; if one is deliberately not being done, cancel it with cancel_criterion and a reason; if you are reviewing (QA in ready_for_qa/in_qa, PM in pm_uat), record your verdict with review_criterion instead",
			},
			{
				"two open criteria",
				[]domain.AcceptanceCriterion{
					{ID: id1, Text: "a"},
					{ID: id2, Text: "b"},
				},
				domain.TaskColumnDone,
				"cannot move to done: 2 acceptance criteria incomplete: [00000000-0000-0000-0000-000000000001] a; [00000000-0000-0000-0000-000000000002] b — if you implemented them, tick each with set_criterion_completed; if one is deliberately not being done, cancel it with cancel_criterion and a reason; if you are reviewing (QA in ready_for_qa/in_qa, PM in pm_uat), record your verdict with review_criterion instead",
			},
			{
				"different target column",
				[]domain.AcceptanceCriterion{{ID: id1, Text: "a"}},
				domain.TaskColumnCodeReview,
				"cannot move to code_review: 1 acceptance criteria incomplete: [00000000-0000-0000-0000-000000000001] a — if you implemented them, tick each with set_criterion_completed; if one is deliberately not being done, cancel it with cancel_criterion and a reason; if you are reviewing (QA in ready_for_qa/in_qa, PM in pm_uat), record your verdict with review_criterion instead",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &Service{
					criteria:        &fakeCriteriaStore{items: tc.items},
					requireCriteria: true,
					workflows:       workflowtest.Default().Reader(),
				}
				err := svc.criteriaGate(context.Background(), uuid.New(), "task", tc.target)
				if err == nil || err.Error() != tc.want {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("criteriaReviewGate", func(t *testing.T) {
		cases := []struct {
			name   string
			items  []domain.AcceptanceCriterion
			prev   domain.TaskColumn
			target domain.TaskColumn
			want   string
		}{
			{
				"unchecked, qa",
				[]domain.AcceptanceCriterion{{ID: id1, Text: "android link is on the page", Completed: true}},
				domain.TaskColumnInQA,
				domain.TaskColumnDone,
				"cannot move to done: 1 acceptance criteria await your qa verdict: [00000000-0000-0000-0000-000000000001] android link is on the page — call review_criterion with each id above, then retry the move",
			},
			{
				"unchecked, pm",
				[]domain.AcceptanceCriterion{{ID: id1, Text: "a", Completed: true, Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true}}}},
				domain.TaskColumnPMUAT,
				domain.TaskColumnHumanUAT,
				"cannot move to human_uat: 1 acceptance criteria await your pm verdict: [00000000-0000-0000-0000-000000000001] a — call review_criterion with each id above, then retry the move",
			},
			{
				"rejected, qa",
				[]domain.AcceptanceCriterion{{ID: id1, Text: "login works", Completed: true, Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: false, Note: "500 on submit"}}}},
				domain.TaskColumnInQA,
				domain.TaskColumnPMUAT,
				"cannot move to pm_uat: 1 acceptance criteria are rejected by qa: [00000000-0000-0000-0000-000000000001] login works (500 on submit) — move the task to need_revision instead, or re-verify and approve them (review_criterion)",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &Service{
					criteria:        &fakeCriteriaStore{items: tc.items},
					requireCriteria: true,
					workflows:       workflowtest.Default().Reader(),
				}
				err := svc.criteriaReviewGate(context.Background(), uuid.New(), "task", tc.prev, tc.target)
				if err == nil || err.Error() != tc.want {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("testCaseGate", func(t *testing.T) {
		cases := []struct {
			name  string
			items []domain.TaskTestCase
			want  string
		}{
			{
				"no cases at all",
				nil,
				"cannot move to pm_uat: this task carries no test cases — record the cases you derived and executed with record_test_cases (title, category, status, expected/actual, evidence), including the ones you considered and rejected as status=invalid with the reason in notes",
			},
			{
				"one planned case",
				[]domain.TaskTestCase{{ID: id1, Title: "expired token is rejected", Status: domain.TestCaseStatusPlanned}},
				"cannot move to pm_uat: 1 test case(s) are still planned and were never executed: expired token is rejected — run each one and record its result (passed/failed), or mark it skipped with what blocked it, or invalid with why it is not a valid case",
			},
			{
				"two planned cases",
				[]domain.TaskTestCase{
					{ID: id1, Title: "a", Status: domain.TestCaseStatusPlanned},
					{ID: id2, Title: "b", Status: domain.TestCaseStatusPlanned},
				},
				"cannot move to pm_uat: 2 test case(s) are still planned and were never executed: a; b — run each one and record its result (passed/failed), or mark it skipped with what blocked it, or invalid with why it is not a valid case",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &Service{
					testCases:       &fakeTestCaseStore{items: tc.items},
					requireCriteria: true,
					workflows:       workflowtest.Default().Reader(),
				}
				err := svc.testCaseGate(context.Background(), uuid.New(), "task", domain.TaskColumnInQA, domain.TaskColumnPMUAT)
				if err == nil || err.Error() != tc.want {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("validateAgentSelfMove", func(t *testing.T) {
		agentID := uuid.New()
		task := domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &agentID}

		cases := []struct {
			name   string
			target domain.TaskColumn
			want   string
		}{
			{
				"need_revision",
				domain.TaskColumnNeedRevision,
				"you are this task's assignee: need_revision is reserved for reviewers handing work back. Fix the work yourself and move the task to code_review when done; if you are missing information, use ask_user instead",
			},
			{
				"todo",
				domain.TaskColumnTodo,
				"you claimed this task: do not move it back to todo. Continue the work and move it to code_review when done; if you are missing information, use ask_user",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := domain.UpdateBoardTaskRequest{Actor: domain.TaskActorAgent, ActorAgentID: &agentID, Column: &tc.target}
				err := validateAgentSelfMove(task, req, domain.TaskColumnInProgress)
				if err == nil || err.Error() != tc.want {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("validateMoveAllowed", func(t *testing.T) {
		cases := []struct {
			name     string
			blockers []domain.BoardTask
			want     string
		}{
			{
				"one blocker",
				[]domain.BoardTask{{ID: id1, Key: "T-5", Title: "API migration", Column: domain.TaskColumnInProgress}},
				"work order: this task is blocked until these are done: T-5 (API migration) [in_progress]",
			},
			{
				"two blockers",
				[]domain.BoardTask{
					{ID: id1, Key: "T-5", Title: "API migration", Column: domain.TaskColumnInProgress},
					{ID: id2, Key: "T-6", Title: "Schema change", Column: domain.TaskColumnTodo},
				},
				"work order: this task is blocked until these are done: T-5 (API migration) [in_progress], T-6 (Schema change) [todo]",
			},
			{
				"blocker with no title",
				[]domain.BoardTask{{ID: id1, Key: "T-7", Column: domain.TaskColumnBacklog}},
				"work order: this task is blocked until these are done: T-7 [backlog]",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &Service{
					relations: &fixedBlockerStore{graphRelationStore: newGraphRelations(), blockers: tc.blockers},
					workflows: workflowtest.Default().Reader(),
				}
				err := svc.validateMoveAllowed(context.Background(), uuid.New(), "task", domain.TaskColumnInProgress)
				if err == nil || err.Error() != tc.want {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("ReviewTaskCriterion wrong column", func(t *testing.T) {
		cases := []struct {
			name string
			task domain.BoardTask
			want string
		}{
			{
				"in_progress",
				domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Key: "T-4", Column: domain.TaskColumnInProgress},
				"criterion verdicts are recorded while the task is under QA (ready_for_qa/in_qa) or PM UAT (pm_uat); task T-4 is in in_progress — do not move the task to reach the criteria: their ids are in your run context and in move refusals; if the task already left your column, stop and report instead of retrying",
			},
			{
				"done",
				domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Key: "T-9", Column: domain.TaskColumnDone},
				"criterion verdicts are recorded while the task is under QA (ready_for_qa/in_qa) or PM UAT (pm_uat); task T-9 is in done — do not move the task to reach the criteria: their ids are in your run context and in move refusals; if the task already left your column, stop and report instead of retrying",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				criteria := &fakeCriteriaStore{criterion: domain.AcceptanceCriterion{ID: uuid.New(), TaskID: tc.task.ID, Text: "a"}}
				svc := &Service{
					criteria:  criteria,
					tasks:     &criterionTaskStore{fakeReleaseTaskStore: &fakeReleaseTaskStore{task: tc.task}},
					workflows: workflowtest.Default().Reader(),
				}
				_, err := svc.ReviewTaskCriterion(context.Background(), criteria.criterion.ID, uuid.New(), true, "")
				if err == nil || err.Error() != tc.want {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("workflowSetupTaskBrief", func(t *testing.T) {
		cases := []struct {
			name string
			kind string
			want string
		}{
			{
				"backend",
				domain.RepoKindBackend,
				"Create and push GitHub Actions CI/CD workflows for this repository (kind: backend).\n\nRequired jobs (under .github/workflows):\n- validate: lint / static analysis / type checking\n- build: compilation (e.g. go build / docker build)\n- test: automated tests\n- stage_deploy: a workflow_dispatch-triggerable workflow that deploys to staging\n- preprod_deploy: (optional) a workflow_dispatch-triggerable workflow that deploys to a pre-production environment\n- prod_deploy: a workflow_dispatch-triggerable workflow that deploys to production\n\nOnce each workflow exists, save the job/workflow mapping under Repository Settings > Pipeline so the QA gate and release steps run through GitHub Actions.",
			},
			{
				"frontend",
				domain.RepoKindFrontend,
				"Create and push GitHub Actions CI/CD workflows for this repository (kind: frontend).\n\nRequired jobs (under .github/workflows):\n- validate: lint / static analysis / type checking\n- build: compilation (e.g. npm/pnpm build)\n- test: automated tests\n- stage_deploy: a workflow_dispatch-triggerable workflow that deploys to staging\n- preprod_deploy: (optional) a workflow_dispatch-triggerable workflow that deploys to a pre-production environment\n- prod_deploy: a workflow_dispatch-triggerable workflow that deploys to production\n\nOnce each workflow exists, save the job/workflow mapping under Repository Settings > Pipeline so the QA gate and release steps run through GitHub Actions.",
			},
			{
				"mobile",
				domain.RepoKindMobile,
				"Create and push GitHub Actions CI/CD workflows for this repository (kind: mobile).\n\nRequired jobs (under .github/workflows):\n- validate: lint / static analysis / type checking\n- build: compilation (e.g. xcodebuild/fastlane — do not use docker)\n- test: automated tests\n- stage_deploy: a workflow_dispatch-triggerable workflow that deploys to staging\n- preprod_deploy: (optional) a workflow_dispatch-triggerable workflow that deploys to a pre-production environment\n- prod_deploy: a workflow_dispatch-triggerable workflow that deploys to production\n\nOnce each workflow exists, save the job/workflow mapping under Repository Settings > Pipeline so the QA gate and release steps run through GitHub Actions.",
			},
			{
				"monorepo",
				domain.RepoKindMonorepo,
				"Create and push GitHub Actions CI/CD workflows for this repository (kind: monorepo).\n\nRequired jobs (under .github/workflows):\n- validate: lint / static analysis / type checking\n- build: compilation (a separate build per subproject (backend/frontend/mobile/worker/data/game))\n- test: automated tests\n- stage_deploy: a workflow_dispatch-triggerable workflow that deploys to staging\n- preprod_deploy: (optional) a workflow_dispatch-triggerable workflow that deploys to a pre-production environment\n- prod_deploy: a workflow_dispatch-triggerable workflow that deploys to production\n\nOnce each workflow exists, save the job/workflow mapping under Repository Settings > Pipeline so the QA gate and release steps run through GitHub Actions.",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := workflowSetupTaskBrief(tc.kind)
				if got != tc.want {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			})
		}
	})
}
