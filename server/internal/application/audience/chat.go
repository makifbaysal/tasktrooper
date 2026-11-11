package audience

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type chatSystemInput struct{ Sheet, Product, Language, History string }

type researchUserInput struct {
	Title, Report, Question, Language string
	Personas                          []string
}

var (
	chatSystemKey     = prompt.Define("audience.chat_system", chatSystemInput{Sheet: "Who you are: Ayse.", Product: "A kanban for AI agents.", Language: "English", History: "You said maybe."})
	researchSystemKey = prompt.Define[struct{}]("audience.research_system", struct{}{})
	researchUserKey   = prompt.Define("audience.research_user", researchUserInput{Title: "Would they buy?", Report: "Mean 5.2", Question: "Who objected?", Personas: []string{"p1: no"}})
)

// Thread is a persona's conversation as the screen shows it.
func (s *Service) Thread(ctx context.Context, personaID uuid.UUID) ([]domain.ChatMessage, error) {
	if _, err := s.d.Store.GetPersona(ctx, personaID); err != nil {
		return nil, err
	}
	return s.d.Store.ListMessages(ctx, personaID)
}

func (s *Service) ClearThread(ctx context.Context, personaID uuid.UUID) error {
	if _, err := s.d.Store.GetPersona(ctx, personaID); err != nil {
		return err
	}
	return s.d.Store.ClearMessages(ctx, personaID)
}

// Chat sends one message to a persona and answers their reply. The person
// stays in character, remembers the thread and what they told the latest
// studies, and only knows the product as the audience's brief states it.
func (s *Service) Chat(ctx context.Context, personaID uuid.UUID, message string) (domain.ChatMessage, error) {
	return s.talk(ctx, personaID, message, nil, nil)
}

func (s *Service) talk(ctx context.Context, personaID uuid.UUID, message string, studyID, groupID *uuid.UUID) (domain.ChatMessage, error) {
	message = clip(strings.TrimSpace(message), 2000)
	if message == "" {
		return domain.ChatMessage{}, invalid("write a message first")
	}
	p, err := s.d.Store.GetPersona(ctx, personaID)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	a, err := s.d.Store.GetAudience(ctx, p.AudienceID)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	rt, err := s.agentFor(ctx)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	history, err := s.d.Store.ListMessages(ctx, personaID)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	if len(history) > maxThreadTurns {
		history = history[len(history)-maxThreadTurns:]
	}
	said, err := s.whatTheyTold(ctx, p)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	msgs := []domain.Message{{Role: domain.RoleSystem, Content: chatSystemKey.Render(chatSystemInput{
		Sheet: sheetOf(p.Name, p.Segment, p.PersonaProfile), Product: productText(a, a.ProductContext), Language: a.Brief.Language, History: said,
	})}}
	for _, m := range history {
		role := domain.RoleUser
		if m.Role == "persona" {
			role = domain.RoleAssistant
		}
		msgs = append(msgs, domain.Message{Role: role, Content: m.Content})
	}
	msgs = append(msgs, domain.Message{Role: domain.RoleUser, Content: message})

	reply, err := s.ask(ctx, rt, askRequest{Messages: msgs, Label: "audience-chat:" + p.Name, Title: "talk to " + p.Name, MaxTurns: 3})
	if err != nil {
		return domain.ChatMessage{}, fmt.Errorf("%s did not answer: %w", p.Name, err)
	}
	reply = clip(strings.TrimSpace(reply), 4000)
	if reply == "" {
		return domain.ChatMessage{}, fmt.Errorf("%s did not answer", p.Name)
	}
	user := domain.ChatMessage{PersonaID: personaID, StudyID: studyID, GroupID: groupID, Role: "user", Content: message}
	persona := domain.ChatMessage{PersonaID: personaID, StudyID: studyID, GroupID: groupID, Role: "persona", Content: reply}
	if err := s.d.Store.AddMessages(ctx, []domain.ChatMessage{user, persona}); err != nil {
		return domain.ChatMessage{}, err
	}
	stored, err := s.d.Store.ListMessages(ctx, personaID)
	if err != nil || len(stored) == 0 {
		persona.CreatedAt = s.d.Now()
		return persona, nil
	}
	return stored[len(stored)-1], nil
}

// whatTheyTold is what this person answered in the audience's studies, newest
// first, so a conversation stays consistent with the research.
func (s *Service) whatTheyTold(ctx context.Context, p domain.Persona) (string, error) {
	studies, err := s.d.Store.ListStudies(ctx, &p.AudienceID)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, st := range studies {
		if st.Status != domain.StudyDone || len(parts) >= 2 {
			continue
		}
		answers, err := s.d.Store.ListAnswers(ctx, st.ID)
		if err != nil {
			return "", err
		}
		for _, ans := range answers {
			if ans.Status != "ok" || ans.PersonaID == nil || *ans.PersonaID != p.ID {
				continue
			}
			parts = append(parts, fmt.Sprintf("In the study %q:\n%s", st.Title, answerDigest(ans.Answers, st.Currency)))
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func answerDigest(a domain.StudyAnswers, currency string) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("- You rated buying it %d out of 10 and said %s.", a.IntentScore, a.Verdict))
	if a.Reasoning != "" {
		lines = append(lines, "- Why: "+a.Reasoning)
	}
	if a.MaxPrice != nil {
		lines = append(lines, fmt.Sprintf("- The most you would pay: %s %s.", trimFloat(*a.MaxPrice), currency))
	}
	for _, r := range a.PriceReactions {
		verb := "would not buy"
		if r.WouldBuy {
			verb = "would buy"
		}
		lines = append(lines, fmt.Sprintf("- At %s %s you %s.", trimFloat(r.Price), currency, verb))
	}
	for _, kv := range []struct {
		label string
		items []string
	}{{"What is missing for you", a.Missing}, {"What holds you back", a.Objections}, {"What you would use instead", a.Alternatives}} {
		if len(kv.items) > 0 {
			lines = append(lines, "- "+kv.label+": "+strings.Join(kv.items, "; "))
		}
	}
	for _, qa := range a.Answers {
		lines = append(lines, fmt.Sprintf("- Asked %q you said: %s", qa.Question, qa.Answer))
	}
	return strings.Join(lines, "\n")
}

// ---- studies in detail

type FollowUpReply struct {
	PersonaID *uuid.UUID `json:"persona_id,omitempty"`
	Persona   string     `json:"persona"`
	Text      string     `json:"text"`
}

// FollowUpGroup is one question put to one person or to everyone, with the
// answers that came back.
type FollowUpGroup struct {
	GroupID  uuid.UUID       `json:"group_id"`
	Question string          `json:"question"`
	AskedAt  string          `json:"asked_at"`
	Asked    int             `json:"asked"`
	Replies  []FollowUpReply `json:"replies"`
}

// PendingPerson is someone a running study has not heard from yet.
type PendingPerson struct {
	PersonaID uuid.UUID `json:"persona_id"`
	Ordinal   int       `json:"ordinal"`
	Name      string    `json:"name"`
	Segment   string    `json:"segment,omitempty"`
	domain.PersonaProfile
}

type StudyDetail struct {
	domain.Study
	Answers []domain.StudyAnswer `json:"answers"`
	// Pending lists the people of the audience who have not answered while
	// the interviews run.
	Pending   []PendingPerson `json:"pending"`
	FollowUps []FollowUpGroup `json:"follow_ups"`
	// Running: a worker is on it now.
	Running bool `json:"running"`
}

func (s *Service) ListStudies(ctx context.Context, audienceID *uuid.UUID) ([]domain.Study, error) {
	studies, err := s.d.Store.ListStudies(ctx, audienceID)
	if err != nil {
		return nil, err
	}
	for i := range studies {
		studies[i].Report = nil
	}
	return studies, nil
}

func (s *Service) Study(ctx context.Context, id uuid.UUID) (StudyDetail, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return StudyDetail{}, err
	}
	answers, err := s.d.Store.ListAnswers(ctx, id)
	if err != nil {
		return StudyDetail{}, err
	}
	sortAnswers(answers)
	msgs, err := s.d.Store.ListStudyMessages(ctx, id)
	if err != nil {
		return StudyDetail{}, err
	}
	names := map[uuid.UUID]string{}
	for _, a := range answers {
		if a.PersonaID != nil {
			names[*a.PersonaID] = a.Persona.Name
		}
	}
	if a, err := s.d.Store.GetAudience(ctx, st.AudienceID); err == nil {
		st.AudienceName = a.Name
	}
	return StudyDetail{Study: st, Answers: answers, Pending: s.pendingPeople(ctx, st, answers), FollowUps: groupFollowUps(msgs, names), Running: s.running(id)}, nil
}

func (s *Service) pendingPeople(ctx context.Context, st domain.Study, answers []domain.StudyAnswer) []PendingPerson {
	out := []PendingPerson{}
	if st.Status != domain.StudyRunning {
		return out
	}
	personas, err := s.d.Store.ListPersonas(ctx, st.AudienceID)
	if err != nil {
		return out
	}
	done := map[uuid.UUID]bool{}
	for _, a := range answers {
		if a.PersonaID != nil {
			done[*a.PersonaID] = true
		}
	}
	for _, p := range personas {
		if !done[p.ID] {
			out = append(out, PendingPerson{PersonaID: p.ID, Ordinal: p.Ordinal, Name: p.Name, Segment: p.Segment, PersonaProfile: p.PersonaProfile})
		}
	}
	return out
}

func groupFollowUps(msgs []domain.ChatMessage, names map[uuid.UUID]string) []FollowUpGroup {
	index := map[uuid.UUID]*FollowUpGroup{}
	var order []uuid.UUID
	for _, m := range msgs {
		if m.GroupID == nil {
			continue
		}
		g := index[*m.GroupID]
		if g == nil {
			g = &FollowUpGroup{GroupID: *m.GroupID, Replies: []FollowUpReply{}, AskedAt: m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")}
			index[*m.GroupID] = g
			order = append(order, *m.GroupID)
		}
		if m.Role == "user" {
			g.Question = m.Content
			g.Asked++
			continue
		}
		pid := m.PersonaID
		g.Replies = append(g.Replies, FollowUpReply{PersonaID: &pid, Persona: names[pid], Text: m.Content})
	}
	out := make([]FollowUpGroup, 0, len(order))
	for _, id := range order {
		out = append(out, *index[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].AskedAt > out[j].AskedAt })
	return out
}

// FollowUpCalls is how many LLM calls asking everyone costs.
func (s *Service) FollowUpCalls(ctx context.Context, studyID uuid.UUID) (int, error) {
	answers, err := s.d.Store.ListAnswers(ctx, studyID)
	if err != nil {
		return 0, err
	}
	return len(s.askable(ctx, answers)), nil
}

// askable are the answers whose person still exists to be asked again.
func (s *Service) askable(ctx context.Context, answers []domain.StudyAnswer) []domain.StudyAnswer {
	var out []domain.StudyAnswer
	for _, a := range answers {
		if a.Status != "ok" || a.PersonaID == nil {
			continue
		}
		if _, err := s.d.Store.GetPersona(ctx, *a.PersonaID); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// FollowUp asks one person of a finished study another question; the answer is
// kept in their thread and listed under the study.
func (s *Service) FollowUp(ctx context.Context, studyID, personaID uuid.UUID, question string) (domain.ChatMessage, error) {
	st, err := s.d.Store.GetStudy(ctx, studyID)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	answers, err := s.d.Store.ListAnswers(ctx, studyID)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	found := false
	for _, a := range answers {
		if a.PersonaID != nil && *a.PersonaID == personaID {
			found = true
		}
	}
	if !found {
		return domain.ChatMessage{}, domain.ErrPersonaNotFound
	}
	group := uuid.New()
	return s.talk(ctx, personaID, question, &st.ID, &group)
}

// AskEveryone puts the same question to every person who answered the study,
// in the background; the replies appear under the study as they arrive. It
// answers with the group id and the number of calls it spends.
func (s *Service) AskEveryone(ctx context.Context, studyID uuid.UUID, question string) (uuid.UUID, int, error) {
	question = clip(strings.TrimSpace(question), 2000)
	if question == "" {
		return uuid.Nil, 0, invalid("write a question first")
	}
	st, err := s.d.Store.GetStudy(ctx, studyID)
	if err != nil {
		return uuid.Nil, 0, err
	}
	if st.Status != domain.StudyDone {
		return uuid.Nil, 0, invalid("ask everyone once the study is done")
	}
	answers, err := s.d.Store.ListAnswers(ctx, studyID)
	if err != nil {
		return uuid.Nil, 0, err
	}
	people := s.askable(ctx, answers)
	if len(people) == 0 {
		return uuid.Nil, 0, invalid("none of the people of this study exist any more; the audience was rebuilt")
	}
	if _, err := s.agentFor(ctx); err != nil {
		return uuid.Nil, 0, err
	}
	runCtx, done, err := s.begin(ctx, uuid.New())
	if err != nil {
		return uuid.Nil, 0, err
	}
	group := uuid.New()
	go func() {
		defer done()
		var wg sync.WaitGroup
		for _, a := range people {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.talk(runCtx, *a.PersonaID, question, &st.ID, &group); err != nil && runCtx.Err() == nil {
					log.Warn().Err(err).Str("study_id", st.ID.String()).Msg("audience: a persona did not answer the follow-up")
				}
			}()
		}
		wg.Wait()
	}()
	return group, len(people), nil
}

// AskResearch answers a question about a finished study from its report and
// the people's answers. One call; nothing is kept.
func (s *Service) AskResearch(ctx context.Context, studyID uuid.UUID, question string) (string, error) {
	question = clip(strings.TrimSpace(question), 2000)
	if question == "" {
		return "", invalid("write a question first")
	}
	st, err := s.d.Store.GetStudy(ctx, studyID)
	if err != nil {
		return "", err
	}
	if st.Status != domain.StudyDone || st.Report == nil {
		return "", invalid("the study has no report yet")
	}
	a, err := s.d.Store.GetAudience(ctx, st.AudienceID)
	if err != nil {
		return "", err
	}
	answers, err := s.d.Store.ListAnswers(ctx, studyID)
	if err != nil {
		return "", err
	}
	rt, err := s.agentFor(ctx)
	if err != nil {
		return "", err
	}
	sortAnswers(answers)
	ok := okAnswers(answers)
	ids := make([]string, len(ok))
	for i := range ok {
		ids[i] = fmt.Sprintf("p%d", i+1)
	}
	text, err := s.ask(ctx, rt, askRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: prompt.Text(researchSystemKey)},
			{Role: domain.RoleUser, Content: researchUserKey.Render(researchUserInput{
				Title: st.Title, Report: ReportText(*st.Report), Personas: personaDigests(ids, ok), Question: question, Language: a.Brief.Language,
			})},
		},
		Label: "audience-study:ask", Title: "answer a question about a study", MaxTurns: 4,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// ReportText is the report in the plain form an LLM or an agent reads.
func ReportText(r domain.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Verdict: %s (purchase-intent index %.0f of 100)\n", r.Verdict.Label, r.Verdict.Score)
	if r.Verdict.Rationale != "" {
		b.WriteString(r.Verdict.Rationale + "\n")
	}
	b.WriteString("\n" + r.Summary + "\n\n" + strings.TrimSpace(statsText(r)) + "\n")
	for _, group := range []struct {
		title  string
		themes []domain.Theme
	}{{"Objections", r.Objections}, {"What is missing", r.Needs}, {"Alternatives", r.Alternatives}} {
		if len(group.themes) == 0 {
			continue
		}
		b.WriteString("\n" + group.title + ":\n")
		for _, t := range group.themes {
			fmt.Fprintf(&b, "- %s (%d people)\n", t.Label, t.Count)
		}
	}
	for _, rec := range r.Recommendations {
		fmt.Fprintf(&b, "\nRecommendation (%s): %s. %s", rec.Priority, rec.Title, rec.Detail)
	}
	return b.String()
}
