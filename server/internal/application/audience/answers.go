package audience

import (
	"fmt"
	"sort"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func clamp10(v int) int {
	if v < 0 {
		return 0
	}
	if v > 10 {
		return 10
	}
	return v
}

// readOpen reads what every kind shares: the one-sentence quote and the
// answers to the script's questions.
func readOpen(m map[string]any, out *domain.StudyAnswers) {
	out.Quote = clip(strOf(m, "quote"), 300)
	for _, qa := range objectsOf(m, "answers") {
		q, a := strOf(qa, "question"), strOf(qa, "answer")
		if q != "" && a != "" {
			out.Answers = append(out.Answers, domain.QuestionAnswer{Question: clip(q, maxQuestionLen), Answer: clip(a, 600)})
		}
	}
}

// parseLandingAnswers reads a visitor's answers about a page. The act-now
// score is the number the report counts, as the purchase intent is for an
// interview.
func parseLandingAnswers(raw string) (domain.StudyAnswers, error) {
	out, err := parseAnswers(raw, nil)
	if err != nil {
		return out, err
	}
	var m map[string]any
	if err := decodeObject(raw, &m); err != nil {
		return out, err
	}
	clarity, ok := intOf(m, "clarity_score", "clarity")
	if !ok {
		return domain.StudyAnswers{}, fmt.Errorf(`"clarity_score" is missing`)
	}
	out.MaxPrice, out.PriceReactions, out.Alternatives = nil, nil, nil
	out.Landing = &domain.LandingAnswer{
		FirstImpression: clip(strOf(m, "first_impression"), 400),
		ClarityScore:    clamp10(clarity),
		TrustSignals:    listOf(m, 6, 160, "trust_signals"),
		Change:          clip(strOf(m, "change", "one_change"), 300),
	}
	return out, nil
}

// parseMessageAnswers reads one reader's ratings of the candidate lines. perm
// is the order the lines were shown in (presented position -> 0-based line),
// so every stored number is about the line's own place in the study.
func parseMessageAnswers(raw string, perm []int) (domain.StudyAnswers, error) {
	var m map[string]any
	if err := decodeObject(raw, &m); err != nil {
		return domain.StudyAnswers{}, err
	}
	n := len(perm)
	seen := map[int]domain.VariantRating{}
	for _, r := range objectsOf(m, "ratings") {
		line, ok := intOf(r, "line", "variant", "n")
		if !ok || line < 1 || line > n {
			continue
		}
		rt := domain.VariantRating{Variant: perm[line-1] + 1, Comment: clip(strOf(r, "comment"), 240)}
		var all [4]bool
		get := func(dst *int, idx int, key string) {
			if v, ok := intOf(r, key); ok {
				*dst, all[idx] = clamp10(v), true
			}
		}
		get(&rt.Clarity, 0, "clarity")
		get(&rt.Relevance, 1, "relevance")
		get(&rt.Credibility, 2, "credibility")
		get(&rt.Appeal, 3, "appeal")
		if all[0] && all[1] && all[2] && all[3] {
			seen[rt.Variant] = rt
		}
	}
	if len(seen) < n {
		return domain.StudyAnswers{}, fmt.Errorf(`"ratings" must rate every one of the %d lines with clarity, relevance, credibility and appeal`, n)
	}
	out := domain.StudyAnswers{}
	for _, rt := range seen {
		out.Ratings = append(out.Ratings, rt)
	}
	sort.Slice(out.Ratings, func(i, j int) bool { return out.Ratings[i].Variant < out.Ratings[j].Variant })

	best := 0
	if b, ok := intOf(m, "best", "best_line"); ok && b >= 1 && b <= n {
		best = perm[b-1] + 1
	}
	if best == 0 {
		for _, rt := range out.Ratings {
			if best == 0 || rt.Appeal > seen[best].Appeal {
				best = rt.Variant
			}
		}
	}
	out.BestVariant = best
	out.IntentScore = seen[best].Appeal
	out.Verdict = verdictFromScore(out.IntentScore)
	out.Reasoning = clip(strOf(m, "best_reason", "reasoning"), 600)
	out.Stronger = clip(strOf(m, "stronger_line", "stronger"), 300)
	readOpen(m, &out)
	return out, nil
}

// parseAssessment reads one version's scorecard in an A/B test.
func parseAssessment(m map[string]any) (*domain.PageAssessment, error) {
	if m == nil {
		return nil, fmt.Errorf("the scorecard is missing")
	}
	act, ok := intOf(m, "intent_score", "act_score", "score")
	if !ok {
		return nil, fmt.Errorf(`"intent_score" is missing`)
	}
	clarity, ok := intOf(m, "clarity_score", "clarity")
	if !ok {
		return nil, fmt.Errorf(`"clarity_score" is missing`)
	}
	act = clamp10(act)
	out := &domain.PageAssessment{
		FirstImpression: clip(strOf(m, "first_impression"), 400),
		ClarityScore:    clamp10(clarity),
		ActScore:        act,
		Verdict:         oneOf(strOf(m, "verdict"), "yes", "maybe", "no"),
		Objections:      listOf(m, 6, 160, "objections"),
		Missing:         listOf(m, 6, 160, "missing"),
		TrustSignals:    listOf(m, 6, 160, "trust_signals"),
		Change:          clip(strOf(m, "change", "one_change"), 300),
	}
	if out.Verdict == "" {
		out.Verdict = verdictFromScore(act)
	}
	return out, nil
}

// parseABAnswers reads one visitor's two scorecards and their preference.
// swapped says page 2 was version A.
func parseABAnswers(raw string, swapped bool) (domain.StudyAnswers, error) {
	var m map[string]any
	if err := decodeObject(raw, &m); err != nil {
		return domain.StudyAnswers{}, err
	}
	first, err := parseAssessment(objectAt(m, "page_1"))
	if err != nil {
		return domain.StudyAnswers{}, fmt.Errorf("page_1: %w", err)
	}
	second, err := parseAssessment(objectAt(m, "page_2"))
	if err != nil {
		return domain.StudyAnswers{}, fmt.Errorf("page_2: %w", err)
	}
	pref, ok := intOf(m, "preferred", "winner")
	if !ok || (pref != 1 && pref != 2) {
		return domain.StudyAnswers{}, fmt.Errorf(`"preferred" must be 1 or 2`)
	}
	out := domain.StudyAnswers{ShownFirst: "a", PageA: first, PageB: second}
	if swapped {
		out.PageA, out.PageB, out.ShownFirst = second, first, "b"
	}
	shownPref := first
	if pref == 2 {
		shownPref = second
	}
	out.Preferred = "a"
	if (pref == 1) == swapped {
		out.Preferred = "b"
	}
	out.IntentScore, out.Verdict = shownPref.ActScore, shownPref.Verdict
	out.Reasoning = clip(strOf(m, "preference_reason", "reasoning", "reason"), 600)
	readOpen(m, &out)
	return out, nil
}

func objectAt(m map[string]any, key string) map[string]any {
	o, _ := m[key].(map[string]any)
	return o
}
