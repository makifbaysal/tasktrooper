package audience

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type numbered struct {
	N    int
	Text string
}

type answerLandingInput struct {
	Page, Intent string
	Questions    []string
}

type answerMessagesInput struct {
	Background string
	Lines      []numbered
	Questions  []string
}

type answerABInput struct {
	First, Second, Intent string
	Questions             []string
}

type scriptInterviewsInput struct {
	Goal, Product, Language string
	Segments                []string
}

type scriptLandingInput struct {
	Goal, Intent, Page, Language string
	Segments                     []string
}

type scriptMessagesInput struct {
	Goal, Product, Language string
	Variants, Segments      []string
}

type scriptABInput struct {
	Goal, Intent, PageA, PageB, Language string
	Segments                             []string
}

type methodologyKindInput struct {
	Personas, Segments, Calls, Variants int
	Intent                              string
}

var (
	answerLandingKey  = prompt.Define("audience.answer_landing_user", answerLandingInput{Page: "Acme: invoices.", Intent: "evaluate", Questions: []string{"What did you expect?"}})
	answerMessagesKey = prompt.Define("audience.answer_messages_user", answerMessagesInput{Background: "A kanban.", Lines: []numbered{{1, "Ship faster"}, {2, "Boards for agents"}}, Questions: []string{"What did you expect?"}})
	answerABKey       = prompt.Define("audience.answer_ab_user", answerABInput{First: "Page one.", Second: "Page two.", Intent: "buy", Questions: []string{"What did you expect?"}})

	scriptSystemKey     = prompt.Define[struct{}]("audience.script_system", struct{}{})
	scriptInterviewsKey = prompt.Define("audience.script_user_interviews", scriptInterviewsInput{Goal: "g", Product: "p", Segments: []string{"- Freelancers"}})
	scriptLandingKey    = prompt.Define("audience.script_landing_page_test", scriptLandingInput{Goal: "g", Intent: "buy", Page: "p", Segments: []string{"- Freelancers"}})
	scriptMessagesKey   = prompt.Define("audience.script_message_testing", scriptMessagesInput{Goal: "g", Product: "p", Variants: []string{"1. a", "2. b"}, Segments: []string{"- Freelancers"}})
	scriptABKey         = prompt.Define("audience.script_ab_test", scriptABInput{Goal: "g", Intent: "buy", PageA: "a", PageB: "b", Segments: []string{"- Freelancers"}})

	synthLandingSystemKey  = prompt.Define[struct{}]("audience.synth_landing_system", struct{}{})
	synthMessagesSystemKey = prompt.Define[struct{}]("audience.synth_messages_system", struct{}{})
	synthABSystemKey       = prompt.Define[struct{}]("audience.synth_ab_system", struct{}{})

	methodologyLandingKey  = prompt.Define("audience.methodology_landing", methodologyKindInput{Personas: 12, Segments: 3, Calls: 14, Intent: "buy"})
	methodologyMessagesKey = prompt.Define("audience.methodology_messages", methodologyKindInput{Personas: 12, Segments: 3, Calls: 14, Variants: 3})
	methodologyABKey       = prompt.Define("audience.methodology_ab", methodologyKindInput{Personas: 12, Segments: 3, Calls: 14, Intent: "buy"})
)

func segmentLinesOf(b domain.AudienceBrief) []string {
	var out []string
	for _, seg := range b.Segments {
		line := "- " + seg.Name
		if seg.Description != "" {
			line += ": " + seg.Description
		}
		out = append(out, line)
	}
	return out
}

func pageCopy(st domain.Study, i int) string {
	if i < len(st.Inputs.Pages) {
		return st.Inputs.Pages[i].Copy
	}
	return ""
}

func intentOf(st domain.Study) string {
	if st.Inputs.Intent == "" {
		return defaultIntent
	}
	return st.Inputs.Intent
}

// scriptPrompt is the system and user message that ask for the study's script.
func (s *Service) scriptPrompt(st domain.Study, a domain.Audience) (string, string) {
	segs := segmentLinesOf(a.Brief)
	goal := st.Goal
	switch st.Kind {
	case domain.KindLandingPageTest:
		return prompt.Text(scriptSystemKey), scriptLandingKey.Render(scriptLandingInput{Goal: goal, Intent: intentOf(st), Page: pageCopy(st, 0), Language: a.Brief.Language, Segments: segs})
	case domain.KindMessageTesting:
		variants := make([]string, len(st.Inputs.Variants))
		for i, v := range st.Inputs.Variants {
			variants[i] = fmt.Sprintf("%d. %s", i+1, v)
		}
		return prompt.Text(scriptSystemKey), scriptMessagesKey.Render(scriptMessagesInput{Goal: goal, Product: clip(a.Brief.Product, 600), Variants: variants, Language: a.Brief.Language, Segments: segs})
	case domain.KindABTest:
		return prompt.Text(scriptSystemKey), scriptABKey.Render(scriptABInput{Goal: goal, Intent: intentOf(st), PageA: pageCopy(st, 0), PageB: pageCopy(st, 1), Language: a.Brief.Language, Segments: segs})
	default:
		return prompt.Text(scriptSystemKey), scriptInterviewsKey.Render(scriptInterviewsInput{Goal: goal, Product: productText(a, ""), Language: a.Brief.Language, Segments: segs})
	}
}

// interview is what one person is asked and how their answer is read.
func (s *Service) interview(st domain.Study, a domain.Audience, p domain.Persona) (user string, parse func(raw string) (domain.StudyAnswers, error)) {
	switch st.Kind {
	case domain.KindLandingPageTest:
		return answerLandingKey.Render(answerLandingInput{Page: pageCopy(st, 0), Intent: intentOf(st), Questions: st.Questions}), parseLandingAnswers
	case domain.KindMessageTesting:
		perm := rotation(len(st.Inputs.Variants), p.Ordinal)
		lines := make([]numbered, len(perm))
		for i, orig := range perm {
			lines[i] = numbered{N: i + 1, Text: st.Inputs.Variants[orig]}
		}
		return answerMessagesKey.Render(answerMessagesInput{Background: clip(a.Brief.Product, 600), Lines: lines, Questions: st.Questions}),
			func(raw string) (domain.StudyAnswers, error) { return parseMessageAnswers(raw, perm) }
	case domain.KindABTest:
		swapped := abSwapped(p.Ordinal)
		first, second := pageCopy(st, 0), pageCopy(st, 1)
		if swapped {
			first, second = second, first
		}
		return answerABKey.Render(answerABInput{First: first, Second: second, Intent: intentOf(st), Questions: st.Questions}),
			func(raw string) (domain.StudyAnswers, error) { return parseABAnswers(raw, swapped) }
	default:
		return answerUserKey.Render(answerUserInput{Product: st.ProductSnapshot, Currency: st.Currency, PricePoints: priceStrings(st.PricePoints), Questions: st.Questions}),
			func(raw string) (domain.StudyAnswers, error) { return parseAnswers(raw, st.PricePoints) }
	}
}

// askPersona asks one person the study's questions and keeps the answer; a
// person who cannot answer is recorded as failed, never as a "no".
func (s *Service) askPersona(ctx, wctx context.Context, st domain.Study, a domain.Audience, p domain.Persona, rt domain.Agent) {
	var parsed domain.StudyAnswers
	user, parse := s.interview(st, a, p)
	err := s.askJSON(ctx, rt, askRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: answerSystemKey.Render(answerSystemInput{Sheet: sheetOf(p.Name, p.Segment, p.PersonaProfile), Language: a.Brief.Language})},
			{Role: domain.RoleUser, Content: user},
		},
		Label: "audience-study:" + p.Name, Title: "interview " + p.Name, MaxTurns: 4,
	}, func(raw string) error {
		got, err := parse(raw)
		if err != nil {
			return err
		}
		parsed = got
		return nil
	})
	if ctx.Err() != nil {
		return
	}
	pid := p.ID
	ans := domain.StudyAnswer{StudyID: st.ID, PersonaID: &pid, Persona: snapshotOf(p), Status: "ok", Answers: parsed}
	failed := 0
	if err != nil {
		ans.Status, ans.Error, ans.Answers = "failed", clip(err.Error(), 300), domain.StudyAnswers{}
		failed = 1
	}
	if serr := s.d.Store.SaveAnswer(wctx, ans); serr != nil {
		log.Error().Err(serr).Str("study_id", st.ID.String()).Msg("audience: saving an answer failed")
		return
	}
	if _, _, berr := s.d.Store.BumpStudyProgress(wctx, st.ID, 1-failed, failed); berr != nil {
		log.Warn().Err(berr).Msg("audience: study progress not recorded")
	}
}

// subjectText is what the synthesis is told was tested.
func subjectText(st domain.Study) string {
	switch st.Kind {
	case domain.KindLandingPageTest:
		return fmt.Sprintf("Visitor intent: %s\n\n%s", intentOf(st), pageCopy(st, 0))
	case domain.KindMessageTesting:
		lines := make([]string, len(st.Inputs.Variants))
		for i, v := range st.Inputs.Variants {
			lines[i] = fmt.Sprintf("Line %d: %s", i+1, v)
		}
		return strings.Join(lines, "\n")
	case domain.KindABTest:
		return fmt.Sprintf("Visitor intent: %s\n\nPage A:\n%s\n\nPage B:\n%s", intentOf(st), pageCopy(st, 0), pageCopy(st, 1))
	default:
		return st.ProductSnapshot
	}
}

func synthSystemFor(kind domain.StudyKind) string {
	switch kind {
	case domain.KindLandingPageTest:
		return prompt.Text(synthLandingSystemKey)
	case domain.KindMessageTesting:
		return prompt.Text(synthMessagesSystemKey)
	case domain.KindABTest:
		return prompt.Text(synthABSystemKey)
	default:
		return prompt.Text(synthSystemKey)
	}
}

func methodologyFor(st domain.Study, personas, segments, calls int) string {
	in := methodologyKindInput{Personas: personas, Segments: segments, Calls: calls, Variants: len(st.Inputs.Variants), Intent: intentOf(st)}
	switch st.Kind {
	case domain.KindLandingPageTest:
		return methodologyLandingKey.Render(in)
	case domain.KindMessageTesting:
		return methodologyMessagesKey.Render(in)
	case domain.KindABTest:
		return methodologyABKey.Render(in)
	default:
		return methodologyKey.Render(methodologyInput{
			Personas: personas, Segments: segments, Calls: calls, Currency: st.Currency, PriceTests: strings.Join(priceStrings(st.PricePoints), ", "),
		})
	}
}
