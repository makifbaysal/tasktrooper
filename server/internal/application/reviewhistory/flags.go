package reviewhistory

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxFlagFiles = 8

// changedFiles is every path the task's recorded runs changed, and the paths
// git showed changed that no recorded tool call accounts for.
func changedFiles(traces []domain.RunTrace) (changed, unrecorded []string) {
	seenC, seenU := map[string]bool{}, map[string]bool{}
	for _, t := range traces {
		if t.Verification == nil || !t.Verification.Available {
			continue
		}
		for _, f := range t.Verification.Files {
			if !seenC[f.Path] {
				seenC[f.Path] = true
				changed = append(changed, f.Path)
			}
		}
		for _, p := range t.Verification.Unrecorded {
			if !seenU[p] {
				seenU[p] = true
				unrecorded = append(unrecorded, p)
			}
		}
	}
	sort.Strings(changed)
	sort.Strings(unrecorded)
	return changed, unrecorded
}

func firstN(items []string, n int) []string {
	if len(items) > n {
		return items[:n]
	}
	return items
}

func deriveFlags(in Inputs, rounds []domain.ReviewHistoryRound, moves []domain.ReviewHistoryMove) []domain.ReviewFlag {
	var flags []domain.ReviewFlag
	flags = append(flags, agentFlags(in, rounds)...)
	flags = append(flags, autoFlags(in, moves)...)
	flags = append(flags, coverageFlags(in, rounds)...)

	acks := map[string]domain.ReviewFlagAck{}
	for _, a := range in.Acks {
		acks[a.FlagKey] = a
	}
	for i := range flags {
		if a, ok := acks[flags[i].ID]; ok {
			at := a.ReviewedAt
			flags[i].ReviewedBy, flags[i].ReviewedAt = a.ReviewedBy, &at
		}
	}
	sort.SliceStable(flags, func(i, j int) bool {
		if flags[i].Reviewed() != flags[j].Reviewed() {
			return !flags[i].Reviewed()
		}
		return flags[i].RaisedAt.Before(flags[j].RaisedAt)
	})
	if flags == nil {
		flags = []domain.ReviewFlag{}
	}
	return flags
}

// agentFlags are the places a reviewer asked a person to look: the same flag
// raised again in a later round is still one flag.
func agentFlags(in Inputs, rounds []domain.ReviewHistoryRound) []domain.ReviewFlag {
	rows := append([]domain.TaskReviewFinding(nil), in.Findings...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	byKey := map[string]*domain.ReviewFlag{}
	var order []string
	for _, r := range rows {
		if r.Status == domain.ReviewFindingWithdrawn {
			continue
		}
		if r.Kind != domain.ReviewItemHumanReview && strings.TrimSpace(r.HumanReviewReason) == "" {
			continue
		}
		key := domain.ReviewFlagKey("agent", domain.FlagFingerprint(r.File, r.HumanReviewReason))
		if f, ok := byKey[key]; ok {
			if r.Question != "" {
				f.Question = r.Question
			}
			if r.Line > 0 {
				f.Line = r.Line
			}
			continue
		}
		f := &domain.ReviewFlag{
			ID: key, Source: domain.ReviewFlagSourceAgent, Code: domain.ReviewFlagAgentUncertain,
			Reason: r.HumanReviewReason, Question: r.Question, File: r.File, Line: r.Line, Gate: r.Gate,
			AgentID: r.AgentID, AgentName: in.name(r.AgentID, r.AgentName), RaisedAt: r.CreatedAt,
			FileURL: domain.ReviewFileURL(in.Task.PRURL, r.File, r.Line),
		}
		for _, rd := range rounds {
			if rd.Gate == r.Gate && !rd.EnteredAt.After(r.CreatedAt) {
				f.Round = rd.Seq
			}
		}
		byKey[key] = f
		order = append(order, key)
	}
	out := make([]domain.ReviewFlag, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

func autoFlags(in Inputs, moves []domain.ReviewHistoryMove) []domain.ReviewFlag {
	var flags []domain.ReviewFlag
	auto := func(f domain.ReviewFlag) {
		f.Source = domain.ReviewFlagSourceAutomatic
		flags = append(flags, f)
	}
	raisedAt := in.Now
	if raisedAt.IsZero() {
		raisedAt = time.Now()
	}
	var lastRun time.Time
	for _, t := range in.Traces {
		if t.FinishedAt != nil && t.FinishedAt.After(lastRun) {
			lastRun = *t.FinishedAt
		}
	}
	if !lastRun.IsZero() {
		raisedAt = lastRun
	}

	changed, unrecorded := changedFiles(in.Traces)
	if in.Rules != nil && len(changed) > 0 {
		matches := in.Rules.SensitiveMatches(changed)
		ids := make([]string, 0, len(matches))
		for id := range matches {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			files := matches[id]
			auto(domain.ReviewFlag{
				ID:   domain.ReviewFlagKey("auto", domain.ReviewFlagSensitivePath, id, domain.ShortHash(files)),
				Code: domain.ReviewFlagSensitivePath, Reason: id, Files: firstN(files, maxFlagFiles), Count: len(files),
				File: files[0], FileURL: domain.ReviewFileURL(in.Task.PRURL, files[0], 0), RaisedAt: raisedAt,
			})
		}
		if max := in.Rules.LargeDiff.MaxFiles; max > 0 && len(changed) >= max {
			auto(domain.ReviewFlag{
				ID:   domain.ReviewFlagKey("auto", domain.ReviewFlagLargeDiff, strconv.Itoa(max)),
				Code: domain.ReviewFlagLargeDiff, Count: len(changed), RaisedAt: raisedAt,
			})
		}
		if source, anyTest := in.Rules.SourceNeedingTests(changed); len(source) > 0 && !anyTest {
			auto(domain.ReviewFlag{
				ID:   domain.ReviewFlagKey("auto", domain.ReviewFlagTestsMissing, domain.ShortHash(source)),
				Code: domain.ReviewFlagTestsMissing, Files: firstN(source, maxFlagFiles), Count: len(source),
				File: source[0], FileURL: domain.ReviewFileURL(in.Task.PRURL, source[0], 0), RaisedAt: raisedAt,
			})
		}
	}
	if len(unrecorded) > 0 {
		auto(domain.ReviewFlag{
			ID:   domain.ReviewFlagKey("auto", domain.ReviewFlagUnrecordedChanges, domain.ShortHash(unrecorded)),
			Code: domain.ReviewFlagUnrecordedChanges, Files: firstN(unrecorded, maxFlagFiles), Count: len(unrecorded),
			File: unrecorded[0], FileURL: domain.ReviewFileURL(in.Task.PRURL, unrecorded[0], 0), RaisedAt: raisedAt,
		})
	}

	bounces := 0
	for _, m := range moves {
		if m.Bounce {
			bounces++
		}
		if m.LoopGuard {
			auto(domain.ReviewFlag{
				ID:   domain.ReviewFlagKey("auto", domain.ReviewFlagLoopGuard, strconv.FormatInt(m.At.Unix(), 10)),
				Code: domain.ReviewFlagLoopGuard, Reason: m.SystemReason, Count: bounces, Gate: m.From, RaisedAt: m.At,
			})
		}
	}

	flags = append(flags, qaEvidenceFlags(in)...)
	return flags
}

// qaEvidenceFlags: a criterion QA approved with no passed test case, carrying
// evidence, behind it. QA's own rule asks for one; a person decides whether
// the approval stands without.
func qaEvidenceFlags(in Inputs) []domain.ReviewFlag {
	evidenced := map[uuid.UUID]bool{}
	for _, tc := range in.TestCases {
		if tc.CriterionID != nil && tc.Status == domain.TestCaseStatusPassed && strings.TrimSpace(tc.Evidence) != "" {
			evidenced[*tc.CriterionID] = true
		}
	}
	var out []domain.ReviewFlag
	for _, c := range in.Criteria {
		if c.Canceled {
			continue
		}
		for _, ck := range c.Checks {
			if ck.Role != domain.CriterionReviewRoleQA || !ck.Approved || evidenced[c.ID] {
				continue
			}
			out = append(out, domain.ReviewFlag{
				ID:     domain.ReviewFlagKey("auto", domain.ReviewFlagQANoEvidence, c.ID.String()),
				Source: domain.ReviewFlagSourceAutomatic, Code: domain.ReviewFlagQANoEvidence, Reason: c.Text,
				Gate: string(domain.TaskColumnInQA), AgentID: ck.AgentID, AgentName: in.name(ck.AgentID, ""), RaisedAt: ck.CheckedAt,
			})
		}
	}
	return out
}

// coverageFlags: files the latest code review round's reviewers, taken
// together, never opened and were never handed.
func coverageFlags(in Inputs, rounds []domain.ReviewHistoryRound) []domain.ReviewFlag {
	for i := len(rounds) - 1; i >= 0; i-- {
		r := rounds[i]
		if r.Gate != string(domain.TaskColumnCodeReview) {
			continue
		}
		if r.Coverage == nil || len(r.Coverage.NobodyReviewed) == 0 {
			return nil
		}
		paths := make([]string, len(r.Coverage.NobodyReviewed))
		for j, f := range r.Coverage.NobodyReviewed {
			paths[j] = f.Path
		}
		raised := r.EnteredAt
		if r.LeftAt != nil {
			raised = *r.LeftAt
		}
		return []domain.ReviewFlag{{
			ID:     domain.ReviewFlagKey("auto", domain.ReviewFlagCoverageGap, domain.ShortHash(paths)),
			Source: domain.ReviewFlagSourceAutomatic, Code: domain.ReviewFlagCoverageGap,
			Files: firstN(paths, maxFlagFiles), Count: len(paths), File: paths[0],
			FileURL: domain.ReviewFileURL(in.Task.PRURL, paths[0], 0), Gate: r.Gate, Round: r.Seq, RaisedAt: raised,
		}}
	}
	return nil
}
