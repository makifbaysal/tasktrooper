package reviewhistory_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/reviewhistory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var t0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

type fixture struct {
	task      domain.BoardTask
	spans     []domain.TaskColumnSpan
	events    []domain.BoardEvent
	verdicts  []domain.TaskReviewVerdict
	architect uuid.UUID
	security  uuid.UUID
	dev       uuid.UUID
	qa        uuid.UUID
}

func newFixture() *fixture {
	return &fixture{
		task:      domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, PRURL: "https://github.com/acme/app/pull/7"},
		architect: uuid.New(), security: uuid.New(), dev: uuid.New(), qa: uuid.New(),
	}
}

func (f *fixture) span(col string, enter int, leave int) uuid.UUID {
	sp := domain.TaskColumnSpan{ID: uuid.New(), TaskID: f.task.ID, BoardColumn: col, EnteredAt: at(enter), VisitNo: 1}
	if leave > 0 {
		l := at(leave)
		sp.LeftAt = &l
	}
	f.spans = append(f.spans, sp)
	return sp.ID
}

func (f *fixture) move(min int, from, to, actor string, extra map[string]interface{}) {
	p := map[string]interface{}{"from_column": from, "to_column": to, "actor": actor}
	for k, v := range extra {
		p[k] = v
	}
	raw, _ := json.Marshal(p)
	f.events = append(f.events, domain.BoardEvent{ID: uuid.New(), TaskID: f.task.ID, EventType: domain.BoardEventTaskMoved, Payload: raw, CreatedAt: at(min)})
}

func (f *fixture) verdict(span uuid.UUID, agent uuid.UUID, name, v string, min int) {
	id := agent
	f.verdicts = append(f.verdicts, domain.TaskReviewVerdict{TaskID: f.task.ID, SpanID: span, AgentID: &id, AgentName: name, Verdict: v, DecidedAt: at(min)})
}

func (f *fixture) finding(agent uuid.UUID, name, gate, key, file string, line int, msg string, min int, extra ...func(*domain.TaskReviewFinding)) domain.TaskReviewFinding {
	id := agent
	r := domain.TaskReviewFinding{
		ID: uuid.New(), TaskID: f.task.ID, Kind: domain.ReviewItemFinding, Gate: gate, AgentID: &id, AgentName: name,
		Fingerprint: domain.ReviewFingerprint(gate, key, file, "bug", msg), File: file, Line: line, Severity: "high",
		Category: "bug", Message: msg, Status: domain.ReviewFindingOpen, CreatedAt: at(min),
	}
	for _, e := range extra {
		e(&r)
	}
	return r
}

func rules(t *testing.T) *reviewhistory.Rules {
	r, err := reviewhistory.LoadRules(catalog.ReviewFS())
	require.NoError(t, err)
	return r
}

func names(f *fixture) func(uuid.UUID) string {
	m := map[uuid.UUID]string{f.architect: "architect", f.security: "security", f.dev: "dev", f.qa: "qa"}
	return func(id uuid.UUID) string { return m[id] }
}

// A task that was sent back twice by code review (one finding carried, one
// fixed, one new), then failed QA once and passed.
func loopFixture(t *testing.T) (reviewhistory.Inputs, *fixture) {
	f := newFixture()
	s1 := f.span("code_review", 10, 20)
	f.span("need_revision", 20, 21)
	f.span("in_progress", 21, 40)
	s2 := f.span("code_review", 40, 50)
	f.span("ready_for_qa", 50, 55)
	f.span("in_qa", 55, 70)
	f.span("need_revision", 70, 71)
	f.span("in_progress", 71, 80)
	f.span("in_qa", 80, 0)

	f.move(10, "in_progress", "code_review", "agent", nil)
	f.move(20, "code_review", "need_revision", "agent", map[string]interface{}{"actor_agent_id": f.architect.String()})
	f.move(21, "need_revision", "in_progress", "system", nil)
	f.move(40, "in_progress", "code_review", "agent", nil)
	f.move(50, "code_review", "ready_for_qa", "agent", map[string]interface{}{"actor_agent_id": f.architect.String()})
	f.move(55, "ready_for_qa", "in_qa", "system", nil)
	f.move(70, "in_qa", "need_revision", "agent", map[string]interface{}{"actor_agent_id": f.qa.String()})
	f.move(80, "in_progress", "in_qa", "agent", nil)

	f.verdict(s1, f.architect, "architect", domain.ReviewVerdictReject, 19)
	f.verdict(s1, f.security, "security", domain.ReviewVerdictApprove, 18)
	f.verdict(s2, f.architect, "architect", domain.ReviewVerdictApprove, 49)
	f.verdict(s2, f.security, "security", domain.ReviewVerdictApprove, 48)

	crit := uuid.New()
	findings := []domain.TaskReviewFinding{
		f.finding(f.architect, "architect", "code_review", "ARCH-1", "api/handler.go", 42, "missing nil check", 19, func(r *domain.TaskReviewFinding) { r.HeadSHA = "aaa" }),
		f.finding(f.architect, "architect", "code_review", "ARCH-2", "api/handler.go", 90, "error swallowed", 19),
		f.finding(f.security, "security", "code_review", "SEC-1", "auth/token.go", 10, "token compared with ==", 18),
		// round 2: the architect repeats ARCH-2 (still open) and raises ARCH-3, drops ARCH-1; security drops SEC-1
		f.finding(f.architect, "architect", "code_review", "ARCH-2", "api/handler.go", 91, "error swallowed", 49),
		f.finding(f.architect, "architect", "code_review", "ARCH-3", "api/new.go", 5, "unbounded loop", 49),
		// QA fails the criterion in its first round, passes it in the second
		f.finding(f.qa, "qa", "in_qa", "", "", 0, "login returns 500", 69, func(r *domain.TaskReviewFinding) {
			r.Category = domain.ReviewCategoryAcceptanceCriterion
			r.CriterionID = &crit
			r.Fingerprint = domain.CriterionFingerprint(crit)
		}),
		f.finding(f.qa, "qa", "in_qa", "", "", 0, "", 90, func(r *domain.TaskReviewFinding) {
			r.Category = domain.ReviewCategoryAcceptanceCriterion
			r.CriterionID = &crit
			r.Fingerprint = domain.CriterionFingerprint(crit)
			r.Status = domain.ReviewFindingPassed
		}),
	}
	runs := []domain.TaskAgentRun{
		{ID: uuid.New(), TaskID: f.task.ID, AgentID: f.dev, Status: "completed", Summary: "fixed nil check", CreatedAt: at(22)},
		{ID: uuid.New(), TaskID: f.task.ID, AgentID: f.architect, Status: "completed", CreatedAt: at(11)},
		{ID: uuid.New(), TaskID: f.task.ID, AgentID: f.dev, Status: "completed", CreatedAt: at(72)},
	}
	fin := at(35)
	traces := []domain.RunTrace{{
		RunID: runs[0].ID, TaskID: f.task.ID, AgentID: f.dev, BaseSHA: "aaa", HeadSHA: "bbb", FinishedAt: &fin,
		Verification: &domain.RunTraceVerification{Available: true, Files: []domain.TraceFileChange{{Path: "api/handler.go"}, {Path: "auth/token.go"}}},
	}}
	return reviewhistory.Inputs{
		Task: f.task, Spans: f.spans, Verdicts: f.verdicts, Events: f.events, Runs: runs, Traces: traces, Findings: findings,
		Criteria:  []domain.AcceptanceCriterion{{ID: crit, TaskID: f.task.ID, Text: "user can log in"}},
		AgentName: names(f), Rules: rules(t), Now: at(100),
	}, f
}

func byID(h domain.ReviewHistory, gate, key string) domain.ReviewHistoryFinding {
	for _, f := range h.Findings {
		if f.ID == domain.ReviewFingerprint(gate, key, "", "", "") {
			return f
		}
	}
	return domain.ReviewHistoryFinding{}
}

func TestHistoryTellsTheWholeLoop(t *testing.T) {
	in, f := loopFixture(t)
	h := reviewhistory.Build(in)

	require.Len(t, h.Rounds, 4)
	r1, r2, r3, r4 := h.Rounds[0], h.Rounds[1], h.Rounds[2], h.Rounds[3]
	assert.Equal(t, []string{"code_review", "code_review", "in_qa", "in_qa"}, []string{r1.Gate, r2.Gate, r3.Gate, r4.Gate})
	assert.Equal(t, []int{1, 2, 3, 4}, []int{r1.Seq, r2.Seq, r3.Seq, r4.Seq})
	assert.Equal(t, 2, r4.Number)
	assert.Equal(t, "changes_requested", r1.Outcome)
	assert.Equal(t, "approved", r2.Outcome)
	assert.Equal(t, "failed", r3.Outcome)
	assert.Equal(t, "open", r4.Outcome)

	require.Len(t, r1.Reviewers, 2)
	for _, rv := range r1.Reviewers {
		if rv.AgentName == "architect" {
			assert.Equal(t, domain.ReviewVerdictReject, rv.Verdict)
			assert.NotNil(t, rv.RunID, "the reviewer's own run is linked")
		}
	}

	require.Len(t, r1.Moves, 3)
	assert.True(t, r1.Moves[1].Bounce)
	assert.Equal(t, f.architect, *r1.Moves[1].AgentID)
	require.Len(t, r1.FixRuns, 1)
	assert.Equal(t, "dev", r1.FixRuns[0].AgentName)
	assert.Equal(t, "bbb", r1.FixRuns[0].HeadSHA)
	assert.Equal(t, 2, r1.FixRuns[0].FilesChanged)
	assert.Empty(t, r2.FixRuns)
	require.Len(t, r3.FixRuns, 1)
	assert.False(t, r3.FixRuns[0].HasTrace)

	assert.Equal(t, "aaa", r1.HeadSHA)
	assert.Equal(t, "aaa", r2.FromSHA)
	assert.Equal(t, "bbb", r2.HeadSHA)

	arch1, arch2, arch3 := byID(h, "code_review", "ARCH-1"), byID(h, "code_review", "ARCH-2"), byID(h, "code_review", "ARCH-3")
	assert.Equal(t, domain.ReviewStateFixed, arch1.Status)
	assert.Equal(t, 2, arch1.FixedRound)
	assert.Equal(t, domain.ReviewStateOpen, arch2.Status, "still reported in round 2")
	assert.Equal(t, 1, arch2.RaisedRound)
	assert.Equal(t, 91, arch2.Line, "the latest report's line")
	assert.Equal(t, domain.ReviewStateOpen, arch3.Status)
	assert.Equal(t, 2, arch3.RaisedRound)
	assert.Equal(t, domain.ReviewStateFixed, byID(h, "code_review", "SEC-1").Status, "security approved without repeating it")
	assert.Regexp(t, `^https://github.com/acme/app/pull/7/files#diff-[0-9a-f]{64}R42$`, arch1.FileURL)

	assert.ElementsMatch(t, []string{arch1.ID, byID(h, "code_review", "SEC-1").ID}, r2.Fixed)
	assert.Equal(t, []string{arch2.ID}, r2.StillOpen)
	assert.Equal(t, []string{arch3.ID}, r2.Raised)

	var qa domain.ReviewHistoryFinding
	for _, fd := range h.Findings {
		if fd.Category == domain.ReviewCategoryAcceptanceCriterion {
			qa = fd
		}
	}
	assert.Equal(t, domain.ReviewStateFixed, qa.Status)
	assert.Equal(t, "user can log in", qa.CriterionText)
	assert.Equal(t, "login returns 500", qa.Message)
	assert.Equal(t, 3, qa.RaisedRound)
	assert.Equal(t, 4, qa.FixedRound)

	assert.Equal(t, domain.ReviewHistorySummary{
		Rounds: 4, FindingsRaised: 5, Fixed: 3, Open: 2, Bounces: 2, QAFailures: 1, OpenFlags: h.Summary.OpenFlags,
	}, h.Summary)
}

func TestAFindingTheReviewerNeverSpokeAboutAgainStaysOpen(t *testing.T) {
	in, f := loopFixture(t)
	// round 2 has no verdict and no report from the security reviewer
	var kept []domain.TaskReviewVerdict
	for _, v := range in.Verdicts {
		if !(v.AgentName == "security" && v.DecidedAt.After(at(30))) {
			kept = append(kept, v)
		}
	}
	in.Verdicts = kept
	h := reviewhistory.Build(in)
	assert.Equal(t, domain.ReviewStateOpen, byID(h, "code_review", "SEC-1").Status)
	assert.Equal(t, domain.ReviewStateFixed, byID(h, "code_review", "ARCH-1").Status)
	_ = f
}

func TestAFindingThatComesBackIsReopened(t *testing.T) {
	in, f := loopFixture(t)
	in.Findings = append(in.Findings, f.finding(f.architect, "architect", "code_review", "ARCH-1", "api/handler.go", 42, "missing nil check", 49))
	h := reviewhistory.Build(in)
	arch1 := byID(h, "code_review", "ARCH-1")
	assert.Equal(t, domain.ReviewStateOpen, arch1.Status)
	assert.Equal(t, 1, arch1.RaisedRound)
	assert.Zero(t, arch1.FixedRound)
	assert.Equal(t, []string{"raised", "still_open"}, trailStates(arch1))

	// fixed in round 3 of code review, then back in round 4
	f2 := newFixture()
	s1 := f2.span("code_review", 10, 20)
	f2.span("need_revision", 20, 30)
	f2.span("code_review", 30, 40)
	f2.span("need_revision", 40, 50)
	s3 := f2.span("code_review", 50, 60)
	f2.span("need_revision", 60, 70)
	s4 := f2.span("code_review", 70, 0)
	f2.task.Column = domain.TaskColumnCodeReview
	f2.verdict(s1, f2.architect, "architect", "reject", 19)
	f2.verdict(s3, f2.architect, "architect", "approve", 59)
	f2.verdict(s4, f2.architect, "architect", "reject", 75)
	rows := []domain.TaskReviewFinding{
		f2.finding(f2.architect, "architect", "code_review", "K", "a.go", 1, "x", 19),
		f2.finding(f2.architect, "architect", "code_review", "K", "a.go", 1, "x", 74),
	}
	h2 := reviewhistory.Build(reviewhistory.Inputs{Task: f2.task, Spans: f2.spans, Verdicts: f2.verdicts, Findings: rows, AgentName: names(f2), Now: at(100)})
	k := h2.Findings[0]
	assert.Equal(t, domain.ReviewStateReopened, k.Status)
	assert.Equal(t, []string{"raised", "fixed", "reopened"}, trailStates(k))
	assert.Equal(t, 1, h2.Summary.Reopened)
	assert.Equal(t, 1, h2.Summary.Open)
}

func trailStates(f domain.ReviewHistoryFinding) []string {
	var out []string
	for _, s := range f.Trail {
		out = append(out, s.State)
	}
	return out
}

func TestAWithdrawnFindingIsNotCountedOpen(t *testing.T) {
	in, f := loopFixture(t)
	in.Findings = append(in.Findings, f.finding(f.architect, "architect", "code_review", "ARCH-2", "api/handler.go", 91, "error swallowed", 49, func(r *domain.TaskReviewFinding) {
		r.Status = domain.ReviewFindingWithdrawn
	}))
	h := reviewhistory.Build(in)
	assert.Equal(t, domain.ReviewStateWithdrawn, byID(h, "code_review", "ARCH-2").Status)
}

func TestAQAFailureIsNotFixedBySilence(t *testing.T) {
	in, _ := loopFixture(t)
	var kept []domain.TaskReviewFinding
	for _, r := range in.Findings {
		if r.Status != domain.ReviewFindingPassed {
			kept = append(kept, r)
		}
	}
	in.Findings = kept
	h := reviewhistory.Build(in)
	for _, fd := range h.Findings {
		if fd.Category == domain.ReviewCategoryAcceptanceCriterion {
			assert.Equal(t, domain.ReviewStateOpen, fd.Status)
		}
	}
}

func TestAHumanSendingTheCardBackIsAReviewer(t *testing.T) {
	f := newFixture()
	f.task.Column = domain.TaskColumnNeedRevision
	f.span("code_review", 10, 20)
	f.move(20, "code_review", "need_revision", "human", nil)
	h := reviewhistory.Build(reviewhistory.Inputs{Task: f.task, Spans: f.spans, Events: f.events, Now: at(30)})
	require.Len(t, h.Rounds, 1)
	assert.Equal(t, "changes_requested", h.Rounds[0].Outcome)
	require.Len(t, h.Rounds[0].Reviewers, 1)
	assert.True(t, h.Rounds[0].Reviewers[0].Human)
	assert.Equal(t, 1, h.Summary.Bounces)
}

func TestSyntheticMovesAreNotBounces(t *testing.T) {
	f := newFixture()
	f.span("code_review", 10, 20)
	f.move(20, "code_review", "need_revision", "system", map[string]interface{}{"system_reason": domain.MoveReasonReconciled})
	h := reviewhistory.Build(reviewhistory.Inputs{Task: f.task, Spans: f.spans, Events: f.events, Now: at(30)})
	assert.Zero(t, h.Summary.Bounces)
}

func TestLoopGuardStopsAreCountedAndFlagged(t *testing.T) {
	f := newFixture()
	f.task.Column = domain.TaskColumnBlocked
	f.span("code_review", 10, 20)
	f.move(20, "code_review", "need_revision", "agent", nil)
	f.move(30, "need_revision", "blocked", "system", map[string]interface{}{"system_reason": domain.MoveReasonReviewLoopParked})
	h := reviewhistory.Build(reviewhistory.Inputs{Task: f.task, Spans: f.spans, Events: f.events, Now: at(40), Rules: rules(t)})
	assert.Equal(t, 1, h.Summary.LoopGuardStops)
	assert.True(t, h.Rounds[0].LoopGuard)
	require.Len(t, h.Flags, 1)
	assert.Equal(t, domain.ReviewFlagLoopGuard, h.Flags[0].Code)
	assert.Equal(t, domain.ReviewFlagSourceAutomatic, h.Flags[0].Source)
	assert.Equal(t, 1, h.Flags[0].Count)
}

func TestHistoryOfATaskNeverReviewedIsEmptyNotNull(t *testing.T) {
	h := reviewhistory.Build(reviewhistory.Inputs{Task: domain.BoardTask{ID: uuid.New()}})
	raw, err := json.Marshal(h)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"rounds":[]`)
	assert.Contains(t, string(raw), `"findings":[]`)
	assert.Contains(t, string(raw), `"flags":[]`)
}
