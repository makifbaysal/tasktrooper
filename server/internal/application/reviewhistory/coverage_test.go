package reviewhistory_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/reviewhistory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func readEvent(path string) domain.RunTraceEvent {
	d, _ := json.Marshal(map[string]string{"path": path})
	return domain.RunTraceEvent{Tool: "read_file", Category: domain.TraceCategoryRead, Target: path, Status: domain.TraceStatusOK, Detail: d}
}

func execEvent(cmd, status, errText string) domain.RunTraceEvent {
	d, _ := json.Marshal(map[string]string{"command": cmd})
	return domain.RunTraceEvent{Tool: "run_terminal", Category: domain.TraceCategoryExec, Target: cmd, Status: status, Error: errText, Detail: d}
}

// patchOf builds the combined stat+patch text a review run is handed.
func patchOf(files ...string) string {
	var sb strings.Builder
	sb.WriteString(" stat header\n")
	for _, f := range files {
		sb.WriteString("\ndiff --git a/" + f + " b/" + f + "\n+changed\n")
	}
	return sb.String()
}

type covFixture struct {
	f       *fixture
	arch    uuid.UUID
	sec     uuid.UUID
	events  map[uuid.UUID][]domain.RunTraceEvent
	runs    []domain.TaskAgentRun
	verdict []domain.TaskReviewVerdict
	spans   []domain.TaskColumnSpan
}

func newCov(t *testing.T) (*covFixture, reviewhistory.Inputs) {
	f := newFixture()
	f.task.Column = domain.TaskColumnReadyForQA
	span := f.span("code_review", 10, 20)
	f.verdict(span, f.architect, "system-architect", domain.ReviewVerdictApprove, 19)
	f.verdict(span, f.security, "security-agent", domain.ReviewVerdictApprove, 18)
	f.move(20, "code_review", "ready_for_qa", "agent", nil)
	archRun := domain.TaskAgentRun{ID: uuid.New(), TaskID: f.task.ID, AgentID: f.architect, CreatedAt: at(11)}
	secRun := domain.TaskAgentRun{ID: uuid.New(), TaskID: f.task.ID, AgentID: f.security, CreatedAt: at(12)}
	fin := at(9)
	tr := traceOf(f, "svc/a.go", "svc/b.go", "svc/c.go", "ui/style.css", "gen/api.pb.go", "svc/big.go")
	tr.FinishedAt = &fin
	events := map[uuid.UUID][]domain.RunTraceEvent{
		archRun.ID: {readEvent("svc/a.go"), readEvent("/work/tree/svc/b.go"), execEvent("git diff origin/main -- svc/c.go", "ok", ""), execEvent("go test ./...", "error", "exit status 1: toolchain missing")},
		secRun.ID:  {readEvent("svc/a.go"), {Tool: "load_skill", Category: domain.TraceCategoryRead, Target: "security-diff-review-method", Status: "ok"}},
	}
	in := reviewhistory.Inputs{
		Task: f.task, Spans: f.spans, Verdicts: f.verdicts, Events: f.events, Runs: []domain.TaskAgentRun{archRun, secRun},
		Traces: []domain.RunTrace{tr}, AgentName: names(f), Rules: rules(t), Now: at(60),
		Diff: []reviewhistory.DiffFile{
			{Path: "svc/a.go", Added: 40, Deleted: 10}, {Path: "svc/b.go", Added: 20}, {Path: "svc/c.go", Added: 30},
			{Path: "ui/style.css", Added: 10}, {Path: "gen/api.pb.go", Added: 900}, {Path: "svc/big.go", Added: 2000},
		},
		Patch:     patchOf("svc/a.go", "svc/b.go", "ui/style.css"),
		RunEvents: func(id uuid.UUID) []domain.RunTraceEvent { return events[id] },
	}
	return &covFixture{f: f, arch: f.architect, sec: f.security, events: events}, in
}

func coverageOf(h domain.ReviewHistory, agent string) domain.ReviewerCoverage {
	for _, rv := range h.Rounds[0].Coverage.Reviewers {
		if rv.AgentName == agent {
			return rv
		}
	}
	return domain.ReviewerCoverage{}
}

func TestCoverageIsComputedFromTheDiffAndTheRecordedReads(t *testing.T) {
	_, in := newCov(t)
	h := reviewhistory.Build(in)
	cov := h.Rounds[0].Coverage
	require.NotNil(t, cov)
	assert.Equal(t, 6, cov.ChangedFiles)
	assert.Equal(t, 50+20+30+10+900+2000, cov.ChangedLines)

	arch := coverageOf(h, "system-architect")
	assert.True(t, arch.DiffKnown)
	assert.ElementsMatch(t, []string{"trace", "diff"}, arch.Basis)
	assert.Equal(t, 5, arch.FilesTotal, "the generated file is not counted against a reviewer")
	assert.Equal(t, 4, arch.FilesReviewed, "a, b opened; c via git diff; style.css in the diff it was handed")
	assert.Equal(t, 3, arch.FilesOpened)
	assert.Equal(t, 40+10+20+30+10, arch.LinesReviewed)
	assert.Equal(t, 40+10+20+30+10+2000, arch.LinesTotal)
	assert.Equal(t, []string{"svc/big.go"}, arch.UnreadFiles)
	assert.Equal(t, 1, arch.UnreadCount)
}

func TestWhatACheckCouldNotRunAndWhatWasSkippedIsListed(t *testing.T) {
	_, in := newCov(t)
	h := reviewhistory.Build(in)
	arch := coverageOf(h, "system-architect")
	byKind := map[string]domain.ReviewNotReviewed{}
	for _, n := range arch.NotReviewed {
		byKind[n.Kind] = n
	}
	assert.Equal(t, "run_terminal", byKind["check"].Scope)
	assert.Contains(t, byKind["check"].Reason, "toolchain missing")
	assert.Equal(t, []string{"gen/api.pb.go"}, byKind["skipped"].Files)
	assert.Equal(t, []string{"svc/big.go"}, byKind["large"].Files)
}

func TestAReviewersRoleLeavesFilesOutsideItUnreviewedByIt(t *testing.T) {
	_, in := newCov(t)
	h := reviewhistory.Build(in)
	sec := coverageOf(h, "security-agent")
	assert.Equal(t, []string{"security"}, sec.Aspects)
	var role domain.ReviewNotReviewed
	for _, n := range sec.NotReviewed {
		if n.Kind == "role" {
			role = n
		}
	}
	assert.Equal(t, []string{"ui/style.css"}, role.Files)
	assert.NotContains(t, sec.UnreadFiles, "ui/style.css", "outside its role is not an omission")
}

func TestFilesNoReviewerCoveredAreListedAndFlagged(t *testing.T) {
	_, in := newCov(t)
	h := reviewhistory.Build(in)
	var paths []string
	for _, f := range h.Rounds[0].Coverage.NobodyReviewed {
		paths = append(paths, f.Path)
	}
	assert.Equal(t, []string{"svc/big.go"}, paths)
	var gap *domain.ReviewFlag
	for i := range h.Flags {
		if h.Flags[i].Code == domain.ReviewFlagCoverageGap {
			gap = &h.Flags[i]
		}
	}
	require.NotNil(t, gap)
	assert.Equal(t, domain.ReviewFlagSourceAutomatic, gap.Source)
	assert.Equal(t, []string{"svc/big.go"}, gap.Files)
	assert.Contains(t, gap.FileURL, "github.com/acme/app/pull/7/files#diff-")
}

func TestWhatTheReviewerSaysItCouldNotVerifyTakesCoverageBack(t *testing.T) {
	cf, in := newCov(t)
	row := func(kind, scope, reason, msg string) domain.TaskReviewFinding {
		id := cf.arch
		return domain.TaskReviewFinding{ID: uuid.New(), TaskID: in.Task.ID, Kind: kind, Gate: "code_review", AgentID: &id, AgentName: "system-architect",
			Scope: scope, Reason: reason, Message: msg, Status: "open", CreatedAt: at(15), Fingerprint: "x" + scope + msg}
	}
	in.Findings = []domain.TaskReviewFinding{
		row(domain.ReviewItemNotReviewed, "svc/a.go", "could not run the migration", "svc/a.go"),
		row(domain.ReviewItemAspect, "", "", "correctness"),
	}
	h := reviewhistory.Build(in)
	arch := coverageOf(h, "system-architect")
	assert.Equal(t, 3, arch.FilesReviewed, "a.go no longer counts")
	assert.Contains(t, arch.Aspects, "correctness")
	assert.Contains(t, arch.Basis, "declared")
	var declared *domain.ReviewNotReviewed
	for i, n := range arch.NotReviewed {
		if n.Source == "declared" {
			declared = &arch.NotReviewed[i]
		}
	}
	require.NotNil(t, declared)
	assert.Equal(t, "svc/a.go", declared.Scope)
	assert.Equal(t, "could not run the migration", declared.Reason)
	// security still read a.go, so nobody-reviewed is unchanged
	assert.Len(t, h.Rounds[0].Coverage.NobodyReviewed, 1)

	// when security did not cover a.go either, nobody did
	in.Findings = append(in.Findings, func() domain.TaskReviewFinding {
		r := row(domain.ReviewItemNotReviewed, "svc/a.go", "unsure", "svc/a.go")
		id := cf.sec
		r.AgentID, r.AgentName = &id, "security-agent"
		return r
	}())
	h = reviewhistory.Build(in)
	var paths []string
	for _, f := range h.Rounds[0].Coverage.NobodyReviewed {
		paths = append(paths, f.Path)
	}
	assert.ElementsMatch(t, []string{"svc/a.go", "svc/big.go"}, paths)
}

func TestWithoutADiffOrTraceNothingIsClaimed(t *testing.T) {
	_, in := newCov(t)
	in.Patch, in.Diff, in.RunEvents = "", nil, nil
	h := reviewhistory.Build(in)
	arch := coverageOf(h, "system-architect")
	assert.False(t, arch.DiffKnown)
	assert.Zero(t, arch.FilesReviewed)
	assert.Empty(t, h.Rounds[0].Coverage.NobodyReviewed, "no diff, no claim about what nobody saw")
	for _, f := range h.Flags {
		assert.NotEqual(t, domain.ReviewFlagCoverageGap, f.Code)
	}
}

func TestAnEarlierRoundNeverClaimsTheDiffItWasHanded(t *testing.T) {
	f := newFixture()
	s1 := f.span("code_review", 10, 20)
	f.span("need_revision", 20, 30)
	s2 := f.span("code_review", 30, 40)
	f.verdict(s1, f.architect, "system-architect", "reject", 19)
	f.verdict(s2, f.architect, "system-architect", "approve", 39)
	in := reviewhistory.Inputs{
		Task: f.task, Spans: f.spans, Verdicts: f.verdicts, AgentName: names(f), Rules: rules(t), Now: at(60),
		Diff:  []reviewhistory.DiffFile{{Path: "a.go", Added: 3}},
		Patch: patchOf("a.go"),
	}
	h := reviewhistory.Build(in)
	assert.False(t, h.Rounds[0].Coverage.Reviewers[0].DiffKnown)
	assert.True(t, h.Rounds[1].Coverage.Reviewers[0].DiffKnown)
}

func TestInjectedDiffVisibilityFollowsTheByteCut(t *testing.T) {
	text := patchOf("one.go") + strings.Repeat("x", 100) + patchOf("two.go")
	files, truncated := reviewhistory.VisibleInInjectedDiff(text, 60)
	assert.Equal(t, []string{"one.go"}, files)
	assert.True(t, truncated)
	files, truncated = reviewhistory.VisibleInInjectedDiff(text, 10000)
	assert.Equal(t, []string{"one.go", "two.go"}, files)
	assert.False(t, truncated)
}

func TestQACoverageCountsCriteriaItChecked(t *testing.T) {
	f := newFixture()
	f.span("in_qa", 10, 20)
	qaRun := domain.TaskAgentRun{ID: uuid.New(), TaskID: f.task.ID, AgentID: f.qa, CreatedAt: at(11)}
	c1, c2 := uuid.New(), uuid.New()
	qid := f.qa
	row := f.finding(f.qa, "qa", "in_qa", "", "", 0, "login fails", 15)
	h := reviewhistory.Build(reviewhistory.Inputs{
		Task: f.task, Spans: f.spans, Runs: []domain.TaskAgentRun{qaRun}, Findings: []domain.TaskReviewFinding{row}, AgentName: names(f), Rules: rules(t), Now: at(60),
		Criteria: []domain.AcceptanceCriterion{
			{ID: c1, Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, AgentID: &qid, Approved: true}}},
			{ID: c2},
		},
		RunEvents: func(uuid.UUID) []domain.RunTraceEvent {
			return []domain.RunTraceEvent{execEvent("npm start", "error", "port in use")}
		},
	})
	cov := h.Rounds[0].Coverage.Reviewers[0]
	assert.Equal(t, 2, cov.CriteriaTotal)
	assert.Equal(t, 1, cov.CriteriaChecked)
	assert.Equal(t, "run_terminal", cov.NotReviewed[0].Scope)
}
