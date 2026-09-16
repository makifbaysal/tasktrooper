package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TestCaseStatus is the verdict on one derived test case.
//
// Five values, and the two unusual ones carry the whole point of keeping this
// list on the card: `invalid` is a case that was thought of and rejected — it
// contradicts the spec, it is unreachable by design, it belongs to another
// task — and `skipped` is a valid case that could NOT be executed in this
// round (no device, no stage deploy, missing credential). Folding either into
// "passed" is the lie the list exists to prevent, and dropping them entirely
// would hide the reasoning that a reviewer most wants to audit.
type TestCaseStatus string

const (
	TestCaseStatusPlanned TestCaseStatus = "planned"
	TestCaseStatusPassed  TestCaseStatus = "passed"
	TestCaseStatusFailed  TestCaseStatus = "failed"
	TestCaseStatusSkipped TestCaseStatus = "skipped"
	TestCaseStatusInvalid TestCaseStatus = "invalid"
)

// TestCaseCategory is the kind of case, so a reader can see at a glance which
// dimensions a round actually covered instead of counting titles.
type TestCaseCategory string

const (
	TestCaseCategoryHappyPath  TestCaseCategory = "happy_path"
	TestCaseCategoryBoundary   TestCaseCategory = "boundary"
	TestCaseCategoryNegative   TestCaseCategory = "negative"
	TestCaseCategoryAuth       TestCaseCategory = "auth"
	TestCaseCategoryEmptyState TestCaseCategory = "empty_state"
	TestCaseCategoryRegression TestCaseCategory = "regression"
	TestCaseCategoryVisual     TestCaseCategory = "visual"
	TestCaseCategoryAsync      TestCaseCategory = "async"
	TestCaseCategoryOther      TestCaseCategory = "other"
)

// TestCaseStatuses and TestCaseCategories are the wire contract with the SQL
// CHECK constraints in migration 130 and with the tool schemas; adding a value
// means changing all three.
var TestCaseStatuses = []TestCaseStatus{
	TestCaseStatusPlanned, TestCaseStatusPassed, TestCaseStatusFailed,
	TestCaseStatusSkipped, TestCaseStatusInvalid,
}

var TestCaseCategories = []TestCaseCategory{
	TestCaseCategoryHappyPath, TestCaseCategoryBoundary, TestCaseCategoryNegative,
	TestCaseCategoryAuth, TestCaseCategoryEmptyState, TestCaseCategoryRegression,
	TestCaseCategoryVisual, TestCaseCategoryAsync, TestCaseCategoryOther,
}

// TaskTestCase is one case of a task's test round, stored on the task next to
// its acceptance criteria.
type TaskTestCase struct {
	ID     uuid.UUID `json:"id"`
	TaskID uuid.UUID `json:"task_id"`
	// CriterionID links the case to the acceptance criterion it exercises, when
	// there is one. The cases with no criterion are the ones this table was
	// built for: what the requirement implies but nobody wrote down.
	CriterionID *uuid.UUID       `json:"criterion_id,omitempty"`
	Title       string           `json:"title"`
	Category    TestCaseCategory `json:"category"`
	Status      TestCaseStatus   `json:"status"`
	Expected    string           `json:"expected,omitempty"`
	Actual      string           `json:"actual,omitempty"`
	// Evidence is what proves the verdict: the command and its output, the
	// screenshot path, the request/response pair.
	Evidence  string    `json:"evidence,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// ScoredAt marks the moment this case's verdict was already turned into a
	// performance score event, so a rework round or a later forward exit
	// never counts the same case twice.
	ScoredAt *time.Time `json:"scored_at,omitempty"`
}

type TaskTestCaseInput struct {
	CriterionID *uuid.UUID       `json:"criterion_id,omitempty"`
	Title       string           `json:"title"`
	Category    TestCaseCategory `json:"category,omitempty"`
	Status      TestCaseStatus   `json:"status,omitempty"`
	Expected    string           `json:"expected,omitempty"`
	Actual      string           `json:"actual,omitempty"`
	Evidence    string           `json:"evidence,omitempty"`
	Notes       string           `json:"notes,omitempty"`
	Position    int              `json:"position,omitempty"`
}

// Normalize trims the input, fills the defaults and refuses the combinations
// that would make the list unreadable.
//
// The three refusals are all the same rule from different sides: a verdict has
// to say what it is based on. A failure with no observed behaviour cannot be
// reproduced, a case rejected as invalid with no reason cannot be audited (and
// is indistinguishable from one quietly dropped), and a case that could not be
// run has to name what blocked it or it reads as untested-and-unexplained.
func (i TaskTestCaseInput) Normalize() (TaskTestCaseInput, error) {
	out := i
	out.Title = strings.TrimSpace(i.Title)
	out.Expected = strings.TrimSpace(i.Expected)
	out.Actual = strings.TrimSpace(i.Actual)
	out.Evidence = strings.TrimSpace(i.Evidence)
	out.Notes = strings.TrimSpace(i.Notes)
	if out.Title == "" {
		return out, fmt.Errorf("test case title is required")
	}
	if out.Category == "" {
		out.Category = TestCaseCategoryOther
	}
	if out.Status == "" {
		out.Status = TestCaseStatusPlanned
	}
	if !out.Category.Valid() {
		return out, fmt.Errorf("invalid test case category %q; use one of %s", out.Category, joinTestCaseCategories())
	}
	if !out.Status.Valid() {
		return out, fmt.Errorf("invalid test case status %q; use one of %s", out.Status, joinTestCaseStatuses())
	}
	switch out.Status {
	case TestCaseStatusFailed:
		if out.Actual == "" {
			return out, fmt.Errorf("test case %q is failed: `actual` must say what was observed", out.Title)
		}
	case TestCaseStatusInvalid:
		if out.Notes == "" {
			return out, fmt.Errorf("test case %q is invalid: `notes` must say why it is not a valid case", out.Title)
		}
	case TestCaseStatusSkipped:
		if out.Notes == "" {
			return out, fmt.Errorf("test case %q is skipped: `notes` must say what blocked it", out.Title)
		}
	}
	return out, nil
}

func (s TestCaseStatus) Valid() bool {
	for _, v := range TestCaseStatuses {
		if s == v {
			return true
		}
	}
	return false
}

func (c TestCaseCategory) Valid() bool {
	for _, v := range TestCaseCategories {
		if c == v {
			return true
		}
	}
	return false
}

// Executed reports whether this case was actually run in a round. Planned and
// skipped cases are not: they are the two ways a case ends up on the list
// without a result behind it.
func (s TestCaseStatus) Executed() bool {
	return s == TestCaseStatusPassed || s == TestCaseStatusFailed
}

func joinTestCaseStatuses() string {
	parts := make([]string, 0, len(TestCaseStatuses))
	for _, v := range TestCaseStatuses {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, ", ")
}

func joinTestCaseCategories() string {
	parts := make([]string, 0, len(TestCaseCategories))
	for _, v := range TestCaseCategories {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, ", ")
}

// TestCaseSummary counts a task's cases by status for the places that show the
// round at a glance — the board card, a move refusal, a run's context.
type TestCaseSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Invalid int `json:"invalid"`
	Planned int `json:"planned"`
}

func SummarizeTestCases(items []TaskTestCase) TestCaseSummary {
	var s TestCaseSummary
	s.Total = len(items)
	for _, c := range items {
		switch c.Status {
		case TestCaseStatusPassed:
			s.Passed++
		case TestCaseStatusFailed:
			s.Failed++
		case TestCaseStatusSkipped:
			s.Skipped++
		case TestCaseStatusInvalid:
			s.Invalid++
		default:
			s.Planned++
		}
	}
	return s
}
