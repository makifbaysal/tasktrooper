package reviewhistory_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/reviewhistory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func traceOf(f *fixture, files ...string) domain.RunTrace {
	fin := at(35)
	changes := make([]domain.TraceFileChange, len(files))
	for i, p := range files {
		changes[i] = domain.TraceFileChange{Path: p, Status: "M"}
	}
	return domain.RunTrace{RunID: uuid.New(), TaskID: f.task.ID, AgentID: f.dev, FinishedAt: &fin,
		Verification: &domain.RunTraceVerification{Available: true, Files: changes}}
}

func flagsFor(t *testing.T, f *fixture, traces ...domain.RunTrace) []domain.ReviewFlag {
	return reviewhistory.Build(reviewhistory.Inputs{Task: f.task, Traces: traces, Rules: rules(t), Now: at(60)}).Flags
}

func codes(flags []domain.ReviewFlag) []string {
	var out []string
	for _, fl := range flags {
		out = append(out, fl.Code)
	}
	return out
}

func TestSensitivePathsAreFlaggedPerRule(t *testing.T) {
	f := newFixture()
	flags := flagsFor(t, f, traceOf(f, "server/auth/session.go", "server/auth/session_test.go", "db/migrations/003_x.sql", "README.md"))
	var reasons []string
	for _, fl := range flags {
		if fl.Code == domain.ReviewFlagSensitivePath {
			reasons = append(reasons, fl.Reason)
			assert.Equal(t, domain.ReviewFlagSourceAutomatic, fl.Source)
		}
	}
	assert.ElementsMatch(t, []string{"auth", "migrations"}, reasons)
}

func TestNothingSensitiveNothingFlagged(t *testing.T) {
	f := newFixture()
	assert.Empty(t, flagsFor(t, f, traceOf(f, "ui/Button.tsx", "ui/Button.test.tsx", "docs/guide.md")))
}

func TestVeryLargeDiffIsFlagged(t *testing.T) {
	f := newFixture()
	var files []string
	for i := 0; i < 41; i++ {
		files = append(files, fmt.Sprintf("pkg/f%d.go", i))
	}
	files = append(files, "pkg/f1_test.go")
	assert.Contains(t, codes(flagsFor(t, f, traceOf(f, files...))), domain.ReviewFlagLargeDiff)
	assert.NotContains(t, codes(flagsFor(t, f, traceOf(f, files[:10]...))), domain.ReviewFlagLargeDiff)
}

func TestChangedCodeWithoutTestsIsFlagged(t *testing.T) {
	f := newFixture()
	assert.Contains(t, codes(flagsFor(t, f, traceOf(f, "pkg/a.go", "pkg/b.go"))), domain.ReviewFlagTestsMissing)
	assert.NotContains(t, codes(flagsFor(t, f, traceOf(f, "pkg/a.go", "pkg/a_test.go"))), domain.ReviewFlagTestsMissing)
	assert.NotContains(t, codes(flagsFor(t, f, traceOf(f, "docs/a.md", "locales/en.json"))), domain.ReviewFlagTestsMissing)
}

func TestFilesChangedOutsideTheRecordedToolCallsAreFlagged(t *testing.T) {
	f := newFixture()
	tr := traceOf(f, "a.txt")
	tr.Verification.Unrecorded = []string{"gen/out.bin"}
	flags := flagsFor(t, f, tr)
	require.Contains(t, codes(flags), domain.ReviewFlagUnrecordedChanges)
	for _, fl := range flags {
		if fl.Code == domain.ReviewFlagUnrecordedChanges {
			assert.Equal(t, []string{"gen/out.bin"}, fl.Files)
		}
	}
}

func TestATraceGitCouldNotVerifyFlagsNothing(t *testing.T) {
	f := newFixture()
	tr := traceOf(f, "server/auth/x.go")
	tr.Verification.Available = false
	assert.Empty(t, flagsFor(t, f, tr))
}

func TestAQAApprovalWithoutEvidenceIsFlagged(t *testing.T) {
	f := newFixture()
	withProof, without, cancelled := uuid.New(), uuid.New(), uuid.New()
	approved := func(id uuid.UUID, text string) domain.AcceptanceCriterion {
		return domain.AcceptanceCriterion{ID: id, Text: text, Checks: []domain.CriterionCheck{{CriterionID: id, Role: domain.CriterionReviewRoleQA, Approved: true, CheckedAt: at(5)}}}
	}
	c := approved(cancelled, "cancelled one")
	c.Canceled = true
	flags := reviewhistory.Build(reviewhistory.Inputs{
		Task:     f.task,
		Criteria: []domain.AcceptanceCriterion{approved(withProof, "has proof"), approved(without, "no proof"), c},
		TestCases: []domain.TaskTestCase{
			{CriterionID: &withProof, Status: domain.TestCaseStatusPassed, Evidence: "curl -> 200"},
			{CriterionID: &without, Status: domain.TestCaseStatusPassed},
		},
		Rules: rules(t), Now: at(60),
	}).Flags
	require.Len(t, flags, 1)
	assert.Equal(t, domain.ReviewFlagQANoEvidence, flags[0].Code)
	assert.Equal(t, "no proof", flags[0].Reason)
}

func TestAgentFlagsAreOneFlagAcrossRoundsAndCarryReviewedState(t *testing.T) {
	f := newFixture()
	f.span("code_review", 10, 20)
	f.span("need_revision", 20, 30)
	f.span("code_review", 30, 0)
	flag := func(min int, q string) domain.TaskReviewFinding {
		r := f.finding(f.security, "security", "code_review", "", "pay/charge.go", 12, "could not run the payment path", min)
		r.Kind = domain.ReviewItemHumanReview
		r.HumanReviewReason = "touches payment logic I could not run"
		r.Question = q
		return r
	}
	first := flag(15, "does retry double charge?")
	second := flag(35, "does retry double charge, after the fix?")
	withdrawn := f.finding(f.architect, "architect", "code_review", "W", "x.go", 1, "gone", 36)
	withdrawn.HumanReviewReason = "unsure"
	withdrawn.Status = domain.ReviewFindingWithdrawn
	// a plain finding that carries a reason is a flag too
	inline := f.finding(f.architect, "architect", "code_review", "B", "api/rate.go", 7, "rate limit is a business rule", 36)
	inline.HumanReviewReason = "business rule not in the criteria"

	in := reviewhistory.Inputs{Task: f.task, Spans: f.spans, Findings: []domain.TaskReviewFinding{first, second, withdrawn, inline}, Rules: rules(t), Now: at(60), AgentName: names(f)}
	flags := reviewhistory.Build(in).Flags
	require.Len(t, flags, 2)
	assert.Equal(t, domain.ReviewFlagSourceAgent, flags[0].Source)
	assert.Equal(t, "security", flags[0].AgentName)
	assert.Equal(t, "does retry double charge, after the fix?", flags[0].Question)
	assert.Equal(t, at(15), flags[0].RaisedAt, "the first time it was raised")
	assert.Equal(t, 1, flags[0].Round)

	in.Acks = []domain.ReviewFlagAck{{TaskID: f.task.ID, FlagKey: flags[0].ID, ReviewedBy: "akif", ReviewedAt: at(50)}}
	h := reviewhistory.Build(in)
	require.Len(t, h.Flags, 2)
	assert.False(t, h.Flags[0].Reviewed(), "unreviewed flags come first")
	assert.True(t, h.Flags[1].Reviewed())
	assert.Equal(t, "akif", h.Flags[1].ReviewedBy)
	assert.Equal(t, 1, h.Summary.OpenFlags)
}

func TestRulesAreReadFromTheCatalogAndExtensible(t *testing.T) {
	r, err := reviewhistory.LoadRules(catalog.ReviewFS())
	require.NoError(t, err)
	got := r.SensitiveMatches([]string{".github/workflows/ci.yml", "Dockerfile", "infra/main.tf", "app/.env.production", "Sources/Auth/LoginAuth.swift"})
	assert.Contains(t, got, "ci")
	assert.Contains(t, got, "deploy")
	assert.Contains(t, got, "infra")
	assert.Contains(t, got, "security")
	assert.Contains(t, got, "auth")
	assert.Positive(t, r.LargeDiff.MaxFiles)
	_ = time.Now
}
