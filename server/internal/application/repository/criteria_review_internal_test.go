package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeCriteriaStore struct {
	items     []domain.AcceptanceCriterion
	criterion domain.AcceptanceCriterion
}

func (f *fakeCriteriaStore) ReplaceForTask(ctx context.Context, taskID uuid.UUID, items []domain.AcceptanceCriterionInput) ([]domain.AcceptanceCriterion, error) {
	return nil, nil
}
func (f *fakeCriteriaStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	return f.items, nil
}
func (f *fakeCriteriaStore) UpdateCompleted(ctx context.Context, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error) {
	return domain.AcceptanceCriterion{}, nil
}
func (f *fakeCriteriaStore) UpdateCanceled(ctx context.Context, criterionID uuid.UUID, canceled bool, reason string) (domain.AcceptanceCriterion, error) {
	return domain.AcceptanceCriterion{ID: criterionID, Canceled: canceled, CancelReason: reason}, nil
}
func (f *fakeCriteriaStore) GetCriterion(ctx context.Context, criterionID uuid.UUID) (domain.AcceptanceCriterion, error) {
	return f.criterion, nil
}
func (f *fakeCriteriaStore) UpsertCheck(ctx context.Context, check domain.CriterionCheck) (domain.CriterionCheck, error) {
	return check, nil
}

func criterion(text string, checks ...domain.CriterionCheck) domain.AcceptanceCriterion {
	return domain.AcceptanceCriterion{ID: uuid.New(), Text: text, Completed: true, Checks: checks}
}

func approved(role domain.CriterionReviewRole) domain.CriterionCheck {
	return domain.CriterionCheck{Role: role, Approved: true}
}

func rejected(role domain.CriterionReviewRole, note string) domain.CriterionCheck {
	return domain.CriterionCheck{Role: role, Approved: false, Note: note}
}

func TestCriteriaReviewGate(t *testing.T) {
	taskID := uuid.New()

	cases := []struct {
		name    string
		items   []domain.AcceptanceCriterion
		prev    domain.TaskColumn
		target  domain.TaskColumn
		require bool
		wantErr string
	}{
		{
			name:    "in_qa to human_uat with all QA approvals passes",
			items:   []domain.AcceptanceCriterion{criterion("a", approved(domain.CriterionReviewRoleQA)), criterion("b", approved(domain.CriterionReviewRoleQA))},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnHumanUAT,
			require: true,
		},
		{
			name:    "ready_for_qa to human_uat without QA verdict blocks",
			items:   []domain.AcceptanceCriterion{criterion("a")},
			prev:    domain.TaskColumnReadyForQA,
			target:  domain.TaskColumnHumanUAT,
			require: true,
			wantErr: "await your qa verdict",
		},
		{
			name:    "QA rejection blocks human_uat and names the note",
			items:   []domain.AcceptanceCriterion{criterion("login works", rejected(domain.CriterionReviewRoleQA, "500 on submit"))},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnHumanUAT,
			require: true,
			wantErr: "500 on submit",
		},
		{
			name:    "move to need_revision is never gated",
			items:   []domain.AcceptanceCriterion{criterion("a", rejected(domain.CriterionReviewRoleQA, "broken"))},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnNeedRevision,
			require: true,
		},
		{
			name:    "in_qa to done without QA verdicts blocks",
			items:   []domain.AcceptanceCriterion{criterion("android link is on the page")},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnDone,
			require: true,
			wantErr: "await your qa verdict",
		},
		{
			name:    "in_qa to human_uat without QA verdicts blocks",
			items:   []domain.AcceptanceCriterion{criterion("a")},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnHumanUAT,
			require: true,
			wantErr: "await your qa verdict",
		},
		{
			name:    "in_qa to released without QA verdicts blocks",
			items:   []domain.AcceptanceCriterion{criterion("a")},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnReleased,
			require: true,
			wantErr: "await your qa verdict",
		},
		{
			name:    "in_qa to done with every QA verdict passes",
			items:   []domain.AcceptanceCriterion{criterion("a", approved(domain.CriterionReviewRoleQA))},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnDone,
			require: true,
		},
		{

			name:    "in_qa back to in_progress is not gated",
			items:   []domain.AcceptanceCriterion{criterion("a")},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnInProgress,
			require: true,
		},
		{
			name:    "gate is off without require_criteria_complete",
			items:   []domain.AcceptanceCriterion{criterion("a")},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnHumanUAT,
			require: false,
		},
		{
			name:    "no criteria means nothing to gate",
			items:   nil,
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnHumanUAT,
			require: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{
				criteria:        &fakeCriteriaStore{items: tc.items},
				requireCriteria: tc.require,
				workflows:       workflowtest.Default().Reader(),
			}
			err := svc.criteriaReviewGate(context.Background(), taskID, "task", tc.prev, tc.target)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected pass, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func untickedCriterion(text string, checks ...domain.CriterionCheck) domain.AcceptanceCriterion {
	return domain.AcceptanceCriterion{ID: uuid.New(), Text: text, Completed: false, Checks: checks}
}

func TestCriteriaGateAcceptsAReviewerApprovalInPlaceOfTheImplementerTick(t *testing.T) {
	taskID := uuid.New()

	cases := []struct {
		name    string
		items   []domain.AcceptanceCriterion
		target  domain.TaskColumn
		wantErr string
	}{
		{
			name:   "QA approval satisfies the gate without the tick",
			items:  []domain.AcceptanceCriterion{untickedCriterion("a", approved(domain.CriterionReviewRoleQA))},
			target: domain.TaskColumnDone,
		},
		{
			name:   "the implementer tick alone still passes",
			items:  []domain.AcceptanceCriterion{criterion("a")},
			target: domain.TaskColumnReadyForQA,
		},
		{

			name:    "a rejection does not satisfy the gate",
			items:   []domain.AcceptanceCriterion{untickedCriterion("a", rejected(domain.CriterionReviewRoleQA, "500 on submit"))},
			target:  domain.TaskColumnDone,
			wantErr: "acceptance criteria incomplete",
		},
		{
			name:    "neither tick nor verdict blocks",
			items:   []domain.AcceptanceCriterion{untickedCriterion("a")},
			target:  domain.TaskColumnCodeReview,
			wantErr: "acceptance criteria incomplete",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{
				criteria:        &fakeCriteriaStore{items: tc.items},
				requireCriteria: true,
				workflows:       workflowtest.Default().Reader(),
			}
			err := svc.criteriaGate(context.Background(), taskID, "task", tc.target)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected pass, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}

			if !strings.Contains(err.Error(), "review_criterion") {
				t.Fatalf("block message must name review_criterion for reviewers, got %v", err)
			}
		})
	}
}

func TestReviewTaskCriterionRequiresNoteOnRejection(t *testing.T) {
	svc := &Service{criteria: &fakeCriteriaStore{}}
	_, err := svc.ReviewTaskCriterion(context.Background(), uuid.New(), uuid.New(), false, "   ")
	if err == nil || !strings.Contains(err.Error(), "note") {
		t.Fatalf("expected note-required error, got %v", err)
	}
}

var _ interface {
	ListTaskCriteria(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error)
} = (*Service)(nil)

type criterionTaskStore struct {
	*fakeReleaseTaskStore
}

func (c *criterionTaskStore) ListAll(context.Context) ([]domain.BoardTask, error) {
	return []domain.BoardTask{c.task}, nil
}

func TestCriteriaRefusalsCarryCriterionIDs(t *testing.T) {
	taskID := uuid.New()

	t.Run("criteriaGate", func(t *testing.T) {
		item := untickedCriterion("android link is on the page")
		svc := &Service{
			criteria:        &fakeCriteriaStore{items: []domain.AcceptanceCriterion{item}},
			requireCriteria: true,
			workflows:       workflowtest.Default().Reader(),
		}

		err := svc.criteriaGate(context.Background(), taskID, "task", domain.TaskColumnReadyForQA)

		if err == nil {
			t.Fatal("expected the gate to block")
		}
		if want := "[" + item.ID.String() + "] android link is on the page"; !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in %v", want, err)
		}
	})

	t.Run("awaiting verdicts names the ids and the next call", func(t *testing.T) {
		item := criterion("android link is on the page")
		svc := &Service{
			criteria:        &fakeCriteriaStore{items: []domain.AcceptanceCriterion{item}},
			requireCriteria: true,
			workflows:       workflowtest.Default().Reader(),
		}

		err := svc.criteriaReviewGate(context.Background(), taskID, "task", domain.TaskColumnInQA, domain.TaskColumnDone)

		if err == nil {
			t.Fatal("expected the gate to block")
		}
		if want := "[" + item.ID.String() + "] android link is on the page"; !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in %v", want, err)
		}

		if !strings.HasSuffix(err.Error(), "— call review_criterion with each id above, then retry the move") {
			t.Fatalf("expected the retry instruction to close the message, got %v", err)
		}
	})

	t.Run("rejected criteria are named by id too", func(t *testing.T) {
		item := criterion("login works", rejected(domain.CriterionReviewRoleQA, "500 on submit"))
		svc := &Service{
			criteria:        &fakeCriteriaStore{items: []domain.AcceptanceCriterion{item}},
			requireCriteria: true,
			workflows:       workflowtest.Default().Reader(),
		}

		err := svc.criteriaReviewGate(context.Background(), taskID, "task", domain.TaskColumnInQA, domain.TaskColumnHumanUAT)

		if err == nil {
			t.Fatal("expected the gate to block")
		}
		if want := "[" + item.ID.String() + "] login works (500 on submit)"; !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in %v", want, err)
		}
	})
}

func TestReviewTaskCriterionStampsTheCurrentHeadSHA(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Key: "T-9", Column: domain.TaskColumnReadyForQA}
	criteria := &fakeCriteriaStore{criterion: domain.AcceptanceCriterion{ID: uuid.New(), TaskID: task.ID, Text: "a"}}
	svc := &Service{
		criteria:      criteria,
		tasks:         &criterionTaskStore{fakeReleaseTaskStore: &fakeReleaseTaskStore{task: task}},
		git:           &fakeReleaseGit{hasGit: true, headSHA: "abc123"},
		workspaceRoot: t.TempDir(),
		workflows:     workflowtest.Default().Reader(),
	}

	check, err := svc.ReviewTaskCriterion(context.Background(), criteria.criterion.ID, uuid.New(), true, "")
	if err != nil {
		t.Fatalf("expected the verdict to be recorded, got %v", err)
	}
	if check.VerifiedSHA != "abc123" {
		t.Fatalf("expected the verdict stamped with the task's current HEAD, got %q", check.VerifiedSHA)
	}
}

func TestReviewTaskCriterionToleratesNoGitClient(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Key: "T-9", Column: domain.TaskColumnReadyForQA}
	criteria := &fakeCriteriaStore{criterion: domain.AcceptanceCriterion{ID: uuid.New(), TaskID: task.ID, Text: "a"}}
	svc := &Service{
		criteria:  criteria,
		tasks:     &criterionTaskStore{fakeReleaseTaskStore: &fakeReleaseTaskStore{task: task}},
		workflows: workflowtest.Default().Reader(),
	}

	check, err := svc.ReviewTaskCriterion(context.Background(), criteria.criterion.ID, uuid.New(), true, "")
	if err != nil {
		t.Fatalf("expected the verdict to be recorded, got %v", err)
	}
	if check.VerifiedSHA != "" {
		t.Fatalf("expected no SHA without a git client, got %q", check.VerifiedSHA)
	}
}

func TestReviewTaskCriterionRefusalForbidsMovingTheTaskToReachIt(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Key: "T-4", Column: domain.TaskColumnInProgress}
	criteria := &fakeCriteriaStore{criterion: domain.AcceptanceCriterion{ID: uuid.New(), TaskID: task.ID, Text: "a"}}
	svc := &Service{
		criteria:  criteria,
		tasks:     &criterionTaskStore{fakeReleaseTaskStore: &fakeReleaseTaskStore{task: task}},
		workflows: workflowtest.Default().Reader(),
	}

	_, err := svc.ReviewTaskCriterion(context.Background(), criteria.criterion.ID, uuid.New(), true, "")

	if err == nil {
		t.Fatal("expected the verdict to be refused outside the review columns")
	}
	for _, want := range []string{
		"task T-4 is in in_progress",
		"do not move the task to reach the criteria",
		"stop and report instead of retrying",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in %v", want, err)
		}
	}
}
