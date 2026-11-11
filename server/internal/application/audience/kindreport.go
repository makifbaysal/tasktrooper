package audience

import (
	"math"
	"sort"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// bySegmentOf groups answers by segment, keeping the order segments first answer.
func bySegmentOf(ok []domain.StudyAnswer) (map[string][]domain.StudyAnswer, []string) {
	groups := map[string][]domain.StudyAnswer{}
	var order []string
	for _, a := range ok {
		n := segmentName(a)
		if _, seen := groups[n]; !seen {
			order = append(order, n)
		}
		groups[n] = append(groups[n], a)
	}
	return groups, order
}

// addKindReport computes the part of the report that belongs to the study's
// kind: arithmetic only.
func addKindReport(r *domain.Report, st domain.Study, ok []domain.StudyAnswer) {
	r.Kind = st.Kind
	switch st.Kind {
	case domain.KindLandingPageTest:
		r.Landing = landingReport(st, ok)
	case domain.KindMessageTesting:
		r.Messages = messageReport(r, st, ok)
	case domain.KindABTest:
		r.AB = abReport(st, ok)
	}
}

func landingReport(st domain.Study, ok []domain.StudyAnswer) *domain.LandingReport {
	lr := &domain.LandingReport{Intent: st.Inputs.Intent}
	if len(st.Inputs.Pages) > 0 {
		lr.Source = pageSource(st.Inputs.Pages[0])
	}
	var all []float64
	for _, a := range ok {
		if a.Answers.Landing != nil {
			all = append(all, float64(a.Answers.Landing.ClarityScore))
		}
	}
	lr.ClarityMean = round(mean(all), 2)
	groups, order := bySegmentOf(ok)
	for _, n := range order {
		var v []float64
		for _, a := range groups[n] {
			if a.Answers.Landing != nil {
				v = append(v, float64(a.Answers.Landing.ClarityScore))
			}
		}
		lr.ClarityBy = append(lr.ClarityBy, domain.NamedScore{Name: n, Value: round(mean(v), 2)})
	}
	return lr
}

func messageReport(r *domain.Report, st domain.Study, ok []domain.StudyAnswer) *domain.MessageReport {
	mr := &domain.MessageReport{Variants: []domain.VariantReport{}}
	groups, order := bySegmentOf(ok)
	for i, text := range st.Inputs.Variants {
		idx := i + 1
		vr := domain.VariantReport{Variant: idx, Text: text}
		var cl, re, cr, ap []float64
		best := 0
		for _, a := range ok {
			for _, rt := range a.Answers.Ratings {
				if rt.Variant == idx {
					cl, re, cr, ap = append(cl, float64(rt.Clarity)), append(re, float64(rt.Relevance)), append(cr, float64(rt.Credibility)), append(ap, float64(rt.Appeal))
				}
			}
			if a.Answers.BestVariant == idx {
				best++
			}
		}
		vr.Clarity, vr.Relevance, vr.Credibility, vr.Appeal = round(mean(cl), 2), round(mean(re), 2), round(mean(cr), 2), round(mean(ap), 2)
		vr.Overall = round((vr.Clarity+vr.Relevance+vr.Credibility+vr.Appeal)/4, 2)
		vr.BestPct = pct(best, len(ok))
		for _, n := range order {
			var v []float64
			for _, a := range groups[n] {
				for _, rt := range a.Answers.Ratings {
					if rt.Variant == idx {
						v = append(v, float64(rt.Clarity+rt.Relevance+rt.Credibility+rt.Appeal)/4)
					}
				}
			}
			vr.BySegment = append(vr.BySegment, domain.NamedScore{Name: n, Value: round(mean(v), 2)})
		}
		mr.Variants = append(mr.Variants, vr)
	}
	ranked := append([]domain.VariantReport(nil), mr.Variants...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Overall != ranked[j].Overall {
			return ranked[i].Overall > ranked[j].Overall
		}
		return ranked[i].BestPct > ranked[j].BestPct
	})
	for rank, v := range ranked {
		mr.Variants[v.Variant-1].Rank = rank + 1
	}
	if len(ranked) > 0 {
		mr.Winner = ranked[0].Variant
		r.Verdict.Score = round(ranked[0].Overall*10, 1)
		r.Verdict.Label = verdictLabel(r.Verdict.Score)
	}
	for _, a := range ok {
		if a.Answers.Stronger != "" && len(mr.Rewrites) < 8 {
			mr.Rewrites = append(mr.Rewrites, domain.ReportQuote{Persona: a.Persona.Name, Segment: a.Persona.Segment, Role: a.Persona.Role, Text: a.Answers.Stronger})
		}
	}
	return mr
}

// abConfidence reads the margin against how many answered: a small group
// never gets better than "lean".
func abConfidence(winsA, winsB int) string {
	n := winsA + winsB
	if n == 0 {
		return "toss-up"
	}
	margin := math.Abs(float64(winsA-winsB)) / float64(n) * 100
	switch {
	case margin >= 40 && n >= 8:
		return "clear"
	case margin >= 20:
		return "lean"
	default:
		return "toss-up"
	}
}

func medianOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return round(percentile(s, .5), 2)
}

func stddev(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	sum := 0.0
	for _, x := range v {
		sum += (x - m) * (x - m)
	}
	return math.Sqrt(sum / float64(len(v)-1))
}

// effectLabel is Cohen's conventional reading of |d|.
func effectLabel(d float64) string {
	switch a := math.Abs(d); {
	case a < 0.2:
		return "negligible"
	case a < 0.5:
		return "small"
	case a < 0.8:
		return "medium"
	default:
		return "large"
	}
}

// pairedMetric compares two scorecards answered by the same people: the means
// and medians of each, the mean of each person's own B-minus-A difference, how
// many scored one version higher, and the effect size of those differences
// (their mean over their spread, capped at 3 when nobody varies).
func pairedMetric(a, b []float64) domain.ABMetric {
	m := domain.ABMetric{
		A: domain.ArmMetric{Mean: round(mean(a), 2), Median: medianOf(a)},
		B: domain.ArmMetric{Mean: round(mean(b), 2), Median: medianOf(b)},
	}
	diffs := make([]float64, 0, len(a))
	for i := range a {
		d := b[i] - a[i]
		diffs = append(diffs, d)
		switch {
		case d > 0:
			m.BHigher++
		case d < 0:
			m.AHigher++
		default:
			m.Same++
		}
	}
	m.Diff = round(mean(diffs), 2)
	if len(diffs) >= 2 {
		sd := stddev(diffs)
		var d float64
		switch {
		case sd > 0:
			d = mean(diffs) / sd
		case mean(diffs) != 0:
			d = math.Copysign(3, mean(diffs))
		}
		d = math.Max(-3, math.Min(3, d))
		m.EffectSize, m.EffectLabel = round(d, 2), effectLabel(d)
	} else {
		m.EffectLabel = effectLabel(0)
	}
	return m
}

// abReport compares the two versions over everyone: each person scored both
// on the same scorecard and then said which they prefer.
func abReport(st domain.Study, ok []domain.StudyAnswer) *domain.ABReport {
	ab := &domain.ABReport{}
	for i, label := range []string{"A", "B"} {
		ab.Sides[i].Label = label
		ab.Sides[i].Quotes = []domain.ReportQuote{}
		if i < len(st.Inputs.Pages) {
			ab.Sides[i].Source = pageSource(st.Inputs.Pages[i])
		}
	}
	groups, order := bySegmentOf(ok)
	var actA, actB, clA, clB []float64
	var yes [2]int
	var assessed []domain.StudyAnswer
	firstWins := 0
	for _, a := range ok {
		if a.Answers.PageA == nil || a.Answers.PageB == nil {
			continue
		}
		assessed = append(assessed, a)
		actA, actB = append(actA, float64(a.Answers.PageA.ActScore)), append(actB, float64(a.Answers.PageB.ActScore))
		clA, clB = append(clA, float64(a.Answers.PageA.ClarityScore)), append(clB, float64(a.Answers.PageB.ClarityScore))
		if a.Answers.PageA.Verdict == "yes" {
			yes[0]++
		}
		if a.Answers.PageB.Verdict == "yes" {
			yes[1]++
		}
		switch a.Answers.Preferred {
		case "a":
			ab.Sides[0].Wins++
		case "b":
			ab.Sides[1].Wins++
		}
		if a.Answers.Preferred == a.Answers.ShownFirst {
			firstWins++
		}
	}
	n := len(assessed)
	ab.Act, ab.Clarity = pairedMetric(actA, actB), pairedMetric(clA, clB)
	for i := range ab.Sides {
		ab.Sides[i].WinPct = pct(ab.Sides[i].Wins, n)
		ab.Sides[i].ActPct = pct(yes[i], n)
	}
	ab.Sides[0].MeanScore, ab.Sides[1].MeanScore = ab.Act.A.Mean, ab.Act.B.Mean
	ab.FirstShownWinPct = pct(firstWins, n)
	for _, name := range order {
		g := groups[name]
		wa, wb, m := 0, 0, 0
		for _, a := range g {
			if a.Answers.PageA == nil || a.Answers.PageB == nil {
				continue
			}
			m++
			if a.Answers.Preferred == "a" {
				wa++
			} else if a.Answers.Preferred == "b" {
				wb++
			}
		}
		ab.Sides[0].BySegment = append(ab.Sides[0].BySegment, domain.NamedScore{Name: name, Value: pct(wa, m)})
		ab.Sides[1].BySegment = append(ab.Sides[1].BySegment, domain.NamedScore{Name: name, Value: pct(wb, m)})
	}
	ab.Sides[0].Issues = frequencyThemes(assessed, func(a domain.StudyAnswer) []string { return a.Answers.PageA.Objections })
	ab.Sides[1].Issues = frequencyThemes(assessed, func(a domain.StudyAnswer) []string { return a.Answers.PageB.Objections })
	switch {
	case ab.Sides[0].Wins > ab.Sides[1].Wins:
		ab.Winner = "A"
	case ab.Sides[1].Wins > ab.Sides[0].Wins:
		ab.Winner = "B"
	default:
		ab.Winner = "tie"
	}
	ab.Confidence = abConfidence(ab.Sides[0].Wins, ab.Sides[1].Wins)
	return ab
}
