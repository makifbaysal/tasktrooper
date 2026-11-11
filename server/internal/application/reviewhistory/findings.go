package reviewhistory

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// roundOf is the round of gate a report written at t belongs to: the latest
// one that had begun by then.
func roundOf(rounds []*gateRound, t time.Time) *gateRound {
	var found *gateRound
	for _, gr := range rounds {
		if !gr.round.EnteredAt.After(t) {
			found = gr
		}
	}
	if found == nil && len(rounds) > 0 {
		return rounds[0]
	}
	return found
}

type perRound struct {
	present, withdrawn, passed bool
	row                        *domain.TaskReviewFinding
}

// attachFindings reads each finding's life across the rounds of its gate: raised
// in the first round that reports it; fixed when a later round was reviewed
// and no longer reports it (or QA approved the criterion); reopened when it
// comes back after that.
func attachFindings(out *domain.ReviewHistory, in Inputs, byGate map[string][]*gateRound) {
	criteria := map[uuid.UUID]string{}
	for _, c := range in.Criteria {
		criteria[c.ID] = c.Text
	}
	rows := append([]domain.TaskReviewFinding(nil), in.Findings...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })

	type key struct{ gate, fp string }
	var order []key
	seen := map[key]map[*gateRound]*perRound{}
	last := map[key]*domain.TaskReviewFinding{}
	for i := range rows {
		row := &rows[i]
		rounds := byGate[row.Gate]
		gr := roundOf(rounds, row.CreatedAt)
		if gr == nil {
			continue
		}
		gr.noteReport(row.AgentID)
		if row.Kind != domain.ReviewItemFinding {
			continue
		}
		k := key{row.Gate, row.Fingerprint}
		if seen[k] == nil {
			seen[k] = map[*gateRound]*perRound{}
			order = append(order, k)
		}
		pr := seen[k][gr]
		if pr == nil {
			pr = &perRound{}
			seen[k][gr] = pr
		}
		switch row.Status {
		case domain.ReviewFindingWithdrawn:
			pr.present, pr.withdrawn, pr.passed = false, true, false
		case domain.ReviewFindingPassed:
			pr.present, pr.withdrawn, pr.passed = false, false, true
		default:
			pr.present, pr.withdrawn, pr.passed = true, false, false
			pr.row = row
			last[k] = row
		}
	}
	// A reviewer that spoke in a round and did not repeat a finding is saying
	// it is gone. QA is the exception: it speaks criterion by criterion, so a
	// criterion it did not look at again is not a criterion it passed.
	reviewedBy := func(gr *gateRound, agent *uuid.UUID) bool {
		if gr.round.Gate == string(domain.TaskColumnInQA) {
			return false
		}
		if agent == nil {
			return len(gr.reporters) > 0
		}
		if gr.reporters[*agent] {
			return true
		}
		for _, v := range in.Verdicts {
			if gr.spanIDs[v.SpanID] && v.AgentID != nil && *v.AgentID == *agent {
				return true
			}
		}
		return false
	}

	for _, k := range order {
		row := last[k]
		if row == nil {
			continue
		}
		f := domain.ReviewHistoryFinding{
			ID: k.fp, Gate: k.gate, AgentID: row.AgentID, AgentName: in.name(row.AgentID, row.AgentName),
			File: row.File, Line: row.Line, Severity: row.Severity, Category: row.Category, Message: row.Message,
			Confidence: row.Confidence, CriterionID: row.CriterionID, Trail: []domain.ReviewHistoryTrailStep{},
		}
		if row.CriterionID != nil {
			f.CriterionText = criteria[*row.CriterionID]
		}
		f.FileURL = domain.ReviewFileURL(in.Task.PRURL, row.File, row.Line)

		state := "none"
		reopened := false
		for _, gr := range byGate[k.gate] {
			pr := seen[k][gr]
			r := gr.round
			step := func(s string) {
				f.Trail = append(f.Trail, domain.ReviewHistoryTrailStep{Round: r.Seq, State: s, At: r.EnteredAt})
			}
			switch {
			case pr != nil && pr.present:
				switch state {
				case "none":
					f.RaisedRound = r.Seq
					r.Raised = append(r.Raised, k.fp)
					step("raised")
				case "fixed", "withdrawn":
					reopened = true
					r.Reopened = append(r.Reopened, k.fp)
					step(domain.ReviewStateReopened)
				default:
					r.StillOpen = append(r.StillOpen, k.fp)
					step("still_open")
				}
				state = "open"
			case pr != nil && (pr.passed || pr.withdrawn) && state == "open":
				if pr.passed {
					state = "fixed"
				} else {
					state = "withdrawn"
				}
				f.FixedRound = r.Seq
				if state == "fixed" {
					r.Fixed = append(r.Fixed, k.fp)
				}
				step(state)
			case state == "open" && reviewedBy(gr, row.AgentID):
				state = "fixed"
				f.FixedRound = r.Seq
				r.Fixed = append(r.Fixed, k.fp)
				step("fixed")
			}
		}
		switch state {
		case "fixed":
			f.Status = domain.ReviewStateFixed
		case "withdrawn":
			f.Status = domain.ReviewStateWithdrawn
		default:
			f.Status = domain.ReviewStateOpen
			if reopened {
				f.Status = domain.ReviewStateReopened
			}
			f.FixedRound = 0
		}
		out.Findings = append(out.Findings, f)
	}
	sort.SliceStable(out.Findings, func(i, j int) bool {
		a, b := out.Findings[i], out.Findings[j]
		if a.RaisedRound != b.RaisedRound {
			return a.RaisedRound < b.RaisedRound
		}
		return severityRank(a.Severity) < severityRank(b.Severity)
	})
}

func severityRank(s string) int {
	for i, v := range domain.ReviewSeverities() {
		if strings.EqualFold(v, s) {
			return i
		}
	}
	return 99
}
