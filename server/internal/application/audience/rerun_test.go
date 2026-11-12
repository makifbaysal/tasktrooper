package audience_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/audience/audiencetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestARerunAsksTheSamePeopleAgainAndTheTwoReportsCompare(t *testing.T) {
	var second atomic.Bool
	improved := map[string]string{
		"Person 1": audiencetest.AnswerJSON(6, "maybe", 9, true),
		"Person 2": audiencetest.AnswerJSON(5, "maybe", 9, true),
		"Person 3": audiencetest.AnswerJSON(9, "yes", 20, true),
		"Person 4": audiencetest.AnswerJSON(9, "yes", 20, true),
	}
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string {
		if second.Load() {
			return improved[n]
		}
		return answers[n]
	}))
	ctx := context.Background()
	a := readyAudience(t, f, 4)
	planned, err := f.svc.PlanStudy(ctx, a.ID, audience.PlanStudyInput{Title: "Would they pay?", Questions: []string{"What would convince you?"}, PricePoints: []float64{9, 19}, Currency: "USD"})
	require.NoError(t, err)
	_, err = f.svc.Rerun(ctx, planned.ID, "x")
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "only a finished study can be run again")
	f.approve(t, planned.ID)
	_, err = f.svc.StartStudy(ctx, planned.ID)
	require.NoError(t, err)
	first := f.waitStudy(t, planned.ID, domain.StudyDone)

	second.Store(true)
	rerun, err := f.svc.Rerun(ctx, first.ID, "Added a free tier.")
	require.NoError(t, err)
	assert.Equal(t, "Would they pay? (re-run)", rerun.Title)
	require.NotNil(t, rerun.ParentStudyID)
	assert.Equal(t, first.ID, *rerun.ParentStudyID)
	assert.Equal(t, "Added a free tier.", rerun.Changes)
	assert.Equal(t, domain.StudyDraft, rerun.Status)
	assert.Equal(t, []float64{9, 19}, rerun.PricePoints)
	assert.Equal(t, first.Questions, rerun.Questions, "the same questions, so the answers compare")
	assert.Equal(t, domain.ScriptAwaitingApproval, rerun.ScriptState, "the person approves the script again; nothing runs on its own")
	assert.Zero(t, rerun.ScriptCalls, "the script is copied, not written again")

	f.approve(t, rerun.ID)
	_, err = f.svc.StartStudy(ctx, rerun.ID)
	require.NoError(t, err)
	done := f.waitStudy(t, rerun.ID, domain.StudyDone)

	asked := f.model.CallsWith("You are a character in a market-research interview")
	require.Len(t, asked, 8)
	assert.Contains(t, asked[len(asked)-1].Messages[1].Content, "Added a free tier.", "the new people-facing product text carries the note of what changed")
	assert.NotContains(t, asked[0].Messages[1].Content, "Added a free tier.")

	cmp, err := f.svc.CompareStudies(ctx, first.ID, done.ID)
	require.NoError(t, err)
	assert.Equal(t, 57.5, first.Report.Verdict.Score)
	assert.Equal(t, 72.5, done.Report.Verdict.Score)
	assert.Equal(t, 15.0, cmp.Verdict.Delta)
	assert.Equal(t, "Added a free tier.", cmp.Changes)
	assert.Equal(t, 4, cmp.Paired)
	assert.Equal(t, 2, cmp.Improved, "persons 1 and 4 scored higher")
	assert.Equal(t, 0, cmp.Worse)
	assert.Equal(t, 2, cmp.Same)
	require.NotEmpty(t, cmp.People)
	assert.Equal(t, 4, cmp.People[0].AfterIntent-cmp.People[0].BeforeIntent, "the biggest mover comes first")
	metrics := map[string]audience.MetricChange{}
	for _, m := range cmp.Metrics {
		metrics[m.Key] = m
	}
	assert.Equal(t, 1.5, metrics["intent_mean"].Delta)
	assert.Equal(t, 0.0, metrics["yes_pct"].Delta, "persons 3 and 4 said yes both times")
	assert.Equal(t, 25.0, metrics["maybe_pct"].Delta)
	assert.Equal(t, -25.0, metrics["no_pct"].Delta)

	_, err = f.svc.CompareStudies(ctx, first.ID, first.ID)
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
	_, err = f.svc.CompareStudies(ctx, uuid.New(), done.ID)
	assert.Error(t, err)
}

func TestThemesAreFollowedAcrossTwoReportsByWhatTheySay(t *testing.T) {
	before := []domain.Theme{
		{Label: "Too expensive", Count: 3, Pct: 75},
		{Label: "No mobile app", Count: 2, Pct: 50},
		{Label: "Unclear who it is for", Count: 1, Pct: 25},
	}
	after := []domain.Theme{
		{Label: "Price is too expensive for me", Count: 1, Pct: 25},
		{Label: "No mobile app", Count: 2, Pct: 50},
		{Label: "Missing integrations", Count: 2, Pct: 50},
	}
	got := map[string]audience.ThemeChange{}
	for _, c := range audience.CompareThemesForTest(before, after) {
		got[c.Label] = c
	}
	assert.Equal(t, "down", got["Price is too expensive for me"].Status, "the same complaint worded differently is one theme: 75% to 25%")
	assert.Equal(t, 3, got["Price is too expensive for me"].BeforeCount)
	assert.Equal(t, "same", got["No mobile app"].Status)
	assert.Equal(t, "new", got["Missing integrations"].Status)
	assert.Equal(t, "resolved", got["Unclear who it is for"].Status)
}
