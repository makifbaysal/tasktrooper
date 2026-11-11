package audience_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/audience/audiencetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fixture struct {
	svc   *audience.Service
	store *audiencetest.Store
	model *audiencetest.Model
}

func newFixture(t *testing.T, model *audiencetest.Model) *fixture {
	t.Helper()
	return newFixtureWith(t, model, fakePages{})
}

func newFixtureWith(t *testing.T, model *audiencetest.Model, pages audience.PageReader) *fixture {
	t.Helper()
	store := audiencetest.NewStore()
	svc := audience.NewService(audience.Deps{
		Store: store, Agents: audiencetest.OneAgent(), LLM: model, Pages: pages,
		Brief:       func(context.Context, uuid.UUID) (string, error) { return "# acme\nGo API plus a React app.", nil },
		Repos:       repos{},
		Concurrency: 3, CallTimeout: 10 * time.Second,
	})
	return &fixture{svc: svc, store: store, model: model}
}

type repos struct{}

func (repos) List(context.Context) ([]domain.Repository, error) {
	return []domain.Repository{{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "acme"}}, nil
}

func (f *fixture) createAudience(t *testing.T, target int) domain.Audience {
	t.Helper()
	a, err := f.svc.CreateAudience(context.Background(), audience.CreateAudienceInput{
		Name: "Freelance designers", Repository: "Acme", PersonaTarget: target,
		Brief: domain.AudienceBrief{
			Product:  "A kanban board where AI agents do the work.",
			Segments: []domain.AudienceSegment{{Name: "Freelancers", Count: 3}, {Name: "Agency leads", Count: 1}}, Pricing: "19 USD per month",
		},
	})
	require.NoError(t, err)
	return a
}

func (f *fixture) waitAudience(t *testing.T, id uuid.UUID, want domain.AudienceStatus) domain.Audience {
	t.Helper()
	var a domain.Audience
	require.Eventually(t, func() bool {
		var err error
		a, err = f.store.GetAudience(context.Background(), id)
		return err == nil && a.Status == want
	}, 5*time.Second, 10*time.Millisecond, "audience never became %s", want)
	return a
}

func (f *fixture) plan(t *testing.T, audienceID uuid.UUID, in audience.PlanStudyInput) (domain.Study, error) {
	t.Helper()
	st, err := f.svc.PlanStudy(context.Background(), audienceID, in)
	if err != nil {
		return st, err
	}
	return f.approve(t, st.ID), nil
}

func (f *fixture) approve(t *testing.T, id uuid.UUID) domain.Study {
	t.Helper()
	st := f.waitScript(t, id, domain.ScriptAwaitingApproval)
	approved, err := f.svc.ApproveScript(context.Background(), id, st.ScriptVersion, "tester")
	require.NoError(t, err)
	return approved
}

func (f *fixture) waitScript(t *testing.T, id uuid.UUID, want domain.ScriptState) domain.Study {
	t.Helper()
	var st domain.Study
	require.Eventually(t, func() bool {
		var err error
		st, err = f.store.GetStudy(context.Background(), id)
		return err == nil && st.ScriptState == want
	}, 5*time.Second, 10*time.Millisecond, "script never became %s (is %s: %s)", want, st.ScriptState, st.Error)
	return st
}

func (f *fixture) waitStudy(t *testing.T, id uuid.UUID, want domain.StudyStatus) domain.Study {
	t.Helper()
	var st domain.Study
	require.Eventually(t, func() bool {
		var err error
		st, err = f.store.GetStudy(context.Background(), id)
		return err == nil && st.Status == want
	}, 5*time.Second, 10*time.Millisecond, "study never became %s (is %s: %s)", want, st.Status, st.Error)
	return st
}

func TestABriefIsValidatedAndTheRepositoryResolvedByName(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(nil))
	ctx := context.Background()
	_, err := f.svc.CreateAudience(ctx, audience.CreateAudienceInput{Name: "x", Brief: domain.AudienceBrief{}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
	_, err = f.svc.CreateAudience(ctx, audience.CreateAudienceInput{Name: " ", Brief: domain.AudienceBrief{Product: "p"}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
	_, err = f.svc.CreateAudience(ctx, audience.CreateAudienceInput{Name: "x", Repository: "nope", Brief: domain.AudienceBrief{Product: "p"}})
	require.ErrorIs(t, err, domain.ErrAudienceInvalid)
	assert.Contains(t, err.Error(), "registered repositories: acme")

	a := f.createAudience(t, 0)
	require.NotNil(t, a.RepositoryID)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", a.RepositoryID.String())
	assert.Equal(t, audience.DefaultPersonas, a.PersonaTarget)
	assert.Equal(t, domain.AudienceDraft, a.Status)
}

func TestBuildingWritesTheRequestedPeopleInTheBriefsSegmentsFromTheProjectsContext(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(nil))
	ctx := context.Background()
	a := f.createAudience(t, 4)

	started, err := f.svc.Build(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AudienceBuilding, started.Status)
	ready := f.waitAudience(t, a.ID, domain.AudienceReady)
	assert.Equal(t, 1, ready.Generation)
	assert.NotNil(t, ready.BuiltAt)

	personas, err := f.store.ListPersonas(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, personas, 4)
	segs := map[string]int{}
	for _, p := range personas {
		segs[p.Segment]++
		assert.Equal(t, 1, p.Generation)
		assert.Equal(t, []string{"too many tools"}, p.Pains, "a single string reads as a list of one")
		assert.Equal(t, "tight", p.Budget)
	}
	assert.Equal(t, map[string]int{"Freelancers": 3, "Agency leads": 1}, segs)

	calls := f.model.CallsWith("You invent the synthetic people")
	require.Len(t, calls, 1, "four people are one call")
	prompt := calls[0].Messages[1].Content
	assert.Contains(t, prompt, "A kanban board where AI agents do the work.")
	assert.Contains(t, prompt, "Go API plus a React app.", "grounded in the project's own brief")
	assert.Contains(t, prompt, "- Freelancers: 3")
	assert.Equal(t, 1, audience.BuildCalls(4))
	assert.Equal(t, 5, audience.BuildCalls(40))
}

func TestMorePeopleThanOneCallWritesAreAskedForInBatchesThatKnowWhoExists(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(nil))
	a := f.createAudience(t, 10)
	_, err := f.svc.Build(context.Background(), a.ID)
	require.NoError(t, err)
	f.waitAudience(t, a.ID, domain.AudienceReady)

	calls := f.model.CallsWith("You invent the synthetic people")
	require.Len(t, calls, 2)
	second := calls[1].Messages[1].Content
	assert.Contains(t, second, "These people already exist")
	assert.Contains(t, second, "Person 1, Designer")
	personas, _ := f.store.ListPersonas(context.Background(), a.ID)
	assert.Len(t, personas, 10)
}

func TestAModelThatWritesNoUsablePeopleFailsTheBuildAndSaysSo(t *testing.T) {
	m := &audiencetest.Model{}
	m.Say("You invent the synthetic people", "Sorry, I cannot do that.")
	f := newFixture(t, m)
	a := f.createAudience(t, 4)
	_, err := f.svc.Build(context.Background(), a.ID)
	require.NoError(t, err)
	failed := f.waitAudience(t, a.ID, domain.AudienceFailed)
	assert.Contains(t, failed.Error, "not valid")
	assert.Len(t, m.CallsWith("You invent the synthetic people"), 2, "one retry naming the problem")
}

func TestARebuildReplacesThePeopleAndAFailedRebuildKeepsTheOldOnes(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(nil))
	ctx := context.Background()
	a := f.createAudience(t, 2)
	_, err := f.svc.Build(ctx, a.ID)
	require.NoError(t, err)
	f.waitAudience(t, a.ID, domain.AudienceReady)
	before, _ := f.store.ListPersonas(ctx, a.ID)

	edited := domain.AudienceBrief{Product: "Now with invoices."}
	_, err = f.svc.UpdateAudience(ctx, a.ID, audience.UpdateAudienceInput{Brief: &edited})
	require.NoError(t, err)
	_, err = f.svc.Build(ctx, a.ID)
	require.NoError(t, err)
	got := f.waitAudience(t, a.ID, domain.AudienceReady)
	assert.Equal(t, 2, got.Generation)
	after, _ := f.store.ListPersonas(ctx, a.ID)
	assert.NotEqual(t, before[0].Name, after[0].Name)

	broken := &audiencetest.Model{}
	broken.Say("You invent the synthetic people", "no")
	bf := newFixture(t, broken)
	bf.store = f.store
	bad := audience.NewService(audience.Deps{Store: f.store, Agents: audiencetest.OneAgent(), LLM: broken, Concurrency: 2})
	_, err = bad.Build(ctx, a.ID)
	require.NoError(t, err)
	failed := f.waitAudience(t, a.ID, domain.AudienceFailed)
	assert.NotEmpty(t, failed.Error)
	kept, _ := f.store.ListPersonas(ctx, a.ID)
	assert.Equal(t, after[0].Name, kept[0].Name, "the people it had are still there")
}

func TestBuildNeedsARuntimeAndRefusesToRunTwice(t *testing.T) {
	m := audiencetest.ScriptedModel(nil)
	m.Delay = 200 * time.Millisecond
	f := newFixture(t, m)
	a := f.createAudience(t, 2)
	_, err := f.svc.Build(context.Background(), a.ID)
	require.NoError(t, err)
	_, err = f.svc.Build(context.Background(), a.ID)
	assert.ErrorIs(t, err, audience.ErrBusy)
	_, err = f.svc.UpdateAudience(context.Background(), a.ID, audience.UpdateAudienceInput{PersonaTarget: ptr(5)})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "no edits while it builds")
	require.NoError(t, f.svc.CancelBuild(context.Background(), a.ID))
	got := f.waitAudience(t, a.ID, domain.AudienceDraft)
	assert.Contains(t, got.Error, "cancelled")

	none := audience.NewService(audience.Deps{Store: f.store, Agents: audiencetest.Agents{}, LLM: m})
	_, err = none.Build(context.Background(), a.ID)
	assert.ErrorIs(t, err, audience.ErrNoRuntime)
	rt, err := none.Runtime(context.Background())
	require.NoError(t, err)
	assert.False(t, rt.Ready)
}

func ptr[T any](v T) *T { return &v }

// ---- studies

func readyAudience(t *testing.T, f *fixture, target int) domain.Audience {
	t.Helper()
	a := f.createAudience(t, target)
	_, err := f.svc.Build(context.Background(), a.ID)
	require.NoError(t, err)
	f.waitAudience(t, a.ID, domain.AudienceReady)
	return a
}

var answers = map[string]string{
	"Person 1": audiencetest.AnswerJSON(2, "no", 5, false),
	"Person 2": audiencetest.AnswerJSON(5, "maybe", 9, true),
	"Person 3": audiencetest.AnswerJSON(9, "yes", 20, true),
	"Person 4": audiencetest.AnswerJSON(7, "yes", 15, true),
}

func TestPlanningAStudyCostsNothingAndSaysWhatItWillCost(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string { return answers[n] }))
	a := readyAudience(t, f, 4)
	before := len(f.model.Calls)

	st, err := f.svc.PlanStudy(context.Background(), a.ID, audience.PlanStudyInput{
		Questions: []string{" What would convince you? ", ""}, PricePoints: []float64{19, 9, 9, 49}, Currency: "eur",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.StudyDraft, st.Status)
	assert.Equal(t, 5, st.CallsEstimate, "four people and the synthesis")
	assert.Equal(t, []float64{9, 19, 49}, st.PricePoints)
	assert.Equal(t, []string{"What would convince you?"}, st.Questions)
	assert.Equal(t, "EUR", st.Currency)
	assert.NotEmpty(t, st.Title)
	assert.Equal(t, before, len(f.model.Calls), "planning asks nobody")

	_, err = f.plan(t, a.ID, audience.PlanStudyInput{PricePoints: []float64{-1}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
	_, err = f.plan(t, a.ID, audience.PlanStudyInput{Questions: []string{"1", "2", "3", "4", "5", "6"}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)

	empty := f.createAudience(t, 4)
	_, err = f.svc.PlanStudy(context.Background(), empty.ID, audience.PlanStudyInput{})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "no people to ask")
}

func TestAStudyAsksEveryoneAndWritesAReportWhoseNumbersAreComputedNotInvented(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string { return answers[n] }))
	ctx := context.Background()
	a := readyAudience(t, f, 4)
	planned, err := f.svc.PlanStudy(ctx, a.ID, audience.PlanStudyInput{Title: "Would they pay?", Questions: []string{"What would convince you?"}, PricePoints: []float64{9, 19}, Currency: "USD"})
	require.NoError(t, err)

	f.approve(t, planned.ID)
	started, err := f.svc.StartStudy(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StudyRunning, started.Status)
	_, err = f.svc.StartStudy(ctx, planned.ID)
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "a study runs once")

	done := f.waitStudy(t, planned.ID, domain.StudyDone)
	assert.Equal(t, 4, done.PersonaDone)
	require.NotNil(t, done.Report)
	r := done.Report
	m := r.Metrics

	assert.Equal(t, 4, m.Respondents)
	assert.Equal(t, 5.75, m.IntentMean)
	assert.Equal(t, 57.5, r.Verdict.Score)
	assert.Equal(t, "promising", r.Verdict.Label)
	assert.Equal(t, "Mean intent is middling.", r.Verdict.Rationale)
	assert.Equal(t, 2, m.Yes)
	assert.Equal(t, 1, m.Maybe)
	assert.Equal(t, 1, m.No)
	assert.Equal(t, []int{0, 0, 1, 0, 0, 1, 0, 1, 0, 1, 0}, m.IntentDistribution)
	assert.Equal(t, 50.0, m.WouldPayNowPct)

	assert.Equal(t, 4, m.Price.Answered)
	assert.Equal(t, 5.0, m.Price.Min)
	assert.Equal(t, 20.0, m.Price.Max)
	assert.Equal(t, 12.0, m.Price.Median)
	assert.Equal(t, 12.25, m.Price.Mean)
	var counted int
	for _, b := range m.Price.Histogram {
		counted += b.Count
	}
	assert.Equal(t, 4, counted)
	require.Len(t, m.Price.Tests, 2)
	assert.Equal(t, domain.PriceTest{Price: 9, Asked: 4, BuyPct: 75}, m.Price.Tests[0])
	assert.Equal(t, 0.0, m.Price.Tests[1].BuyPct, "a reaction to a price that was never asked (99, 5) is dropped; 19 got none")
	assert.Equal(t, 0, m.Price.Tests[1].Asked)

	byName := map[string]domain.SegmentReport{}
	for _, s := range r.Segments {
		byName[s.Name] = s
	}
	assert.Equal(t, 3, byName["Freelancers"].Respondents)
	assert.Equal(t, "Freelancers balk at the price.", byName["Freelancers"].Insight)
	assert.Equal(t, 1, byName["Agency leads"].Respondents)

	require.Len(t, r.Objections, 1)
	assert.Equal(t, domain.Theme{Label: "Too expensive", Count: 3, Pct: 75, Personas: []string{"Person 1", "Person 2", "Person 3"}}, r.Objections[0],
		"counts come from the persona ids the synthesis listed; an unknown id (p99) counts for nobody")
	assert.Equal(t, 2, r.Alternatives[0].Count)
	require.Len(t, r.Quotes, 1, "a quote nobody said is dropped")
	assert.Equal(t, "Person 1", r.Quotes[0].Persona)
	assert.Equal(t, "too pricey for me", r.Quotes[0].Text)
	assert.Equal(t, "Add a free tier", r.Recommendations[0].Title)
	assert.Equal(t, "A free tier.", r.OpenQuestions[0].Digest)
	assert.Contains(t, r.Disclaimer, "synthetic personas")
	assert.Contains(t, r.Methodology, "4 synthetic personas in 2 segment")
	assert.False(t, r.SynthesisFailed)

	detail, err := f.svc.Study(ctx, planned.ID)
	require.NoError(t, err)
	assert.Len(t, detail.Answers, 4)
	assert.Equal(t, "Freelance designers", detail.AudienceName)

	listed, err := f.svc.ListStudies(ctx, nil)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Nil(t, listed[0].Report, "listings carry the status, the study carries the report")

	interview := f.model.CallsWith("You are a character in a market-research interview")
	require.Len(t, interview, 4)
	asked := interview[0].Messages[1].Content
	assert.Contains(t, asked, "A kanban board where AI agents do the work.")
	assert.Contains(t, asked, "Price: 19 USD per month")
	assert.Contains(t, asked, "Go API plus a React app.")
	assert.Contains(t, asked, "9, 19")
	assert.Contains(t, asked, "What would convince you?")
}

func TestInterviewsRunInParallelButNeverMoreThanTheLimit(t *testing.T) {
	m := audiencetest.ScriptedModel(func(n string) string { return answers["Person 2"] })
	m.Delay = 60 * time.Millisecond
	f := newFixture(t, m)
	a := readyAudience(t, f, 8)
	st, err := f.plan(t, a.ID, audience.PlanStudyInput{})
	require.NoError(t, err)
	_, err = f.svc.StartStudy(context.Background(), st.ID)
	require.NoError(t, err)
	f.waitStudy(t, st.ID, domain.StudyDone)
	assert.Equal(t, 3, m.MaxActive, "three at once, no more")
}

func TestAPersonWhoCannotAnswerIsCountedAsFailedNotAsANo(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string {
		if n == "Person 3" {
			return "I refuse to answer in JSON."
		}
		return answers[n]
	}))
	a := readyAudience(t, f, 4)
	st, _ := f.plan(t, a.ID, audience.PlanStudyInput{})
	_, err := f.svc.StartStudy(context.Background(), st.ID)
	require.NoError(t, err)
	done := f.waitStudy(t, st.ID, domain.StudyDone)
	assert.Equal(t, 3, done.PersonaDone)
	assert.Equal(t, 1, done.PersonaFailed)
	assert.Equal(t, 3, done.Report.Metrics.Respondents)
	assert.Equal(t, 1, done.Report.Metrics.Failed)
	assert.Equal(t, 1, done.Report.Metrics.No, "only Person 1 said no")
}

func TestAStudyNobodyAnsweredFailsInsteadOfReportingNothing(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(string) string { return "no json here" }))
	a := readyAudience(t, f, 2)
	st, _ := f.plan(t, a.ID, audience.PlanStudyInput{})
	_, err := f.svc.StartStudy(context.Background(), st.ID)
	require.NoError(t, err)
	failed := f.waitStudy(t, st.ID, domain.StudyFailed)
	assert.Contains(t, failed.Error, "No one answered")
	assert.Nil(t, failed.Report)
}

func TestAFailedSynthesisKeepsTheNumbersAndCountsThemesItself(t *testing.T) {
	// the first matching handler wins, so the broken analyst goes in front
	f := newFixture(t, func() *audiencetest.Model {
		mm := &audiencetest.Model{}
		inner := audiencetest.ScriptedModel(func(n string) string { return answers[n] })
		mm.On("You are a market-research analyst", func(domain.AgentRequest) (string, error) { return "no JSON for you", nil })
		mm.On("", func(req domain.AgentRequest) (string, error) {
			resp, err := inner.Chat(context.Background(), req)
			return resp.Message.Content, err
		})
		return mm
	}())
	a := readyAudience(t, f, 4)
	st, _ := f.plan(t, a.ID, audience.PlanStudyInput{})
	_, err := f.svc.StartStudy(context.Background(), st.ID)
	require.NoError(t, err)
	done := f.waitStudy(t, st.ID, domain.StudyDone)
	r := done.Report
	require.NotNil(t, r)
	assert.True(t, r.SynthesisFailed)
	assert.Equal(t, 5.75, r.Metrics.IntentMean)
	assert.Contains(t, r.Summary, "could not be produced")
	require.NotEmpty(t, r.Objections)
	assert.Equal(t, "Too expensive", r.Objections[0].Label)
	assert.Equal(t, 4, r.Objections[0].Count)
	assert.NotEmpty(t, r.Quotes, "the people's own quotes stand in")
	assert.NotEmpty(t, r.Disclaimer)
}

func TestCancellingAStudyKeepsWhatWasAnsweredAndWritesNoReport(t *testing.T) {
	m := audiencetest.ScriptedModel(func(n string) string { return answers["Person 2"] })
	m.Delay = 300 * time.Millisecond
	f := newFixture(t, m)
	a := readyAudience(t, f, 8)
	st, _ := f.plan(t, a.ID, audience.PlanStudyInput{})
	_, err := f.svc.StartStudy(context.Background(), st.ID)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, f.svc.CancelStudy(context.Background(), st.ID))
	got := f.waitStudy(t, st.ID, domain.StudyCancelled)
	assert.Nil(t, got.Report)
	time.Sleep(400 * time.Millisecond)
	again, _ := f.store.GetStudy(context.Background(), st.ID)
	assert.Equal(t, domain.StudyCancelled, again.Status, "late answers do not undo the cancel")
	assert.Empty(t, m.CallsWith("You are a market-research analyst"))
}

func TestAnInterruptedProcessLeavesNothingStuckRunning(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(nil))
	ctx := context.Background()
	a := f.createAudience(t, 2)
	a.Status = domain.AudienceBuilding
	require.NoError(t, f.store.SaveAudience(ctx, a))
	f.svc.RecoverInterrupted(ctx)
	got, _ := f.store.GetAudience(ctx, a.ID)
	assert.Equal(t, domain.AudienceFailed, got.Status)
	assert.Contains(t, got.Error, "stopped")
}

// ---- conversations

func finishedStudy(t *testing.T, f *fixture) (domain.Audience, domain.Study) {
	t.Helper()
	a := readyAudience(t, f, 4)
	st, _ := f.plan(t, a.ID, audience.PlanStudyInput{Title: "Would they pay?", PricePoints: []float64{9}})
	_, err := f.svc.StartStudy(context.Background(), st.ID)
	require.NoError(t, err)
	return a, f.waitStudy(t, st.ID, domain.StudyDone)
}

func TestAPersonaChatRemembersTheThreadAndWhatTheyToldTheStudy(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string { return answers[n] }))
	ctx := context.Background()
	a, _ := finishedStudy(t, f)
	personas, _ := f.store.ListPersonas(ctx, a.ID)
	p := personas[0]

	reply, err := f.svc.Chat(ctx, p.ID, "Why did you say no?")
	require.NoError(t, err)
	assert.Equal(t, "persona", reply.Role)
	assert.Equal(t, "As I said, it depends on the price.", reply.Content)
	_, err = f.svc.Chat(ctx, p.ID, "And at nine dollars?")
	require.NoError(t, err)

	thread, err := f.svc.Thread(ctx, p.ID)
	require.NoError(t, err)
	require.Len(t, thread, 4)
	assert.Equal(t, []string{"user", "persona", "user", "persona"}, []string{thread[0].Role, thread[1].Role, thread[2].Role, thread[3].Role})

	calls := f.model.CallsWith("You are playing one person")
	require.Len(t, calls, 2)
	last := calls[1].Messages
	system := last[0].Content
	assert.Contains(t, system, p.Name)
	assert.Contains(t, system, "You rated buying it 2 out of 10", "the study's answers are remembered")
	assert.Contains(t, system, "The most you would pay: 5 USD")
	assert.Contains(t, system, "A kanban board where AI agents do the work.")
	require.Len(t, last, 4, "system, first question, first answer, new question")
	assert.Equal(t, domain.RoleAssistant, last[2].Role)
	assert.Equal(t, "And at nine dollars?", last[3].Content)

	require.NoError(t, f.svc.ClearThread(ctx, p.ID))
	thread, _ = f.svc.Thread(ctx, p.ID)
	assert.Empty(t, thread)

	_, err = f.svc.Chat(ctx, p.ID, "  ")
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
	_, err = f.svc.Chat(ctx, uuid.New(), "hi")
	assert.ErrorIs(t, err, domain.ErrPersonaNotFound)
}

func TestFollowUpsGoToOnePersonaOrEveryoneAndAreListedUnderTheStudy(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string { return answers[n] }))
	ctx := context.Background()
	a, st := finishedStudy(t, f)
	personas, _ := f.store.ListPersonas(ctx, a.ID)

	msg, err := f.svc.FollowUp(ctx, st.ID, personas[1].ID, "What would a free tier change?")
	require.NoError(t, err)
	assert.Equal(t, st.ID.String(), msg.StudyID.String())

	_, err = f.svc.FollowUp(ctx, st.ID, uuid.New(), "hi")
	assert.ErrorIs(t, err, domain.ErrPersonaNotFound)

	n, err := f.svc.FollowUpCalls(ctx, st.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	group, calls, err := f.svc.AskEveryone(ctx, st.ID, "What is the one thing missing?")
	require.NoError(t, err)
	assert.Equal(t, 4, calls)
	assert.NotEqual(t, uuid.Nil, group)

	require.Eventually(t, func() bool {
		d, err := f.svc.Study(ctx, st.ID)
		if err != nil || len(d.FollowUps) != 2 {
			return false
		}
		for _, g := range d.FollowUps {
			if g.Question == "What is the one thing missing?" {
				return len(g.Replies) == 4
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond)

	d, _ := f.svc.Study(ctx, st.ID)
	for _, g := range d.FollowUps {
		for _, r := range g.Replies {
			assert.NotEmpty(t, r.Persona)
		}
	}

	_, _, err = f.svc.AskEveryone(ctx, st.ID, " ")
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
}

func TestEveryoneCannotBeAskedBeforeTheStudyIsDone(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(nil))
	a := readyAudience(t, f, 2)
	st, _ := f.plan(t, a.ID, audience.PlanStudyInput{})
	_, _, err := f.svc.AskEveryone(context.Background(), st.ID, "why?")
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
	_, err = f.svc.AskResearch(context.Background(), st.ID, "who objected?")
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)
}

func TestAskResearchAnswersFromTheReportAndTheAnswers(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string { return answers[n] }))
	_, st := finishedStudy(t, f)
	text, err := f.svc.AskResearch(context.Background(), st.ID, "Who objected most?")
	require.NoError(t, err)
	assert.Contains(t, text, "Person 1")
	call := f.model.CallsWith("You answer the product owner's questions")
	require.Len(t, call, 1)
	user := call[0].Messages[1].Content
	assert.Contains(t, user, "Who objected most?")
	assert.Contains(t, user, "purchase-intent index 58 of 100")
	assert.Contains(t, user, "Person 1")
}

func TestDeletingAnAudienceTakesItsStudiesAndAWorkingOneIsRefused(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string { return answers[n] }))
	ctx := context.Background()
	a, st := finishedStudy(t, f)
	require.NoError(t, f.svc.DeleteAudience(ctx, a.ID))
	_, err := f.svc.Study(ctx, st.ID)
	assert.True(t, errors.Is(err, domain.ErrStudyNotFound))
	_, err = f.svc.Audience(ctx, a.ID)
	assert.True(t, errors.Is(err, domain.ErrAudienceNotFound))
}

func TestARegeneratedAudienceKeepsWhatItsStudiesFound(t *testing.T) {
	f := newFixture(t, audiencetest.ScriptedModel(func(n string) string { return answers[n] }))
	ctx := context.Background()
	a, st := finishedStudy(t, f)
	_, err := f.svc.Build(ctx, a.ID)
	require.NoError(t, err)
	f.waitAudience(t, a.ID, domain.AudienceReady)
	d, err := f.svc.Study(ctx, st.ID)
	require.NoError(t, err)
	assert.Len(t, d.Answers, 4, "the study keeps its own copy of who answered")
	assert.Equal(t, "Person 1", d.Answers[0].Persona.Name)
	_, _, err = f.svc.AskEveryone(ctx, st.ID, "still there?")
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "the old people are gone; the new ones never took part")
}
