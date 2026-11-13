package reviewhistory

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Inputs is everything the history is read from. Nothing here is written back.
type Inputs struct {
	Task      domain.BoardTask
	Spans     []domain.TaskColumnSpan
	Verdicts  []domain.TaskReviewVerdict
	Events    []domain.BoardEvent
	Runs      []domain.TaskAgentRun
	Traces    []domain.RunTrace
	Findings  []domain.TaskReviewFinding
	Acks      []domain.ReviewFlagAck
	Criteria  []domain.AcceptanceCriterion
	TestCases []domain.TaskTestCase
	// AgentName resolves an agent id to its name; nil or "" leaves it unnamed.
	AgentName func(uuid.UUID) string
	Rules     *Rules
	Now       time.Time
	// Diff and Patch are the pull request's changed files and its combined
	// stat-plus-patch text as git gives them now; both empty when there is no
	// checkout to read. RunEvents returns a run's recorded tool calls.
	Diff      []DiffFile
	Patch     string
	RunEvents func(uuid.UUID) []domain.RunTraceEvent
}

var loopGuardReasons = map[string]bool{
	domain.MoveReasonReviewLoopParked:   true,
	domain.MoveReasonPipelineLoopParked: true,
	domain.MoveReasonCriteriaLoopParked: true,
	domain.MoveReasonQAUnchangedRejectParked: true,
}

// The review gates whose visits are rounds. A reviewer's report names its own
// gate, which is added when it is not one of these.
var defaultGates = []string{string(domain.TaskColumnCodeReview), string(domain.TaskColumnInQA)}

func (in Inputs) name(id *uuid.UUID, fallback string) string {
	if fallback != "" {
		return fallback
	}
	if id != nil && in.AgentName != nil {
		return in.AgentName(*id)
	}
	return ""
}

type gateRound struct {
	round   *domain.ReviewHistoryRound
	spanIDs map[uuid.UUID]bool
	until   time.Time
	// reporters are the agents that recorded something in this round.
	reporters map[uuid.UUID]bool
}

func (g *gateRound) noteReport(agent *uuid.UUID) {
	if g.reporters == nil {
		g.reporters = map[uuid.UUID]bool{}
	}
	if agent != nil {
		g.reporters[*agent] = true
	}
}

// Build assembles the history: rounds in the order they happened, each with
// the verdicts, the findings it raised and settled, the moves that followed it
// and the runs that answered it, plus the flags a person should look at.
func Build(in Inputs) domain.ReviewHistory {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	out := domain.ReviewHistory{TaskID: in.Task.ID, Rounds: []domain.ReviewHistoryRound{}, Findings: []domain.ReviewHistoryFinding{}, Flags: []domain.ReviewFlag{}}

	moves := readMoves(in)
	gates := append([]string(nil), defaultGates...)
	for _, f := range in.Findings {
		if f.Gate != "" && !contains(gates, f.Gate) {
			gates = append(gates, f.Gate)
		}
	}

	spans := append([]domain.TaskColumnSpan(nil), in.Spans...)
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].EnteredAt.Before(spans[j].EnteredAt) })

	var all []*gateRound
	byGate := map[string][]*gateRound{}
	for _, gate := range gates {
		for i, group := range domain.ReviewRoundSpans(spans, gate) {
			ids := map[uuid.UUID]bool{}
			for _, sp := range group {
				ids[sp.ID] = true
			}
			last := group[len(group)-1]
			r := &domain.ReviewHistoryRound{
				Gate: gate, Number: i + 1, EnteredAt: group[0].EnteredAt, LeftAt: last.LeftAt,
				Reviewers: []domain.ReviewHistoryReviewer{}, Raised: []string{}, Fixed: []string{},
				StillOpen: []string{}, Reopened: []string{}, Moves: []domain.ReviewHistoryMove{}, FixRuns: []domain.ReviewHistoryRun{},
			}
			gr := &gateRound{round: r, spanIDs: ids}
			byGate[gate] = append(byGate[gate], gr)
			all = append(all, gr)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].round.EnteredAt.Before(all[j].round.EnteredAt) })
	for i, gr := range all {
		gr.round.Seq = i + 1
	}
	for _, rounds := range byGate {
		for i, gr := range rounds {
			if i+1 < len(rounds) {
				gr.until = rounds[i+1].round.EnteredAt
			} else {
				gr.until = now.Add(time.Hour)
			}
		}
	}

	windowEnd := func(i int) time.Time {
		if i+1 < len(all) {
			return all[i+1].round.EnteredAt
		}
		return now.Add(time.Hour)
	}

	// Moves, grouped under the round whose window they fall in; every move
	// before the first round is the task's road to review, not its history.
	for _, m := range moves {
		for i, gr := range all {
			if !m.At.Before(gr.round.EnteredAt) && m.At.Before(windowEnd(i)) {
				gr.round.Moves = append(gr.round.Moves, m)
				if m.LoopGuard {
					gr.round.LoopGuard = true
				}
				break
			}
		}
		if len(all) > 0 && !m.At.Before(all[0].round.EnteredAt) {
			if m.Bounce {
				out.Summary.Bounces++
			}
			if m.LoopGuard {
				out.Summary.LoopGuardStops++
			}
		}
	}

	attachFindings(&out, in, byGate)

	latestCode := -1
	for i, gr := range all {
		if gr.round.Gate == string(domain.TaskColumnCodeReview) {
			latestCode = i
		}
	}
	for i, gr := range all {
		r := gr.round
		fillReviewers(r, gr, in)
		fillOutcome(r, in)
		fillCoverage(r, i == latestCode, in)
		fillRuns(r, windowEnd(i), in)
		out.Rounds = append(out.Rounds, *r)
	}
	fillSHAs(out.Rounds, in)

	out.Summary.Rounds = len(out.Rounds)
	for _, f := range out.Findings {
		out.Summary.FindingsRaised++
		switch f.Status {
		case domain.ReviewStateFixed:
			out.Summary.Fixed++
		case domain.ReviewStateWithdrawn:
			out.Summary.Withdrawn++
		default:
			out.Summary.Open++
		}
		for _, step := range f.Trail {
			if step.State == domain.ReviewStateReopened {
				out.Summary.Reopened++
				break
			}
		}
		if f.Category == domain.ReviewCategoryAcceptanceCriterion {
			out.Summary.QAFailures++
		}
	}

	out.Flags = deriveFlags(in, out.Rounds, moves)
	for _, f := range out.Flags {
		if !f.Reviewed() {
			out.Summary.OpenFlags++
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func readMoves(in Inputs) []domain.ReviewHistoryMove {
	events := append([]domain.BoardEvent(nil), in.Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].CreatedAt.Before(events[j].CreatedAt) })
	var out []domain.ReviewHistoryMove
	for _, e := range events {
		if e.EventType != domain.BoardEventTaskMoved {
			continue
		}
		var p map[string]interface{}
		if len(e.Payload) == 0 || json.Unmarshal(e.Payload, &p) != nil {
			continue
		}
		from, _ := p["from_column"].(string)
		to, _ := p["to_column"].(string)
		if to == "" || from == to {
			continue
		}
		m := domain.ReviewHistoryMove{At: e.CreatedAt, From: from, To: to}
		m.Actor, _ = p[domain.EventPayloadActor].(string)
		if m.Actor == "" && e.ActorUserID != nil {
			m.Actor = domain.EventActorHuman
		}
		m.SystemReason, _ = p[domain.EventPayloadReason].(string)
		if raw, _ := p["actor_agent_id"].(string); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				m.AgentID = &id
				m.AgentName = in.name(&id, "")
			}
		}
		_, reconciled := p["reconciled"]
		_, resumed := p["resumed"]
		_, resumedResource := p[domain.EventPayloadResumedResource]
		synthetic := m.SystemReason == domain.MoveReasonReconciled || reconciled || resumed || resumedResource
		m.Bounce = to == string(domain.TaskColumnNeedRevision) && !synthetic
		m.LoopGuard = loopGuardReasons[m.SystemReason]
		out = append(out, m)
	}
	return out
}

func fillReviewers(r *domain.ReviewHistoryRound, gr *gateRound, in Inputs) {
	for _, v := range domain.LatestVerdictPerReviewer(in.Verdicts, gr.spanIDs) {
		at := v.DecidedAt
		r.Reviewers = append(r.Reviewers, domain.ReviewHistoryReviewer{
			AgentID: v.AgentID, AgentName: in.name(v.AgentID, v.AgentName), Verdict: v.Verdict, DecidedAt: &at,
		})
	}
	end := in.Now.Add(time.Hour)
	if r.LeftAt != nil {
		end = r.LeftAt.Add(time.Minute)
	}
	seen := map[uuid.UUID]bool{}
	for _, rv := range r.Reviewers {
		if rv.AgentID != nil {
			seen[*rv.AgentID] = true
		}
	}
	if r.Gate != string(domain.TaskColumnCodeReview) {
		for _, f := range in.Findings {
			if f.Gate == r.Gate && f.AgentID != nil && !seen[*f.AgentID] && !f.CreatedAt.Before(r.EnteredAt) && f.CreatedAt.Before(end) {
				seen[*f.AgentID] = true
				r.Reviewers = append(r.Reviewers, domain.ReviewHistoryReviewer{AgentID: f.AgentID, AgentName: in.name(f.AgentID, f.AgentName), Verdict: "pending"})
			}
		}
	}
	for i := range r.Reviewers {
		rv := &r.Reviewers[i]
		if rv.AgentID == nil {
			continue
		}
		var best *domain.TaskAgentRun
		for j := range in.Runs {
			run := &in.Runs[j]
			if run.AgentID == *rv.AgentID && !run.CreatedAt.Before(r.EnteredAt.Add(-time.Minute)) && run.CreatedAt.Before(end) {
				if best == nil || run.CreatedAt.After(best.CreatedAt) {
					best = run
				}
			}
		}
		if best != nil {
			id := best.ID
			rv.RunID = &id
		}
	}
	if r.Reviewers == nil {
		r.Reviewers = []domain.ReviewHistoryReviewer{}
	}
}

func exitMove(r *domain.ReviewHistoryRound) *domain.ReviewHistoryMove {
	for i := range r.Moves {
		m := &r.Moves[i]
		if m.From == r.Gate && m.To != r.Gate && m.To != string(domain.TaskColumnBlocked) {
			return m
		}
	}
	return nil
}

func fillOutcome(r *domain.ReviewHistoryRound, in Inputs) {
	exit := exitMove(r)
	if r.LeftAt == nil && exit == nil && in.Task.Column == domain.TaskColumn(r.Gate) {
		r.Outcome = "open"
		return
	}
	if exit != nil && exit.Actor == domain.EventActorHuman {
		verdict := "approve"
		if exit.To == string(domain.TaskColumnNeedRevision) {
			verdict = "reject"
		}
		at := exit.At
		r.Reviewers = append(r.Reviewers, domain.ReviewHistoryReviewer{Human: true, Verdict: verdict, DecidedAt: &at})
	}
	rejected := false
	approved := false
	for _, rv := range r.Reviewers {
		switch rv.Verdict {
		case "reject":
			rejected = true
		case "approve":
			approved = true
		}
	}
	sentBack := exit != nil && exit.To == string(domain.TaskColumnNeedRevision)
	if r.Gate == string(domain.TaskColumnInQA) {
		if sentBack || rejected || len(r.Raised)+len(r.StillOpen)+len(r.Reopened) > 0 {
			r.Outcome = "failed"
		} else if exit != nil {
			r.Outcome = "passed"
		} else {
			r.Outcome = "closed"
		}
		return
	}
	switch {
	case rejected || sentBack:
		r.Outcome = "changes_requested"
	case approved:
		r.Outcome = "approved"
	default:
		r.Outcome = "closed"
	}
}

func fillRuns(r *domain.ReviewHistoryRound, until time.Time, in Inputs) {
	if r.LeftAt == nil {
		return
	}
	reviewers := map[uuid.UUID]bool{}
	for _, rv := range r.Reviewers {
		if rv.AgentID != nil {
			reviewers[*rv.AgentID] = true
		}
	}
	traces := map[uuid.UUID]domain.RunTrace{}
	for _, t := range in.Traces {
		traces[t.RunID] = t
	}
	runs := append([]domain.TaskAgentRun(nil), in.Runs...)
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].CreatedAt.Before(runs[j].CreatedAt) })
	for _, run := range runs {
		if run.CreatedAt.Before(*r.LeftAt) || !run.CreatedAt.Before(until) || reviewers[run.AgentID] {
			continue
		}
		id := run.AgentID
		hr := domain.ReviewHistoryRun{
			RunID: run.ID, AgentID: run.AgentID, AgentName: in.name(&id, ""), Status: run.Status,
			StartedAt: run.CreatedAt, Summary: truncate(run.Summary, 240),
		}
		if t, ok := traces[run.ID]; ok {
			hr.HasTrace = true
			hr.BaseSHA, hr.HeadSHA = t.BaseSHA, t.HeadSHA
			hr.FinishedAt = t.FinishedAt
			if t.Verification != nil {
				hr.FilesChanged = len(t.Verification.Files)
			}
		}
		r.FixRuns = append(r.FixRuns, hr)
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// fillSHAs gives each round the commit it was judged at and the commit the
// previous round of the same gate was judged at: the range the developer's
// answer to that round covers.
func fillSHAs(rounds []domain.ReviewHistoryRound, in Inputs) {
	head := map[string]string{}
	for i := range rounds {
		r := &rounds[i]
		r.FromSHA = head[r.Gate]
		end := in.Now.Add(time.Hour)
		if r.LeftAt != nil {
			end = r.LeftAt.Add(time.Minute)
		}
		sha := ""
		for _, f := range in.Findings {
			if f.Gate == r.Gate && f.HeadSHA != "" && !f.CreatedAt.Before(r.EnteredAt) && f.CreatedAt.Before(end) {
				sha = f.HeadSHA
			}
		}
		if sha == "" {
			var latest time.Time
			for _, t := range in.Traces {
				if t.HeadSHA != "" && t.FinishedAt != nil && !t.FinishedAt.After(r.EnteredAt.Add(time.Minute)) && t.FinishedAt.After(latest) {
					latest, sha = *t.FinishedAt, t.HeadSHA
				}
			}
		}
		r.HeadSHA = sha
		if sha != "" {
			head[r.Gate] = sha
		}
	}
}
