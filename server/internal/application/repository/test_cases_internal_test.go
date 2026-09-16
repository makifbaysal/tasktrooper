package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeTestCaseStore struct {
	items []domain.TaskTestCase
	saved []domain.TaskTestCaseInput
}

func (f *fakeTestCaseStore) ListByTask(context.Context, uuid.UUID) ([]domain.TaskTestCase, error) {
	return f.items, nil
}

func (f *fakeTestCaseStore) UpsertForTask(_ context.Context, _ uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	f.saved = append(f.saved, items...)
	return f.items, nil
}

func (f *fakeTestCaseStore) ReplaceForTask(_ context.Context, _ uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error) {
	f.saved = append(f.saved, items...)
	return f.items, nil
}

func (f *fakeTestCaseStore) Get(context.Context, uuid.UUID) (domain.TaskTestCase, error) {
	if len(f.items) == 0 {
		return domain.TaskTestCase{}, nil
	}
	return f.items[0], nil
}

func (f *fakeTestCaseStore) Update(_ context.Context, id uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error) {
	return domain.TaskTestCase{ID: id, Title: item.Title, Status: item.Status, Actual: item.Actual, Notes: item.Notes}, nil
}

func (f *fakeTestCaseStore) Delete(context.Context, uuid.UUID) error { return nil }

func (f *fakeTestCaseStore) MarkScored(context.Context, []uuid.UUID, time.Time) error { return nil }

func testCase(title string, status domain.TestCaseStatus) domain.TaskTestCase {
	return domain.TaskTestCase{ID: uuid.New(), Title: title, Status: status, Category: domain.TestCaseCategoryOther}
}

// The QA phase owes the board the round it ran, not only its verdicts. Without
// this gate a round that tested three obvious things and one that worked
// through boundaries, auth and regression left the same trace on the card.
func TestTestCaseGate(t *testing.T) {
	taskID := uuid.New()
	cases := []struct {
		name    string
		items   []domain.TaskTestCase
		prev    domain.TaskColumn
		target  domain.TaskColumn
		require bool
		wantErr string
	}{
		{
			name:    "no cases at all refuses the hand-off",
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnPMUAT,
			require: true,
			wantErr: "carries no test cases",
		},
		{
			name:    "a case written down but never executed refuses it too",
			items:   []domain.TaskTestCase{testCase("expired token is rejected", domain.TestCaseStatusPlanned)},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnPMUAT,
			require: true,
			wantErr: "expired token is rejected",
		},
		{
			name: "executed and rejected cases are both settled",
			items: []domain.TaskTestCase{
				testCase("happy path", domain.TestCaseStatusPassed),
				testCase("upload of 0 rows", domain.TestCaseStatusInvalid),
				testCase("on a real device", domain.TestCaseStatusSkipped),
			},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnPMUAT,
			require: true,
		},
		{
			name:    "a failing case does not block: its exit is need_revision, not this move",
			items:   []domain.TaskTestCase{testCase("happy path", domain.TestCaseStatusFailed)},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnPMUAT,
			require: true,
		},
		{
			name:    "the gate is QA's alone",
			prev:    domain.TaskColumnCodeReview,
			target:  domain.TaskColumnReadyForQA,
			require: true,
		},
		{
			name:    "moving back for revision is never gated",
			items:   []domain.TaskTestCase{testCase("happy path", domain.TestCaseStatusPlanned)},
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnNeedRevision,
			require: true,
		},
		{
			name:    "off without require_criteria_complete",
			prev:    domain.TaskColumnInQA,
			target:  domain.TaskColumnPMUAT,
			require: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{testCases: &fakeTestCaseStore{items: tc.items}, requireCriteria: tc.require}
			err := svc.testCaseGate(context.Background(), taskID, tc.prev, tc.target)
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

// Two cases with one title collapse into one row (the store matches by title),
// so a batch that repeats a title loses a case silently. Refusing says so.
func TestRecordTestCasesRefusesDuplicateTitles(t *testing.T) {
	svc := &Service{testCases: &fakeTestCaseStore{}}

	_, err := svc.RecordTestCases(context.Background(), uuid.New(), []domain.TaskTestCaseInput{
		{Title: "expired token", Status: domain.TestCaseStatusPassed},
		{Title: "Expired Token", Status: domain.TestCaseStatusFailed, Actual: "200"},
	})

	if err == nil || !strings.Contains(err.Error(), "share the title") {
		t.Fatalf("expected a duplicate-title refusal, got %v", err)
	}
}

// A verdict has to say what it rests on. These three refusals are the same rule
// from three sides — an unreproducible failure, an unauditable rejection, and a
// case reported unrun with nothing named as the blocker.
func TestTestCaseInputDemandsTheEvidenceItsVerdictImplies(t *testing.T) {
	cases := []struct {
		name    string
		in      domain.TaskTestCaseInput
		wantErr string
	}{
		{
			name:    "failed without an observation",
			in:      domain.TaskTestCaseInput{Title: "login", Status: domain.TestCaseStatusFailed},
			wantErr: "`actual` must say what was observed",
		},
		{
			name:    "invalid without a reason",
			in:      domain.TaskTestCaseInput{Title: "login", Status: domain.TestCaseStatusInvalid},
			wantErr: "must say why it is not a valid case",
		},
		{
			name:    "skipped without a blocker",
			in:      domain.TaskTestCaseInput{Title: "login", Status: domain.TestCaseStatusSkipped},
			wantErr: "must say what blocked it",
		},
		{
			name:    "no title",
			in:      domain.TaskTestCaseInput{Status: domain.TestCaseStatusPassed},
			wantErr: "title is required",
		},
		{
			name:    "a status the UI and the CHECK constraint do not know",
			in:      domain.TaskTestCaseInput{Title: "login", Status: "flaky"},
			wantErr: "invalid test case status",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.in.Normalize(); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	// And the happy path keeps its defaults: an unspecified case is planned and
	// uncategorised rather than refused.
	got, err := domain.TaskTestCaseInput{Title: "login"}.Normalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != domain.TestCaseStatusPlanned || got.Category != domain.TestCaseCategoryOther {
		t.Fatalf("expected planned/other defaults, got %s/%s", got.Status, got.Category)
	}
}
