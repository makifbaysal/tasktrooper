package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestFingerprintSurvivesRewordingLineMovesAndCase(t *testing.T) {
	a := domain.ReviewFingerprint("code_review", "", "./api/handler.go", "Correctness", "Missing nil check on line 42 of the handler.")
	b := domain.ReviewFingerprint("code_review", "", "api/handler.go", "correctness", "missing NIL check on line 57 of the handler")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, domain.ReviewFingerprint("code_review", "", "api/other.go", "correctness", "missing nil check on line 42 of the handler"))
	assert.NotEqual(t, a, domain.ReviewFingerprint("in_qa", "", "api/handler.go", "correctness", "missing nil check on line 42 of the handler"))
}

func TestAReviewersOwnKeyWinsOverTheWording(t *testing.T) {
	a := domain.ReviewFingerprint("code_review", "SEC-1", "a.go", "security", "token compared with ==")
	b := domain.ReviewFingerprint("code_review", "sec-1", "b.go", "security", "a different sentence entirely")
	assert.Equal(t, a, b)
}

func TestAReportCannotFlagEverything(t *testing.T) {
	r := domain.ReviewReportInput{NeedsHumanReview: make([]domain.HumanReviewInput, 6)}
	for i := range r.NeedsHumanReview {
		r.NeedsHumanReview[i].Reason = "unsure"
	}
	err := r.Validate()
	require.ErrorIs(t, err, domain.ErrReviewReportInvalid)

	mixed := domain.ReviewReportInput{
		Findings:         []domain.ReviewFindingInput{{Message: "a", HumanReviewReason: "r"}, {Message: "b", HumanReviewReason: "r"}, {Message: "c", HumanReviewReason: "r"}},
		NeedsHumanReview: []domain.HumanReviewInput{{Reason: "x"}, {Reason: "y"}, {Reason: "z"}},
	}
	assert.ErrorIs(t, mixed.Validate(), domain.ErrReviewReportInvalid, "flags on findings count toward the same limit")
}

func TestReportValidation(t *testing.T) {
	for name, r := range map[string]domain.ReviewReportInput{
		"empty":            {},
		"no message":       {Findings: []domain.ReviewFindingInput{{File: "a.go"}}},
		"bad severity":     {Findings: []domain.ReviewFindingInput{{Message: "m", Severity: "scary"}}},
		"bad confidence":   {Findings: []domain.ReviewFindingInput{{Message: "m", Confidence: "sure"}}},
		"bad status":       {Findings: []domain.ReviewFindingInput{{Message: "m", Status: "fixed"}}},
		"negative line":    {Findings: []domain.ReviewFindingInput{{Message: "m", Line: -1}}},
		"flag no reason":   {NeedsHumanReview: []domain.HumanReviewInput{{File: "a.go"}}},
		"not reviewed bad": {NotReviewed: []domain.NotReviewedInput{{Scope: "web/"}}},
		"long message":     {Findings: []domain.ReviewFindingInput{{Message: strings.Repeat("x", 801)}}},
	} {
		r := r
		assert.ErrorIs(t, r.Validate(), domain.ErrReviewReportInvalid, name)
	}
}

func TestAValidReportBecomesRowsOfEachKind(t *testing.T) {
	r := domain.ReviewReportInput{
		Findings:         []domain.ReviewFindingInput{{Key: "K", File: "./a.go", Line: 2, Severity: "HIGH", Message: " bad ", Confidence: "Low"}},
		NeedsHumanReview: []domain.HumanReviewInput{{File: "p.go", Reason: "payments"}},
		NotReviewed:      []domain.NotReviewedInput{{Scope: "web/", Reason: "no browser"}},
		ReviewedAspects:  []string{" Security "},
	}
	require.NoError(t, r.Validate())
	agent := uuid.New()
	rows := r.Rows(uuid.New(), "code_review", &agent, "arch", "sha1", time.Unix(0, 0))
	require.Len(t, rows, 4)
	assert.Equal(t, "finding", rows[0].Kind)
	assert.Equal(t, "a.go", rows[0].File)
	assert.Equal(t, "high", rows[0].Severity)
	assert.Equal(t, "low", rows[0].Confidence)
	assert.Equal(t, "bad", rows[0].Message)
	assert.Equal(t, "open", rows[0].Status)
	assert.Equal(t, "human_review", rows[1].Kind)
	assert.Equal(t, "not_reviewed", rows[2].Kind)
	assert.Equal(t, "web/", rows[2].Scope)
	assert.Equal(t, "aspect", rows[3].Kind)
	assert.Equal(t, "security", rows[3].Message)
}

func TestFileURLPointsAtTheLineInTheGitHubDiff(t *testing.T) {
	u := domain.ReviewFileURL("https://github.com/acme/app/pull/7", "svc/a.go", 12)
	assert.Regexp(t, `^https://github.com/acme/app/pull/7/files#diff-[0-9a-f]{64}R12$`, u)
	assert.NotContains(t, domain.ReviewFileURL("https://github.com/acme/app/pull/7", "svc/a.go", 0), "R")
	assert.Empty(t, domain.ReviewFileURL("https://gitlab.com/acme/app/-/merge_requests/7", "a.go", 1))
	assert.Empty(t, domain.ReviewFileURL("", "a.go", 1))
	assert.Empty(t, domain.ReviewFileURL("https://github.com/acme/app/pull/7", "", 1))
}
