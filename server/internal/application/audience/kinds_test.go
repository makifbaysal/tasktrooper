package audience_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/audience/audiencetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakePages map[string]string

func (p fakePages) Read(_ context.Context, url string) (string, error) {
	if text, ok := p[url]; ok {
		return text, nil
	}
	return "", errors.New("could not fetch that URL")
}

const (
	pageGood = "Invoices that chase themselves. Send, track and get paid in one place. Trusted by 4,000 freelancers. Start free."
	pageBad  = "Welcome. We are a solutions company leveraging synergies to empower stakeholders across verticals worldwide."
)

func landingJSON(act, clarity int) string {
	return fmt.Sprintf(`{"first_impression":"Looks like invoicing.","clarity_score":%d,"intent_score":%d,"verdict":"maybe","would_act_now":false,
	"reasoning":"unclear what it costs","objections":["No pricing"],"missing":["Pricing"],"trust_signals":["4,000 freelancers"],"change":"Show the price","quote":"I can't tell what it costs.",
	"answers":[{"question":"How do you handle this today?","answer":"Spreadsheets."}]}`, clarity, act)
}

var (
	lineRe = regexp.MustCompile(`(?m)^Line (\d+): (.+)$`)
	pageRe = regexp.MustCompile(`(?s)Version 1:\n---\n(.*?)\n---\n\nVersion 2:\n---\n(.*?)\n---`)
)

// kindModel answers a message interview by the text of each line (so the
// order it was shown in does not matter) and an A/B interview by which page
// reads "Invoices".
func kindModel(t *testing.T) (*audiencetest.Model, *sync.Mutex, *[]string) {
	m := audiencetest.ScriptedModel(func(string) string { return landingJSON(7, 8) })
	var mu sync.Mutex
	var shownFirst []string
	m.OnFirst("You are a character in a market-research interview", func(req domain.AgentRequest) (string, error) {
		user := req.Messages[len(req.Messages)-1].Content
		switch {
		case strings.Contains(user, "candidate lines"):
			var ratings []string
			best, bestAppeal := 1, -1
			for _, sm := range lineRe.FindAllStringSubmatch(user, -1) {
				appeal := 3
				if strings.Contains(sm[2], "Invoices that chase themselves") {
					appeal = 9
				}
				if appeal > bestAppeal {
					bestAppeal, best = appeal, mustAtoi(sm[1])
				}
				ratings = append(ratings, fmt.Sprintf(`{"line":%s,"clarity":%d,"relevance":%d,"credibility":%d,"appeal":%d}`, sm[1], appeal, appeal, appeal, appeal))
			}
			return fmt.Sprintf(`{"ratings":[%s],"best":%d,"best_reason":"says what it does","stronger_line":"Get paid faster","quote":"clear wins"}`, strings.Join(ratings, ","), best), nil
		case strings.Contains(user, "two versions of a web page"):
			sm := pageRe.FindStringSubmatch(user)
			card := func(text string) string {
				if strings.Contains(text, "Invoices") {
					return `{"first_impression":"Clear.","clarity_score":9,"intent_score":8,"verdict":"yes","objections":["No pricing"],"missing":["price"],"trust_signals":["4,000 freelancers"],"change":"Show the price"}`
				}
				return `{"first_impression":"Buzzwords.","clarity_score":4,"intent_score":3,"verdict":"no","objections":["vague"],"missing":["price"],"trust_signals":[],"change":"Say what it does"}`
			}
			preferred := 2
			if strings.Contains(sm[1], "Invoices") {
				preferred = 1
			}
			mu.Lock()
			if strings.Contains(sm[1], "Invoices") {
				shownFirst = append(shownFirst, "good")
			} else {
				shownFirst = append(shownFirst, "bad")
			}
			mu.Unlock()
			return fmt.Sprintf(`{"page_1":%s,"page_2":%s,"preferred":%d,"preference_reason":"clearer offer","quote":"the clear one"}`, card(sm[1]), card(sm[2]), preferred), nil
		}
		return landingJSON(7, 8), nil
	})
	m.Say("You are a conversion researcher", `{"summary":"The page works but hides its price.","verdict_rationale":"Clear offer, no price.",
	  "segment_insights":[],"objection_themes":[{"label":"No pricing","personas":["p1","p2"]}],"need_themes":[],"strength_themes":[{"label":"Trust numbers","personas":["p1"]}],
	  "a_reason_themes":[{"label":"Clear offer","personas":["p1","p2","p3"]}],"b_reason_themes":[],
	  "quotes":[{"persona":"p1","text":"I can't tell what it costs.","topic":"price","sentiment":"negative"},{"persona":"p2","version":"A","text":"the clear one","topic":"x","sentiment":"neutral"}],
	  "recommendations":[{"title":"Show the price","detail":"Put it above the fold.","priority":"high"}]}`)
	m.Say("You are a messaging researcher", `{"summary":"The plain line wins.","verdict_rationale":"Clarity.","segment_insights":[],"objection_themes":[],"need_themes":[],
	  "strength_themes":[{"label":"Says what it does","personas":["p1","p2"]}],"quotes":[],"recommendations":[{"title":"Get paid faster","detail":"Shorter.","priority":"high"}]}`)
	return m, &mu, &shownFirst
}

func mustAtoi(s string) int {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

func readyFixture(t *testing.T, m *audiencetest.Model, pages audience.PageReader, people int) (*fixture, domain.Audience) {
	f := newFixtureWith(t, m, pages)
	return f, readyAudience(t, f, people)
}

func TestEachKindValidatesItsInputsAndAnUnknownKindIsRefused(t *testing.T) {
	m, _, _ := kindModel(t)
	reads := 0
	pages := countingPages{fakePages{"https://acme.test/": pageGood, "https://thin.test/": "tiny"}, &reads}
	f, a := readyFixture(t, m, pages, 4)
	ctx := context.Background()
	plan := func(in audience.PlanStudyInput) (domain.Study, error) { return f.svc.PlanStudy(ctx, a.ID, in) }

	_, err := plan(audience.PlanStudyInput{Kind: "survey"})
	require.ErrorIs(t, err, domain.ErrAudienceInvalid)

	_, err = plan(audience.PlanStudyInput{Kind: "landing_page_test"})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "a landing page test needs its page")
	_, err = plan(audience.PlanStudyInput{Kind: "landing_page_test", Inputs: domain.StudyInputs{Pages: []domain.PageInput{{URL: "ftp://x"}}}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "only http and https")
	_, err = plan(audience.PlanStudyInput{Kind: "landing_page_test", Inputs: domain.StudyInputs{Pages: []domain.PageInput{{URL: "https://nope.test/"}}}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "an unreadable URL asks for the text")
	_, err = plan(audience.PlanStudyInput{Kind: "landing_page_test", Inputs: domain.StudyInputs{Pages: []domain.PageInput{{URL: "https://thin.test/"}}}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "a page with no readable text asks for the text")

	_, err = plan(audience.PlanStudyInput{Kind: "ab_test", Inputs: domain.StudyInputs{Pages: []domain.PageInput{{Copy: pageGood}}}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "an A/B test needs exactly two pages")
	_, err = plan(audience.PlanStudyInput{Kind: "message_testing", Inputs: domain.StudyInputs{Variants: []string{"only one", " Only One "}}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "two different lines at least")
	_, err = plan(audience.PlanStudyInput{Kind: "message_testing", Inputs: domain.StudyInputs{Variants: []string{"1", "2", "3", "4", "5", "6"}}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "five lines at most")

	reads = 0
	st, err := plan(audience.PlanStudyInput{Kind: "landing_page_test", Inputs: domain.StudyInputs{Intent: "compare options", Pages: []domain.PageInput{{URL: "https://acme.test/"}}}})
	require.NoError(t, err)
	assert.Equal(t, domain.KindLandingPageTest, st.Kind)
	assert.Equal(t, pageGood, st.Inputs.Pages[0].Copy, "the URL's text is read once and kept")
	assert.Equal(t, "compare options", st.Inputs.Intent)
	assert.Equal(t, 1, reads)

	pasted, err := plan(audience.PlanStudyInput{Kind: "ab_test", Inputs: domain.StudyInputs{Pages: []domain.PageInput{{URL: "https://acme.test/", Copy: "pasted A"}, {Copy: "pasted B"}}}})
	require.NoError(t, err)
	assert.Equal(t, "pasted A", pasted.Inputs.Pages[0].Copy, "pasted text wins over reading the URL")
	assert.Equal(t, 1, reads, "no second fetch")
	assert.Equal(t, "evaluate what it offers", pasted.Inputs.Intent, "intent defaults")

	plain, err := plan(audience.PlanStudyInput{})
	require.NoError(t, err)
	assert.Equal(t, domain.KindUserInterviews, plain.Kind, "no kind is the original study")
	assert.Empty(t, plain.Inputs.Pages)

	noReader := newFixtureWith(t, m, nil)
	a2 := readyAudience(t, noReader, 2)
	_, err = noReader.svc.PlanStudy(ctx, a2.ID, audience.PlanStudyInput{Kind: "landing_page_test", Inputs: domain.StudyInputs{Pages: []domain.PageInput{{URL: "https://acme.test/"}}}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "without a reader a URL cannot be read")
}

type countingPages struct {
	fakePages
	n *int
}

func (c countingPages) Read(ctx context.Context, url string) (string, error) {
	*c.n++
	return c.fakePages.Read(ctx, url)
}

func TestEveryKindCostsOneCallPerPersonThePlanAndTheScript(t *testing.T) {
	for _, k := range domain.StudyKinds {
		assert.Equal(t, 5, audience.StudyCallsFor(k, 4), string(k))
	}
	m, _, _ := kindModel(t)
	f, a := readyFixture(t, m, fakePages{}, 4)
	st, err := f.svc.PlanStudy(context.Background(), a.ID, audience.PlanStudyInput{Kind: "message_testing", Inputs: domain.StudyInputs{Variants: []string{"a b c", "d e f"}}})
	require.NoError(t, err)
	assert.Equal(t, 6, st.CallsEstimate, "four people, the report and the script")
	assert.Equal(t, 1, st.ScriptCalls)

	own, err := f.svc.PlanStudy(context.Background(), a.ID, audience.PlanStudyInput{Questions: []string{"Why?"}})
	require.NoError(t, err)
	assert.Equal(t, 5, own.CallsEstimate, "questions of its own need no script call")
	assert.Equal(t, domain.ScriptAwaitingApproval, own.ScriptState)
}

func TestTheScriptIsWrittenOnceByTheModelEditedAndRewrittenOnRequest(t *testing.T) {
	m, _, _ := kindModel(t)
	f, a := readyFixture(t, m, fakePages{}, 4)
	ctx := context.Background()
	planned, err := f.svc.PlanStudy(ctx, a.ID, audience.PlanStudyInput{Goal: "Would designers pay?"})
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptGenerating, planned.ScriptState)
	ready := f.waitScript(t, planned.ID, domain.ScriptAwaitingApproval)
	require.NotNil(t, ready.Script)
	require.Len(t, ready.Script.Sections, 2)
	assert.Equal(t, "Today", ready.Script.Sections[0].Title)
	assert.Equal(t, []string{"How do you handle this today?", "What went wrong last time?", "What would make you switch?"}, ready.Questions)
	calls := m.CallsWith("You design the interview script")
	require.Len(t, calls, 1, "the script is written once")
	assert.Contains(t, calls[0].Messages[1].Content, "Would designers pay?")
	assert.Contains(t, calls[0].Messages[1].Content, "A kanban board where AI agents do the work.")

	edited, err := f.svc.UpdateStudy(ctx, planned.ID, audience.PlanStudyInput{Script: &domain.StudyScript{Sections: []domain.ScriptSection{
		{Title: " Mine ", Questions: []string{" What is your budget? ", ""}}, {Title: "Empty", Questions: []string{" "}},
	}}})
	require.NoError(t, err)
	assert.Equal(t, []domain.ScriptSection{{Title: "Mine", Questions: []string{"What is your budget?"}}}, edited.Script.Sections, "empty questions and sections are dropped")
	assert.Equal(t, []string{"What is your budget?"}, edited.Questions)
	assert.Equal(t, "Would designers pay?", edited.Goal, "a script-only edit keeps the goal")
	again, err := f.store.GetStudy(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, edited.Script, again.Script, "the edit persists")
	assert.Len(t, m.CallsWith("You design the interview script"), 1, "editing costs nothing")

	tooMany := make([]string, 13)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("q%d", i)
	}
	_, err = f.svc.UpdateStudy(ctx, planned.ID, audience.PlanStudyInput{Script: &domain.StudyScript{Sections: []domain.ScriptSection{{Questions: tooMany}}}})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid)

	regenerated, err := f.svc.GenerateScript(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, regenerated.ScriptCalls)
	assert.Equal(t, 7, regenerated.CallsEstimate, "four people, the report and two scripts")
	f.waitScript(t, planned.ID, domain.ScriptAwaitingApproval)
	assert.Len(t, m.CallsWith("You design the interview script"), 2)
}

func TestAStudyDoesNotStartWhileItsScriptIsBeingWrittenOrFailed(t *testing.T) {
	m, _, _ := kindModel(t)
	m.Delay = 150 * time.Millisecond
	f, a := readyFixture(t, m, fakePages{}, 2)
	ctx := context.Background()
	st, err := f.svc.PlanStudy(ctx, a.ID, audience.PlanStudyInput{})
	require.NoError(t, err)
	_, err = f.svc.StartStudy(ctx, st.ID)
	require.ErrorIs(t, err, domain.ErrAudienceInvalid)
	assert.Contains(t, err.Error(), "script")
	_, err = f.svc.UpdateStudy(ctx, st.ID, audience.PlanStudyInput{Goal: "x"})
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "no edits while it is written")
	f.waitScript(t, st.ID, domain.ScriptAwaitingApproval)

	broken := audiencetest.ScriptedModel(nil)
	broken.OnFirst("You design the interview script", func(domain.AgentRequest) (string, error) { return "I cannot do that", nil })
	f2, a2 := readyFixture(t, broken, fakePages{}, 2)
	st2, err := f2.svc.PlanStudy(ctx, a2.ID, audience.PlanStudyInput{})
	require.NoError(t, err)
	failed := f2.waitScript(t, st2.ID, domain.ScriptFailed)
	assert.NotEmpty(t, failed.Error)
	_, err = f2.svc.StartStudy(ctx, st2.ID)
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "a failed script is rewritten or replaced first")
	own, err := f2.svc.UpdateStudy(ctx, st2.ID, audience.PlanStudyInput{Questions: []string{"What do you use?"}})
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptAwaitingApproval, own.ScriptState, "bringing questions repairs it")
}

func TestALandingPageTestReadsThePageAsAVisitorWithTheScriptAndWaitsForTheReport(t *testing.T) {
	m, _, _ := kindModel(t)
	f, a := readyFixture(t, m, fakePages{}, 4)
	ctx := context.Background()
	planned, err := f.svc.PlanStudy(ctx, a.ID, audience.PlanStudyInput{Kind: "landing_page_test",
		Inputs: domain.StudyInputs{Intent: "buy", Pages: []domain.PageInput{{Copy: pageGood}}}})
	require.NoError(t, err)
	f.approve(t, planned.ID)

	_, err = f.svc.GenerateReport(ctx, planned.ID)
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "no report before the interviews")

	_, err = f.svc.StartStudyWith(ctx, planned.ID, audience.StartOptions{ManualReport: true})
	require.NoError(t, err)
	held := f.waitStudy(t, planned.ID, domain.StudyInterviewed)
	assert.Nil(t, held.Report, "the report waits for the click")
	assert.Equal(t, 4, held.PersonaDone)
	assert.Empty(t, m.CallsWith("You are a conversion researcher"), "no report call yet")

	interview := m.CallsWith("You are a character in a market-research interview")
	require.Len(t, interview, 4)
	asked := interview[0].Messages[1].Content
	assert.Contains(t, asked, pageGood)
	assert.Contains(t, asked, "came to this page with this intent: buy")
	assert.Contains(t, asked, "How do you handle this today?", "the interviews use the stored script")
	assert.Contains(t, asked, "What would make you switch?")

	generating, err := f.svc.GenerateReport(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StudySynthesizing, generating.Status)
	done := f.waitStudy(t, planned.ID, domain.StudyDone)
	r := done.Report
	require.NotNil(t, r)
	assert.Equal(t, domain.KindLandingPageTest, r.Kind)
	require.NotNil(t, r.Landing)
	assert.Equal(t, 8.0, r.Landing.ClarityMean)
	assert.Equal(t, 70.0, r.Verdict.Score)
	assert.Equal(t, "buy", r.Landing.Intent)
	require.Len(t, r.Strengths, 1)
	assert.Equal(t, 1, r.Strengths[0].Count)
	assert.Equal(t, "Show the price", r.Recommendations[0].Title)
	assert.Contains(t, r.Methodology, "first-time visitor")
	assert.Equal(t, 0, r.Metrics.Price.Answered, "no prices in a page test")
	detail, err := f.svc.Study(ctx, planned.ID)
	require.NoError(t, err)
	require.NotNil(t, detail.Answers[0].Answers.Landing)
	assert.Equal(t, 8, detail.Answers[0].Answers.Landing.ClarityScore)
	assert.Empty(t, detail.Pending)
}

func TestMessageTestingRotatesTheLinesAndRanksThemFromTheirOwnPlace(t *testing.T) {
	m, _, _ := kindModel(t)
	f, a := readyFixture(t, m, fakePages{}, 4)
	ctx := context.Background()
	variants := []string{"Boards for agents", "Invoices that chase themselves", "Ship faster"}
	planned, err := f.plan(t, a.ID, audience.PlanStudyInput{Kind: "message_testing", Inputs: domain.StudyInputs{Variants: variants}})
	require.NoError(t, err)
	_, err = f.svc.StartStudy(ctx, planned.ID)
	require.NoError(t, err)
	done := f.waitStudy(t, planned.ID, domain.StudyDone)

	firstLines := map[string]bool{}
	for _, c := range m.CallsWith("You are a character in a market-research interview") {
		sm := lineRe.FindStringSubmatch(c.Messages[1].Content)
		firstLines[sm[2]] = true
	}
	assert.Len(t, firstLines, 3, "every line is shown first to someone")

	r := done.Report
	require.NotNil(t, r.Messages)
	assert.Equal(t, 2, r.Messages.Winner, "the second line in the study's own order, wherever it was shown")
	require.Len(t, r.Messages.Variants, 3)
	assert.Equal(t, 1, r.Messages.Variants[1].Rank)
	assert.Equal(t, 9.0, r.Messages.Variants[1].Overall)
	assert.Equal(t, 100.0, r.Messages.Variants[1].BestPct)
	assert.Equal(t, 3.0, r.Messages.Variants[0].Appeal)
	assert.Equal(t, "Invoices that chase themselves", r.Messages.Variants[1].Text)
	assert.Equal(t, 90.0, r.Verdict.Score, "the verdict is how well the winning line lands")
	assert.NotEmpty(t, r.Messages.Rewrites)
	assert.Equal(t, "Get paid faster", r.Messages.Rewrites[0].Text)
	assert.Equal(t, "Get paid faster", r.Recommendations[0].Title)
	assert.Contains(t, r.Methodology, "rotated")
}

func TestAnABTestScoresEachVersionOnItsOwnForEveryoneThenComparesThem(t *testing.T) {
	m, mu, shownFirst := kindModel(t)
	f, a := readyFixture(t, m, fakePages{}, 4)
	ctx := context.Background()
	planned, err := f.plan(t, a.ID, audience.PlanStudyInput{Kind: "ab_test", Inputs: domain.StudyInputs{Intent: "buy", Pages: []domain.PageInput{{Copy: pageGood}, {Copy: pageBad}}}})
	require.NoError(t, err)
	assert.Equal(t, 6, planned.CallsEstimate, "four people each see both, the report and the script")
	_, err = f.svc.StartStudy(ctx, planned.ID)
	require.NoError(t, err)
	done := f.waitStudy(t, planned.ID, domain.StudyDone)

	mu.Lock()
	order := append([]string(nil), *shownFirst...)
	mu.Unlock()
	good := 0
	for _, o := range order {
		if o == "good" {
			good++
		}
	}
	assert.Equal(t, 2, good, "half the people see version A first")
	assert.Len(t, order, 4)
	assert.Len(t, m.CallsWith("You are a character in a market-research interview"), 4, "one interview per person covers both versions")

	r := done.Report
	require.NotNil(t, r.AB)
	assert.Equal(t, "A", r.AB.Winner)
	assert.Equal(t, 4, r.AB.Sides[0].Wins)
	assert.Equal(t, 100.0, r.AB.Sides[0].WinPct)
	assert.Equal(t, 0.0, r.AB.Sides[1].WinPct)
	assert.Equal(t, "lean", r.AB.Confidence, "four people are never 'clear'")
	assert.Equal(t, 50.0, r.AB.FirstShownWinPct, "position did not decide it")

	assert.Equal(t, 8.0, r.AB.Act.A.Mean)
	assert.Equal(t, 3.0, r.AB.Act.B.Mean, "scores follow the version, not the position")
	assert.Equal(t, 8.0, r.AB.Act.A.Median)
	assert.Equal(t, -5.0, r.AB.Act.Diff, "B minus A")
	assert.Equal(t, 4, r.AB.Act.AHigher)
	assert.Equal(t, 0, r.AB.Act.BHigher)
	assert.Equal(t, 9.0, r.AB.Clarity.A.Mean)
	assert.Equal(t, 4.0, r.AB.Clarity.B.Mean)
	assert.Equal(t, "large", r.AB.Act.EffectLabel)
	assert.Equal(t, -3.0, r.AB.Act.EffectSize, "capped when nobody varies")
	assert.Equal(t, 100.0, r.AB.Sides[0].ActPct)
	assert.Equal(t, 0.0, r.AB.Sides[1].ActPct)

	require.NotEmpty(t, r.AB.Sides[1].Issues)
	assert.Equal(t, "vague", r.AB.Sides[1].Issues[0].Label)
	assert.Equal(t, "No pricing", r.AB.Sides[0].Issues[0].Label)
	require.NotEmpty(t, r.AB.Sides[0].Reasons)
	assert.Equal(t, 3, r.AB.Sides[0].Reasons[0].Count)
	require.Len(t, r.AB.Sides[0].Quotes, 1, "quotes are filed under the version they are about")
	assert.Equal(t, "A", r.AB.Sides[0].Quotes[0].Version)
	assert.Contains(t, r.Methodology, "on its own")
	assert.Contains(t, r.Methodology, "no p-values")

	detail, err := f.svc.Study(ctx, planned.ID)
	require.NoError(t, err)
	require.NotNil(t, detail.Answers[0].Answers.PageA)
	require.NotNil(t, detail.Answers[0].Answers.PageB)
	assert.Equal(t, "a", detail.Answers[0].Answers.Preferred)
}

func TestFailedInterviewsCanBeRetriedBeforeTheReportOrTheReportUsesTheRest(t *testing.T) {
	m, _, _ := kindModel(t)
	var mu sync.Mutex
	failFor := map[string]bool{"Person 2": true}
	m.OnFirst("You are a character in a market-research interview", func(req domain.AgentRequest) (string, error) {
		name := regexp.MustCompile(`Who you are: ([^,.\n]+)`).FindStringSubmatch(req.Messages[0].Content)[1]
		mu.Lock()
		defer mu.Unlock()
		if failFor[name] {
			return "no JSON here", nil
		}
		return landingJSON(7, 8), nil
	})
	f, a := readyFixture(t, m, fakePages{}, 3)
	ctx := context.Background()
	planned, err := f.plan(t, a.ID, audience.PlanStudyInput{Kind: "landing_page_test", Inputs: domain.StudyInputs{Pages: []domain.PageInput{{Copy: pageGood}}}})
	require.NoError(t, err)
	_, err = f.svc.StartStudyWith(ctx, planned.ID, audience.StartOptions{ManualReport: true})
	require.NoError(t, err)
	held := f.waitStudy(t, planned.ID, domain.StudyInterviewed)
	assert.Equal(t, 2, held.PersonaDone)
	assert.Equal(t, 1, held.PersonaFailed)

	mu.Lock()
	failFor["Person 2"] = false
	mu.Unlock()
	retried, err := f.svc.RetryFailed(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StudyRunning, retried.Status)
	assert.Equal(t, 0, retried.PersonaFailed)
	assert.Equal(t, held.CallsEstimate+1, retried.CallsEstimate, "a retry is one more call")
	again := f.waitStudy(t, planned.ID, domain.StudyInterviewed)
	assert.Equal(t, 3, again.PersonaDone)
	assert.Equal(t, 0, again.PersonaFailed)
	_, err = f.svc.RetryFailed(ctx, planned.ID)
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "nothing failed any more")

	mu.Lock()
	failFor["Person 1"] = true
	mu.Unlock()
	second, err := f.plan(t, a.ID, audience.PlanStudyInput{Kind: "landing_page_test", Inputs: domain.StudyInputs{Pages: []domain.PageInput{{Copy: pageGood}}}})
	require.NoError(t, err)
	_, err = f.svc.StartStudyWith(ctx, second.ID, audience.StartOptions{ManualReport: true})
	require.NoError(t, err)
	partial := f.waitStudy(t, second.ID, domain.StudyInterviewed)
	assert.Equal(t, 1, partial.PersonaFailed)
	_, err = f.svc.GenerateReport(ctx, second.ID)
	require.NoError(t, err, "the report can use the completed interviews")
	final := f.waitStudy(t, second.ID, domain.StudyDone)
	assert.Equal(t, 2, final.Report.Metrics.Respondents)
	assert.Equal(t, 1, final.Report.Metrics.Failed)
}

func TestRunningStudyListsThePeopleNotHeardFromYet(t *testing.T) {
	m, _, _ := kindModel(t)
	m.Delay = 300 * time.Millisecond
	f, a := readyFixture(t, m, fakePages{}, 3)
	ctx := context.Background()
	planned, err := f.plan(t, a.ID, audience.PlanStudyInput{Questions: []string{"Why?"}})
	require.NoError(t, err)
	_, err = f.svc.StartStudyWith(ctx, planned.ID, audience.StartOptions{ManualReport: true})
	require.NoError(t, err)
	d, err := f.svc.Study(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StudyRunning, d.Status)
	assert.Len(t, d.Pending, 3)
	f.waitStudy(t, planned.ID, domain.StudyInterviewed)
	d, err = f.svc.Study(ctx, planned.ID)
	require.NoError(t, err)
	assert.Empty(t, d.Pending)
	_ = uuid.Nil
}

func TestNoInterviewRunsUntilThePersonApprovesTheCurrentVersionOfTheScript(t *testing.T) {
	m, _, _ := kindModel(t)
	f, a := readyFixture(t, m, fakePages{}, 3)
	ctx := context.Background()
	planned, err := f.svc.PlanStudy(ctx, a.ID, audience.PlanStudyInput{Goal: "Would designers pay?"})
	require.NoError(t, err)
	written := f.waitScript(t, planned.ID, domain.ScriptAwaitingApproval)
	assert.Equal(t, 1, written.ScriptVersion)
	assert.Equal(t, 0, written.ScriptApprovedVersion)
	assert.Nil(t, written.ScriptApprovedAt)

	time.Sleep(150 * time.Millisecond)
	still, err := f.store.GetStudy(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StudyDraft, still.Status, "a written script starts nothing on its own")
	assert.Empty(t, m.CallsWith("You are a character in a market-research interview"))

	_, err = f.svc.StartStudy(ctx, planned.ID)
	assert.ErrorIs(t, err, audience.ErrScriptNotApproved)
	_, err = f.svc.StartStudyWith(ctx, planned.ID, audience.StartOptions{ManualReport: true})
	assert.ErrorIs(t, err, audience.ErrScriptNotApproved)
	assert.Empty(t, m.CallsWith("You are a character in a market-research interview"))

	_, err = f.svc.ApproveScript(ctx, planned.ID, 7, "akif")
	assert.ErrorIs(t, err, audience.ErrScriptChanged, "an approval of a version that was not shown is refused")
	approved, err := f.svc.ApproveScript(ctx, planned.ID, written.ScriptVersion, "akif")
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptApproved, approved.ScriptState)
	assert.Equal(t, written.ScriptVersion, approved.ScriptApprovedVersion)
	assert.Equal(t, "akif", approved.ScriptApprovedBy)
	require.NotNil(t, approved.ScriptApprovedAt)

	renamed, err := f.svc.UpdateStudy(ctx, planned.ID, audience.PlanStudyInput{Title: "Renamed"})
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptApproved, renamed.ScriptState, "changing the title does not void the approval")

	edited, err := f.svc.UpdateStudy(ctx, planned.ID, audience.PlanStudyInput{Script: &domain.StudyScript{Sections: []domain.ScriptSection{{Questions: []string{"What is your budget?"}}}}})
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptAwaitingApproval, edited.ScriptState, "an edit needs a new approval")
	assert.Equal(t, written.ScriptVersion+1, edited.ScriptVersion)
	assert.Equal(t, 0, edited.ScriptApprovedVersion)
	assert.Empty(t, edited.ScriptApprovedBy)
	_, err = f.svc.StartStudy(ctx, planned.ID)
	assert.ErrorIs(t, err, audience.ErrScriptNotApproved)
	_, err = f.svc.ApproveScript(ctx, planned.ID, written.ScriptVersion, "akif")
	assert.ErrorIs(t, err, audience.ErrScriptChanged, "the old version cannot be approved any more")

	_, err = f.svc.ApproveScript(ctx, planned.ID, edited.ScriptVersion, "akif")
	require.NoError(t, err)
	regenerating, err := f.svc.GenerateScript(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptGenerating, regenerating.ScriptState)
	assert.Equal(t, 0, regenerating.ScriptApprovedVersion, "rewriting the script voids the approval")
	rewritten := f.waitScript(t, planned.ID, domain.ScriptAwaitingApproval)
	assert.Equal(t, edited.ScriptVersion+1, rewritten.ScriptVersion)
	_, err = f.svc.StartStudy(ctx, planned.ID)
	assert.ErrorIs(t, err, audience.ErrScriptNotApproved)

	_, err = f.svc.ApproveScript(ctx, planned.ID, rewritten.ScriptVersion, "akif")
	require.NoError(t, err)
	_, err = f.svc.UpdateStudy(ctx, planned.ID, audience.PlanStudyInput{Kind: "message_testing", Inputs: domain.StudyInputs{Variants: []string{"One line", "Another line"}}})
	require.NoError(t, err)
	changed, err := f.store.GetStudy(ctx, planned.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptAwaitingApproval, changed.ScriptState, "changing what is asked voids the approval too")

	_, err = f.svc.ApproveScript(ctx, planned.ID, changed.ScriptVersion, "akif")
	require.NoError(t, err)
	_, err = f.svc.StartStudy(ctx, planned.ID)
	require.NoError(t, err)
	f.waitStudy(t, planned.ID, domain.StudyDone)
	_, err = f.svc.ApproveScript(ctx, planned.ID, changed.ScriptVersion, "akif")
	assert.ErrorIs(t, err, domain.ErrAudienceInvalid, "a study that ran keeps its script")
}

func TestQuestionsBroughtByThePersonStillWaitForTheirApproval(t *testing.T) {
	m, _, _ := kindModel(t)
	f, a := readyFixture(t, m, fakePages{}, 2)
	ctx := context.Background()
	own, err := f.svc.PlanStudy(ctx, a.ID, audience.PlanStudyInput{Questions: []string{"What do you use?"}})
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptAwaitingApproval, own.ScriptState)
	assert.Equal(t, 1, own.ScriptVersion)
	_, err = f.svc.StartStudy(ctx, own.ID)
	assert.ErrorIs(t, err, audience.ErrScriptNotApproved)

	none := audience.NewService(audience.Deps{Store: f.store, Agents: audiencetest.Agents{}, LLM: m})
	st, err := none.PlanStudy(ctx, a.ID, audience.PlanStudyInput{})
	require.NoError(t, err)
	assert.Equal(t, domain.ScriptNone, st.ScriptState, "no runtime wrote no script")
	_, err = none.StartStudy(ctx, st.ID)
	assert.ErrorIs(t, err, audience.ErrScriptNotApproved, "a study with no script is approved too, to ask only the built-in questions")
}
