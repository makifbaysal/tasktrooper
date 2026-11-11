package audience

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestSegmentsShareTheTargetInProportionAndUnsetOnesShareTheRest(t *testing.T) {
	slots := allocateSegments([]domain.AudienceSegment{{Name: "A", Count: 6}, {Name: "B", Count: 3}, {Name: "C"}}, 12)
	got := map[string]int{}
	total := 0
	for _, s := range slots {
		got[s.name] = s.n
		total += s.n
	}
	assert.Equal(t, 12, total)
	assert.Equal(t, map[string]int{"A": 5, "B": 3, "C": 4}, map[string]int{"A": got["A"] - 1 + 1, "B": got["B"], "C": got["C"]}, "A and C are close: C weighs as the average set segment")
	assert.Equal(t, []segmentSlot{{n: 7}}, allocateSegments(nil, 7))
	assert.Equal(t, 10, allocateSegments([]domain.AudienceSegment{{Name: "Only"}}, 10)[0].n)
}

func TestAnswersAreReadLeniently(t *testing.T) {
	got, err := parseAnswers("```json\n"+`{"intent_score":"8","verdict":"Definitely","would_pay":"yes","max_price":"$12,50","price_reactions":[{"price":"9","would_buy":"true"},{"price":50,"would_buy":true}],"missing":"SSO","objections":["a","  "],"answers":[{"question":"q","answer":"a"},{"question":"","answer":"x"}]}`+"\n```", []float64{9, 19})
	require.NoError(t, err)
	assert.Equal(t, 8, got.IntentScore)
	assert.Equal(t, "yes", got.Verdict, "an unknown verdict is derived from the score")
	assert.True(t, got.WouldPayNow)
	require.NotNil(t, got.MaxPrice)
	assert.Equal(t, 12.5, *got.MaxPrice)
	assert.Equal(t, []domain.PriceReaction{{Price: 9, WouldBuy: true}}, got.PriceReactions, "only the asked prices")
	assert.Equal(t, []string{"SSO"}, got.Missing)
	assert.Equal(t, []string{"a"}, got.Objections)
	assert.Len(t, got.Answers, 1)

	clamped, err := parseAnswers(`{"intent_score":42}`, nil)
	require.NoError(t, err)
	assert.Equal(t, 10, clamped.IntentScore)

	_, err = parseAnswers(`{"verdict":"yes"}`, nil)
	assert.ErrorContains(t, err, "intent_score")
	_, err = parseAnswers(`just words`, nil)
	assert.Error(t, err)
}

func TestHistogramBucketsKeepEveryValueAndRoundEdges(t *testing.T) {
	h := histogram([]float64{5, 9, 15, 20, 20})
	total := 0
	for _, b := range h {
		total += b.Count
	}
	assert.Equal(t, 5, total)
	assert.Equal(t, 4.0, h[0].From, "edges are round numbers at or below the smallest value")
	assert.GreaterOrEqual(t, h[len(h)-1].To, 20.0)
	assert.Equal(t, []domain.Bucket{{From: 7, To: 7, Count: 3}}, histogram([]float64{7, 7, 7}))
	assert.Empty(t, histogram(nil))
}

func TestPercentilesInterpolate(t *testing.T) {
	assert.Equal(t, 12.0, percentile([]float64{5, 9, 15, 20}, .5))
	assert.Equal(t, 5.0, percentile([]float64{5}, .75))
	assert.Equal(t, 0.0, percentile(nil, .5))
}

func TestVerdictLabelsFollowTheScore(t *testing.T) {
	for score, want := range map[float64]string{85: "strong", 70: "strong", 55: "promising", 35: "weak", 10: "poor"} {
		assert.Equal(t, want, verdictLabel(score), "%v", score)
	}
}

func TestThemesWithoutCreditedPeopleAreDropped(t *testing.T) {
	byID := map[string]domain.StudyAnswer{"p1": {Persona: domain.PersonaSnapshot{Name: "Ayse"}}, "p2": {Persona: domain.PersonaSnapshot{Name: "Can"}}}
	got := themesFrom([]rawTheme{{Label: "Price", Personas: []string{"P1", "p1", "p2"}}, {Label: "Ghost", Personas: []string{"p9"}}}, byID)
	require.Len(t, got, 1)
	assert.Equal(t, domain.Theme{Label: "Price", Count: 2, Pct: 100, Personas: []string{"Ayse", "Can"}}, got[0])
}

func TestBriefLimitsAreEnforced(t *testing.T) {
	_, err := NormalizeBrief(domain.AudienceBrief{Product: "p", Segments: make([]domain.AudienceSegment, 0)})
	assert.NoError(t, err)
	many := make([]domain.AudienceSegment, 9)
	for i := range many {
		many[i] = domain.AudienceSegment{Name: string(rune('a' + i))}
	}
	_, err = NormalizeBrief(domain.AudienceBrief{Product: "p", Segments: many})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
	b, err := NormalizeBrief(domain.AudienceBrief{Product: "  p  ", Segments: []domain.AudienceSegment{{Name: " "}, {Name: " X ", Count: -3}}})
	require.NoError(t, err)
	assert.Equal(t, "p", b.Product)
	assert.Equal(t, []domain.AudienceSegment{{Name: "X"}}, b.Segments)
}

func TestRotationGivesEveryLineEveryPositionAndPageOrderAlternates(t *testing.T) {
	first := map[int]int{}
	for ordinal := 1; ordinal <= 6; ordinal++ {
		perm := rotation(3, ordinal)
		assert.ElementsMatch(t, []int{0, 1, 2}, perm)
		first[perm[0]]++
	}
	assert.Equal(t, map[int]int{0: 2, 1: 2, 2: 2}, first)
	swapped := 0
	for ordinal := 1; ordinal <= 8; ordinal++ {
		if abSwapped(ordinal) {
			swapped++
		}
	}
	assert.Equal(t, 4, swapped)
}

func TestMessageRatingsAreStoredAgainstTheLinesOwnNumberNotItsPosition(t *testing.T) {
	perm := []int{2, 0, 1} // shown: line 3, line 1, line 2
	raw := `{"ratings":[{"line":1,"clarity":9,"relevance":9,"credibility":9,"appeal":9},{"line":2,"clarity":1,"relevance":1,"credibility":1,"appeal":1},
	{"line":3,"clarity":5,"relevance":5,"credibility":5,"appeal":15}],"best":1,"stronger_line":"better"}`
	got, err := parseMessageAnswers(raw, perm)
	require.NoError(t, err)
	assert.Equal(t, 3, got.BestVariant, "the first line shown was line 3")
	assert.Equal(t, 9, got.IntentScore)
	byVariant := map[int]int{}
	for _, r := range got.Ratings {
		byVariant[r.Variant] = r.Appeal
	}
	assert.Equal(t, map[int]int{3: 9, 1: 1, 2: 10}, byVariant, "scores are clamped to 10")

	_, err = parseMessageAnswers(`{"ratings":[{"line":1,"clarity":9,"relevance":9,"credibility":9,"appeal":9}]}`, perm)
	assert.Error(t, err, "every line must be rated")
	_, err = parseMessageAnswers(`{"ratings":[{"line":1,"clarity":9},{"line":2,"clarity":9},{"line":3,"clarity":9}]}`, perm)
	assert.Error(t, err, "all four dimensions are needed")
}

func TestAnABAnswerKeepsEachScorecardWithItsVersionWhenThePagesWereSwapped(t *testing.T) {
	card := func(act, clarity int, objection string) string {
		return `{"first_impression":"f","clarity_score":` + itoa(clarity) + `,"intent_score":` + itoa(act) + `,"verdict":"maybe","objections":["` + objection + `"]}`
	}
	raw := `{"page_1":` + card(9, 8, "p1") + `,"page_2":` + card(2, 3, "p2") + `,"preferred":1,"preference_reason":"r"}`
	plain, err := parseABAnswers(raw, false)
	require.NoError(t, err)
	assert.Equal(t, "a", plain.Preferred)
	assert.Equal(t, "a", plain.ShownFirst)
	assert.Equal(t, 9, plain.PageA.ActScore)
	assert.Equal(t, 9, plain.IntentScore, "the preferred version's act-now score")
	swapped, err := parseABAnswers(raw, true)
	require.NoError(t, err)
	assert.Equal(t, "b", swapped.Preferred, "version 1 was B")
	assert.Equal(t, "b", swapped.ShownFirst)
	assert.Equal(t, 9, swapped.PageB.ActScore)
	assert.Equal(t, 2, swapped.PageA.ActScore)
	assert.Equal(t, []string{"p1"}, swapped.PageB.Objections)
	assert.Equal(t, 9, swapped.IntentScore)

	_, err = parseABAnswers(`{"page_1":`+card(1, 1, "x")+`,"page_2":`+card(1, 1, "y")+`,"preferred":3}`, false)
	assert.Error(t, err)
	_, err = parseABAnswers(`{"page_1":`+card(1, 1, "x")+`,"preferred":1}`, false)
	assert.Error(t, err, "both scorecards are needed")
	_, err = parseABAnswers(`{"page_1":{"intent_score":1},"page_2":`+card(1, 1, "y")+`,"preferred":1}`, false)
	assert.Error(t, err, "clarity is part of the scorecard")
}

func TestPairedMetricComparesTheSamePeopleAcrossTheTwoVersions(t *testing.T) {
	m := pairedMetric([]float64{8, 6, 4, 7}, []float64{5, 7, 4, 3})
	assert.Equal(t, 6.25, m.A.Mean)
	assert.Equal(t, 4.75, m.B.Mean)
	assert.Equal(t, -1.5, m.Diff)
	assert.Equal(t, 2, m.AHigher)
	assert.Equal(t, 1, m.BHigher)
	assert.Equal(t, 1, m.Same)
	assert.Equal(t, "medium", m.EffectLabel)
	assert.Equal(t, "negligible", pairedMetric([]float64{5}, []float64{9}).EffectLabel, "one person has no spread to measure")
	same := pairedMetric([]float64{5, 5}, []float64{5, 5})
	assert.Equal(t, 0.0, same.EffectSize)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func TestABConfidenceNeverCallsASmallGroupClear(t *testing.T) {
	assert.Equal(t, "clear", abConfidence(18, 2))
	assert.Equal(t, "lean", abConfidence(4, 0))
	assert.Equal(t, "lean", abConfidence(5, 3))
	assert.Equal(t, "toss-up", abConfidence(5, 5))
	assert.Equal(t, "toss-up", abConfidence(0, 0))
}

func TestAScriptIsReadFromSectionsOrAFlatListAndBounded(t *testing.T) {
	sc, err := parseScript("```json\n{\"sections\":[{\"title\":\"A\",\"questions\":[\"q1\",\"q2\"]},{\"title\":\"B\",\"questions\":\"q3\"}]}\n```")
	require.NoError(t, err)
	assert.Equal(t, []string{"q1", "q2", "q3"}, sc.Flat())
	flat, err := parseScript(`{"questions":["a","b"]}`)
	require.NoError(t, err)
	assert.Len(t, flat.Sections, 1)
	_, err = parseScript(`{"sections":[]}`)
	assert.Error(t, err)
}
