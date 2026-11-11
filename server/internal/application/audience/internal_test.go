package audience

import (
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
