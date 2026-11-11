package audience

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type synthUserInput struct {
	Title, Goal, Product, Language, Currency, Stats string
	Segments, Personas, Questions                   []string
}

type methodologyInput struct {
	Personas, Segments, Calls int
	Currency, PriceTests      string
}

var (
	synthSystemKey = prompt.Define[struct{}]("audience.synth_system", struct{}{})
	synthUserKey   = prompt.Define("audience.synth_user", synthUserInput{
		Title: "Would they buy?", Product: "A kanban for AI agents.", Currency: "USD", Stats: "mean intent 5.2",
		Segments: []string{"- Freelancers: 4"}, Personas: []string{"p1 (Ayse): said maybe"}, Questions: []string{"- What would convince you?"},
	})
	disclaimerKey  = prompt.Define[struct{}]("audience.disclaimer", struct{}{})
	methodologyKey = prompt.Define("audience.methodology", methodologyInput{Personas: 12, Segments: 3, Calls: 13, Currency: "USD", PriceTests: "9, 19"})
)

// ---- the numbers

func okAnswers(answers []domain.StudyAnswer) []domain.StudyAnswer {
	var out []domain.StudyAnswer
	for _, a := range answers {
		if a.Status == "ok" {
			out = append(out, a)
		}
	}
	return out
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

// percentile of an ascending slice, by linear interpolation.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

func round(v float64, places int) float64 {
	f := math.Pow(10, float64(places))
	return math.Round(v*f) / f
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return round(float64(n)/float64(total)*100, 1)
}

// niceStep picks a bucket width that makes round bucket edges.
func niceStep(span float64, buckets int) float64 {
	raw := span / float64(buckets)
	if raw <= 0 {
		return 1
	}
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*mag >= raw {
			return m * mag
		}
	}
	return 10 * mag
}

func histogram(values []float64) []domain.Bucket {
	if len(values) == 0 {
		return []domain.Bucket{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	lo, hi := sorted[0], sorted[len(sorted)-1]
	if lo == hi {
		return []domain.Bucket{{From: lo, To: hi, Count: len(sorted)}}
	}
	step := niceStep(hi-lo, 8)
	start := math.Floor(lo/step) * step
	var out []domain.Bucket
	for from := start; from <= hi+1e-9; from += step {
		out = append(out, domain.Bucket{From: round(from, 2), To: round(from+step, 2)})
	}
	for _, v := range sorted {
		i := int((v - start) / step)
		if i >= len(out) {
			i = len(out) - 1
		}
		out[i].Count++
	}
	return out
}

func priceStats(answers []domain.StudyAnswer, st domain.Study) domain.PriceStats {
	ps := domain.PriceStats{Currency: st.Currency, Histogram: []domain.Bucket{}}
	var prices []float64
	for _, a := range answers {
		if a.Answers.MaxPrice == nil {
			continue
		}
		if *a.Answers.MaxPrice > 0 {
			prices = append(prices, *a.Answers.MaxPrice)
		} else {
			ps.NotWilling++
		}
	}
	ps.Answered = len(prices)
	if len(prices) > 0 {
		sort.Float64s(prices)
		ps.Min, ps.Max = prices[0], prices[len(prices)-1]
		ps.P25, ps.Median, ps.P75 = round(percentile(prices, .25), 2), round(percentile(prices, .5), 2), round(percentile(prices, .75), 2)
		ps.Mean = round(mean(prices), 2)
		ps.Histogram = histogram(prices)
	}
	for _, price := range st.PricePoints {
		asked, buy := 0, 0
		for _, a := range answers {
			for _, r := range a.Answers.PriceReactions {
				if math.Abs(r.Price-price) < 0.005 {
					asked++
					if r.WouldBuy {
						buy++
					}
					break
				}
			}
		}
		ps.Tests = append(ps.Tests, domain.PriceTest{Price: price, Asked: asked, BuyPct: pct(buy, asked)})
	}
	return ps
}

func verdictLabel(score float64) string {
	switch {
	case score >= 70:
		return "strong"
	case score >= 50:
		return "promising"
	case score >= 30:
		return "weak"
	default:
		return "poor"
	}
}

func segmentName(a domain.StudyAnswer) string {
	if a.Persona.Segment == "" {
		return "General"
	}
	return a.Persona.Segment
}

// computeReport is everything in the report that is arithmetic. The narrative
// is added by the synthesis, or left empty when it failed.
func computeReport(st domain.Study, all []domain.StudyAnswer) domain.Report {
	ok := okAnswers(all)
	m := domain.ReportMetrics{Respondents: len(ok), Failed: len(all) - len(ok), IntentDistribution: make([]int, 11)}
	var scores []float64
	payNow := 0
	for _, a := range ok {
		m.IntentDistribution[a.Answers.IntentScore]++
		scores = append(scores, float64(a.Answers.IntentScore))
		switch a.Answers.Verdict {
		case "yes":
			m.Yes++
		case "maybe":
			m.Maybe++
		default:
			m.No++
		}
		if a.Answers.WouldPayNow {
			payNow++
		}
	}
	m.IntentMean = round(mean(scores), 2)
	m.WouldPayNowPct = pct(payNow, len(ok))
	m.Price = priceStats(ok, st)

	bySegment := map[string][]domain.StudyAnswer{}
	var order []string
	for _, a := range ok {
		n := segmentName(a)
		if _, seen := bySegment[n]; !seen {
			order = append(order, n)
		}
		bySegment[n] = append(bySegment[n], a)
	}
	segments := make([]domain.SegmentReport, 0, len(order))
	for _, n := range order {
		group := bySegment[n]
		var sc, prices []float64
		yes := 0
		for _, a := range group {
			sc = append(sc, float64(a.Answers.IntentScore))
			if a.Answers.Verdict == "yes" {
				yes++
			}
			if a.Answers.MaxPrice != nil && *a.Answers.MaxPrice > 0 {
				prices = append(prices, *a.Answers.MaxPrice)
			}
		}
		sort.Float64s(prices)
		segments = append(segments, domain.SegmentReport{
			Name: n, Respondents: len(group), IntentMean: round(mean(sc), 2), YesPct: pct(yes, len(group)),
			MedianPrice: round(percentile(prices, .5), 2),
		})
	}

	score := round(m.IntentMean*10, 1)
	return domain.Report{
		GeneratedAt: st.CreatedAt,
		Verdict:     domain.ReportVerdict{Label: verdictLabel(score), Score: score},
		Metrics:     m,
		Segments:    segments,
		Objections:  []domain.Theme{}, Needs: []domain.Theme{}, Alternatives: []domain.Theme{},
		Quotes: []domain.ReportQuote{}, Recommendations: []domain.Recommendation{},
	}
}

// ---- the narrative

type synthResult struct {
	Summary          string
	VerdictRationale string
	SegmentInsights  map[string]string
	Objections       []rawTheme
	Needs            []rawTheme
	Alternatives     []rawTheme
	Quotes           []rawQuote
	Recommendations  []domain.Recommendation
	OpenQuestions    []domain.OpenQuestionDigest
}

type rawTheme struct {
	Label    string
	Personas []string
}

type rawQuote struct {
	Persona, Text, Topic, Sentiment string
}

func parseSynthesis(raw string) (synthResult, error) {
	var m map[string]any
	if err := decodeObject(raw, &m); err != nil {
		return synthResult{}, err
	}
	out := synthResult{
		Summary:          clip(strOf(m, "summary"), 1800),
		VerdictRationale: clip(strOf(m, "verdict_rationale", "rationale"), 600),
		SegmentInsights:  map[string]string{},
	}
	if out.Summary == "" {
		return out, fmt.Errorf(`"summary" is missing`)
	}
	for _, si := range objectsOf(m, "segment_insights") {
		if seg, ins := strOf(si, "segment", "name"), strOf(si, "insight"); seg != "" && ins != "" {
			out.SegmentInsights[strings.ToLower(seg)] = clip(ins, 600)
		}
	}
	themes := func(keys ...string) []rawTheme {
		var ts []rawTheme
		for _, t := range objectsOf(m, keys...) {
			if label := clip(strOf(t, "label", "theme", "name"), 120); label != "" {
				ts = append(ts, rawTheme{Label: label, Personas: listOf(t, 60, 20, "personas", "persona_ids", "ids")})
			}
		}
		return ts
	}
	out.Objections = themes("objection_themes", "objections")
	out.Needs = themes("need_themes", "needs")
	out.Alternatives = themes("alternative_themes", "alternatives")
	for _, q := range objectsOf(m, "quotes") {
		if text := clip(strOf(q, "text", "quote"), 400); text != "" {
			out.Quotes = append(out.Quotes, rawQuote{Persona: strOf(q, "persona", "persona_id"), Text: text, Topic: clip(strOf(q, "topic"), 80),
				Sentiment: oneOf(strOf(q, "sentiment"), "positive", "negative", "neutral")})
		}
	}
	for _, r := range objectsOf(m, "recommendations") {
		if title := clip(strOf(r, "title"), 160); title != "" {
			out.Recommendations = append(out.Recommendations, domain.Recommendation{Title: title, Detail: clip(strOf(r, "detail"), 600), Priority: oneOf(strOf(r, "priority"), "high", "medium", "low")})
		}
	}
	for _, q := range objectsOf(m, "open_questions") {
		if question, digest := strOf(q, "question"), strOf(q, "digest"); question != "" && digest != "" {
			out.OpenQuestions = append(out.OpenQuestions, domain.OpenQuestionDigest{Question: clip(question, maxQuestionLen), Digest: clip(digest, 800)})
		}
	}
	return out, nil
}

// synthesize writes the narrative over the computed numbers. A failed
// synthesis leaves a report with the numbers and frequency-counted themes, and
// says so; it never fails the study.
func (s *Service) synthesize(ctx context.Context, st domain.Study, a domain.Audience, all []domain.StudyAnswer, rt domain.Agent) domain.Report {
	report := computeReport(st, all)
	report.GeneratedAt = s.d.Now()
	ok := okAnswers(all)
	ids := make([]string, len(ok))
	byID := map[string]domain.StudyAnswer{}
	for i, ans := range ok {
		ids[i] = "p" + strconv.Itoa(i+1)
		byID[ids[i]] = ans
	}

	var result synthResult
	err := s.askJSON(ctx, rt, askRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: prompt.Text(synthSystemKey)},
			{Role: domain.RoleUser, Content: synthUserKey.Render(synthUserInput{
				Title: st.Title, Goal: st.Goal, Product: st.ProductSnapshot, Language: a.Brief.Language, Currency: st.Currency,
				Stats: statsText(report), Segments: segmentLines(report), Personas: personaDigests(ids, ok), Questions: questionLines(st.Questions),
			})},
		},
		Label: "audience-study:report", Title: "write study report", MaxTurns: 6,
	}, func(raw string) error {
		got, err := parseSynthesis(raw)
		if err != nil {
			return err
		}
		result = got
		return nil
	})
	if err != nil {
		if ctx.Err() == nil {
			log.Warn().Err(err).Str("study_id", st.ID.String()).Msg("audience: synthesis failed; the report keeps its numbers")
		}
		report.SynthesisFailed = true
		report.Objections = frequencyThemes(ok, func(a domain.StudyAnswer) []string { return a.Answers.Objections })
		report.Needs = frequencyThemes(ok, func(a domain.StudyAnswer) []string { return a.Answers.Missing })
		report.Alternatives = frequencyThemes(ok, func(a domain.StudyAnswer) []string { return a.Answers.Alternatives })
		report.Quotes = fallbackQuotes(ok)
		report.Summary = fmt.Sprintf("The written analysis could not be produced. The numbers below come straight from the %d answers: mean purchase intent %.1f of 10, %d yes, %d maybe, %d no.",
			report.Metrics.Respondents, report.Metrics.IntentMean, report.Metrics.Yes, report.Metrics.Maybe, report.Metrics.No)
	} else {
		report.Summary = result.Summary
		report.Verdict.Rationale = result.VerdictRationale
		for i := range report.Segments {
			report.Segments[i].Insight = result.SegmentInsights[strings.ToLower(report.Segments[i].Name)]
		}
		report.Objections = themesFrom(result.Objections, byID)
		report.Needs = themesFrom(result.Needs, byID)
		report.Alternatives = themesFrom(result.Alternatives, byID)
		report.Quotes = quotesFrom(result.Quotes, byID)
		if len(report.Quotes) == 0 {
			report.Quotes = fallbackQuotes(ok)
		}
		report.Recommendations = nonNilRecs(result.Recommendations)
		report.OpenQuestions = result.OpenQuestions
	}
	report.Methodology = methodologyKey.Render(methodologyInput{
		Personas: len(ok), Segments: len(report.Segments), Calls: StudyCalls(len(all)), Currency: st.Currency, PriceTests: strings.Join(priceStrings(st.PricePoints), ", "),
	})
	report.Disclaimer = prompt.Text(disclaimerKey)
	return report
}

func nonNilRecs(r []domain.Recommendation) []domain.Recommendation {
	if r == nil {
		return []domain.Recommendation{}
	}
	return r
}

func statsText(r domain.Report) string {
	m := r.Metrics
	lines := []string{
		fmt.Sprintf("Respondents: %d (%d could not answer)", m.Respondents, m.Failed),
		fmt.Sprintf("Mean purchase intent: %.2f of 10 (index %.0f of 100, verdict %s)", m.IntentMean, r.Verdict.Score, r.Verdict.Label),
		fmt.Sprintf("Verdicts: %d yes, %d maybe, %d no; would pay for it today: %.0f%%", m.Yes, m.Maybe, m.No, m.WouldPayNowPct),
	}
	if m.Price.Answered > 0 {
		lines = append(lines, fmt.Sprintf("Highest price they would pay (%s): median %s, mean %s, range %s to %s; %d would pay nothing",
			m.Price.Currency, trimFloat(m.Price.Median), trimFloat(m.Price.Mean), trimFloat(m.Price.Min), trimFloat(m.Price.Max), m.Price.NotWilling))
	}
	for _, t := range m.Price.Tests {
		lines = append(lines, fmt.Sprintf("At %s %s: %.0f%% would buy (%d asked)", trimFloat(t.Price), m.Price.Currency, t.BuyPct, t.Asked))
	}
	return strings.Join(lines, "\n")
}

func segmentLines(r domain.Report) []string {
	var out []string
	for _, s := range r.Segments {
		out = append(out, fmt.Sprintf("- %s: %d people, mean intent %.1f, %.0f%% yes", s.Name, s.Respondents, s.IntentMean, s.YesPct))
	}
	return out
}

func questionLines(qs []string) []string {
	var out []string
	for _, q := range qs {
		out = append(out, "- "+q)
	}
	return out
}

func personaDigests(ids []string, ok []domain.StudyAnswer) []string {
	out := make([]string, len(ok))
	for i, a := range ok {
		ans := a.Answers
		var b strings.Builder
		fmt.Fprintf(&b, "%s — %s", ids[i], a.Persona.Name)
		if a.Persona.Role != "" {
			b.WriteString(", " + a.Persona.Role)
		}
		if a.Persona.Segment != "" {
			b.WriteString(" [" + a.Persona.Segment + "]")
		}
		fmt.Fprintf(&b, "\nintent %d/10, verdict %s", ans.IntentScore, ans.Verdict)
		if ans.MaxPrice != nil {
			fmt.Fprintf(&b, ", max price %s", trimFloat(*ans.MaxPrice))
		}
		if ans.Reasoning != "" {
			b.WriteString("\nreasoning: " + ans.Reasoning)
		}
		for _, kv := range []struct {
			label string
			items []string
		}{{"missing", ans.Missing}, {"objections", ans.Objections}, {"uses instead", ans.Alternatives}} {
			if len(kv.items) > 0 {
				b.WriteString("\n" + kv.label + ": " + strings.Join(kv.items, "; "))
			}
		}
		if ans.Quote != "" {
			b.WriteString("\nquote: " + ans.Quote)
		}
		for _, qa := range ans.Answers {
			b.WriteString("\nQ: " + qa.Question + "\nA: " + qa.Answer)
		}
		out[i] = b.String()
	}
	return out
}

func themesFrom(raw []rawTheme, byID map[string]domain.StudyAnswer) []domain.Theme {
	out := []domain.Theme{}
	total := len(byID)
	for _, t := range raw {
		seen := map[string]bool{}
		var names []string
		for _, id := range t.Personas {
			id = strings.ToLower(strings.TrimSpace(id))
			if a, ok := byID[id]; ok && !seen[id] {
				seen[id] = true
				names = append(names, a.Persona.Name)
			}
		}
		if len(names) == 0 {
			continue // a theme nobody is credited with is not a finding
		}
		out = append(out, domain.Theme{Label: t.Label, Count: len(names), Pct: pct(len(names), total), Personas: names})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// quotesFrom keeps the quotes that really are in the persona's own words.
func quotesFrom(raw []rawQuote, byID map[string]domain.StudyAnswer) []domain.ReportQuote {
	out := []domain.ReportQuote{}
	seen := map[string]bool{}
	for _, q := range raw {
		a, ok := byID[strings.ToLower(strings.TrimSpace(q.Persona))]
		if !ok || seen[q.Text] || !saidBy(a, q.Text) {
			continue
		}
		seen[q.Text] = true
		out = append(out, domain.ReportQuote{Persona: a.Persona.Name, Segment: a.Persona.Segment, Role: a.Persona.Role, Text: q.Text, Topic: q.Topic, Sentiment: q.Sentiment})
	}
	return out
}

func saidBy(a domain.StudyAnswer, text string) bool {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	want := norm(strings.Trim(text, `"“”' `))
	if want == "" {
		return false
	}
	have := []string{a.Answers.Quote, a.Answers.Reasoning}
	for _, qa := range a.Answers.Answers {
		have = append(have, qa.Answer)
	}
	for _, h := range have {
		if strings.Contains(norm(h), want) {
			return true
		}
	}
	return false
}

// fallbackQuotes picks the keenest and the coldest voices when the synthesis
// chose none: each person's own quote, highest and lowest intent first.
func fallbackQuotes(ok []domain.StudyAnswer) []domain.ReportQuote {
	withQuote := make([]domain.StudyAnswer, 0, len(ok))
	for _, a := range ok {
		if a.Answers.Quote != "" {
			withQuote = append(withQuote, a)
		}
	}
	sort.SliceStable(withQuote, func(i, j int) bool { return withQuote[i].Answers.IntentScore > withQuote[j].Answers.IntentScore })
	pick := func(a domain.StudyAnswer, sentiment string) domain.ReportQuote {
		return domain.ReportQuote{Persona: a.Persona.Name, Segment: a.Persona.Segment, Role: a.Persona.Role, Text: a.Answers.Quote, Sentiment: sentiment}
	}
	out := []domain.ReportQuote{}
	n := len(withQuote)
	for i := 0; i < n && i < 3; i++ {
		out = append(out, pick(withQuote[i], "positive"))
	}
	for i := n - 1; i >= 3 && i >= n-3; i-- {
		out = append(out, pick(withQuote[i], "negative"))
	}
	return out
}

// frequencyThemes counts identical short answers; the fallback when no
// synthesis could group them.
func frequencyThemes(ok []domain.StudyAnswer, pick func(domain.StudyAnswer) []string) []domain.Theme {
	type entry struct {
		label string
		names []string
	}
	index := map[string]*entry{}
	var order []string
	for _, a := range ok {
		seen := map[string]bool{}
		for _, item := range pick(a) {
			key := strings.ToLower(strings.TrimSpace(item))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			e := index[key]
			if e == nil {
				e = &entry{label: item}
				index[key] = e
				order = append(order, key)
			}
			e.names = append(e.names, a.Persona.Name)
		}
	}
	themes := []domain.Theme{}
	for _, k := range order {
		e := index[k]
		themes = append(themes, domain.Theme{Label: e.label, Count: len(e.names), Pct: pct(len(e.names), len(ok)), Personas: e.names})
	}
	sort.SliceStable(themes, func(i, j int) bool { return themes[i].Count > themes[j].Count })
	if len(themes) > 8 {
		themes = themes[:8]
	}
	return themes
}
