package audience

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type sheetInput struct {
	Name, Segment, Role, Location, Bio, CurrentSolution, Budget, TechSavviness, Personality string
	Age                                                                                     int
	Goals, Pains                                                                            []string
}

type answerUserInput struct {
	Product, Context, Currency string
	PricePoints, Questions     []string
}

type answerSystemInput struct{ Sheet, Language string }

var (
	sheetKey = prompt.Define("audience.sheet", sheetInput{
		Name: "Ayse Demir", Segment: "Freelancers", Role: "UX designer", Location: "Istanbul", Age: 31,
		Goals: []string{"more clients"}, Pains: []string{"tool sprawl"}, Budget: "moderate",
	})
	answerSystemKey = prompt.Define("audience.answer_system", answerSystemInput{Sheet: "Who you are: Ayse.", Language: "English"})
	answerUserKey   = prompt.Define("audience.answer_user", answerUserInput{
		Product: "A kanban for AI agents.", Currency: "USD", PricePoints: []string{"9", "19"}, Questions: []string{"What would convince you?"},
	})
)

func sheetOf(name, segment string, p domain.PersonaProfile) string {
	return sheetKey.Render(sheetInput{
		Name: name, Segment: segment, Age: p.Age, Role: p.Role, Location: p.Location, Bio: p.Bio, Goals: p.Goals, Pains: p.Pains,
		CurrentSolution: p.CurrentSolution, Budget: p.Budget, TechSavviness: p.TechSavviness, Personality: p.Personality,
	})
}

// productText is what a persona is told about the product: the owner's
// description, the price they state and what the project contains now.
func productText(a domain.Audience, context string) string {
	var b strings.Builder
	b.WriteString(a.Brief.Product)
	if a.Brief.Pricing != "" {
		b.WriteString("\n\nPrice: " + a.Brief.Pricing)
	}
	if context != "" {
		b.WriteString("\n\nWhat the project contains right now:\n" + context)
	}
	return b.String()
}

// StudyCalls is the LLM calls a study of n people takes: one per person and
// the synthesis.
func StudyCalls(n int) int { return n + 1 }

type PlanStudyInput struct {
	Title       string
	Goal        string
	Questions   []string
	PricePoints []float64
	Currency    string
}

// PlanStudy writes a study down without running it. Nothing is asked until
// StartStudy, which is where the calls are spent.
func (s *Service) PlanStudy(ctx context.Context, audienceID uuid.UUID, in PlanStudyInput) (domain.Study, error) {
	a, err := s.d.Store.GetAudience(ctx, audienceID)
	if err != nil {
		return domain.Study{}, err
	}
	personas, err := s.d.Store.ListPersonas(ctx, audienceID)
	if err != nil {
		return domain.Study{}, err
	}
	if len(personas) == 0 {
		return domain.Study{}, invalid("the audience has no people yet; build it first")
	}
	st, err := normalizeStudy(domain.Study{AudienceID: audienceID, Title: in.Title, Goal: in.Goal, Questions: in.Questions, PricePoints: in.PricePoints, Currency: in.Currency})
	if err != nil {
		return domain.Study{}, err
	}
	if st.Title == "" {
		st.Title = clip(a.Name+": would they buy?", 120)
	}
	st.PersonaTotal = len(personas)
	st.CallsEstimate = StudyCalls(len(personas))
	return s.d.Store.CreateStudy(ctx, st)
}

func normalizeStudy(st domain.Study) (domain.Study, error) {
	st.Title = clip(strings.TrimSpace(st.Title), 120)
	st.Goal = clip(strings.TrimSpace(st.Goal), 600)
	var qs []string
	for _, q := range st.Questions {
		if q = clip(strings.TrimSpace(q), maxQuestionLen); q != "" {
			qs = append(qs, q)
		}
	}
	if len(qs) > maxExtraQuestions {
		return st, invalid("at most %d extra questions", maxExtraQuestions)
	}
	st.Questions = qs
	var prices []float64
	seen := map[float64]bool{}
	for _, p := range st.PricePoints {
		if p < 0 || math.IsNaN(p) || math.IsInf(p, 0) {
			return st, invalid("a price point must be zero or more")
		}
		p = math.Round(p*100) / 100
		if !seen[p] {
			seen[p] = true
			prices = append(prices, p)
		}
	}
	if len(prices) > maxPricePoints {
		return st, invalid("at most %d price points", maxPricePoints)
	}
	sort.Float64s(prices)
	st.PricePoints = prices
	st.Currency = strings.ToUpper(clip(strings.TrimSpace(st.Currency), 3))
	if st.Currency == "" {
		st.Currency = "USD"
	}
	return st, nil
}

// UpdateStudy edits a study that has not started.
func (s *Service) UpdateStudy(ctx context.Context, id uuid.UUID, in PlanStudyInput) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyDraft {
		return domain.Study{}, invalid("a study that has started cannot be edited; plan a new one")
	}
	next, err := normalizeStudy(domain.Study{Title: firstNonEmpty(in.Title, st.Title), Goal: in.Goal, Questions: in.Questions, PricePoints: in.PricePoints, Currency: firstNonEmpty(in.Currency, st.Currency)})
	if err != nil {
		return domain.Study{}, err
	}
	st.Title, st.Goal, st.Questions, st.PricePoints, st.Currency = next.Title, next.Goal, next.Questions, next.PricePoints, next.Currency
	if err := s.d.Store.SaveStudy(ctx, st); err != nil {
		return domain.Study{}, err
	}
	return s.d.Store.GetStudy(ctx, id)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// StartStudy asks every person of the audience, in the background, and then
// writes the report. It answers at once.
func (s *Service) StartStudy(ctx context.Context, id uuid.UUID) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyDraft {
		return domain.Study{}, invalid("this study already ran; plan a new one to ask again")
	}
	a, err := s.d.Store.GetAudience(ctx, st.AudienceID)
	if err != nil {
		return domain.Study{}, err
	}
	if a.Status == domain.AudienceBuilding {
		return domain.Study{}, invalid("the audience is still being built")
	}
	personas, err := s.d.Store.ListPersonas(ctx, a.ID)
	if err != nil {
		return domain.Study{}, err
	}
	if len(personas) == 0 {
		return domain.Study{}, invalid("the audience has no people yet; build it first")
	}
	rt, err := s.agentFor(ctx)
	if err != nil {
		return domain.Study{}, err
	}
	runCtx, done, err := s.begin(ctx, st.ID)
	if err != nil {
		return domain.Study{}, err
	}
	now := s.d.Now()
	st.ProductSnapshot = productText(a, s.projectContext(ctx, a.RepositoryID))
	st.Status = domain.StudyRunning
	st.PersonaTotal, st.PersonaDone, st.PersonaFailed = len(personas), 0, 0
	st.CallsEstimate = StudyCalls(len(personas))
	st.StartedAt, st.FinishedAt, st.Error = &now, nil, ""
	if err := s.d.Store.SaveStudy(ctx, st); err != nil {
		done()
		return domain.Study{}, err
	}
	go func() {
		defer done()
		s.runStudy(runCtx, st, a, personas, rt)
	}()
	return st, nil
}

// CancelStudy stops a running study. What was answered stays; no report is
// written.
func (s *Service) CancelStudy(ctx context.Context, id uuid.UUID) error {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return err
	}
	if st.Status.Terminal() || st.Status == domain.StudyDraft {
		return nil
	}
	now := s.d.Now()
	st.Status, st.FinishedAt, st.Error = domain.StudyCancelled, &now, "Cancelled."
	if err := s.d.Store.SaveStudy(ctx, st); err != nil {
		return err
	}
	s.cancelRun(id)
	return nil
}

func (s *Service) DeleteStudy(ctx context.Context, id uuid.UUID) error {
	if s.running(id) {
		return ErrBusy
	}
	return s.d.Store.DeleteStudy(ctx, id)
}

func (s *Service) runStudy(ctx context.Context, st domain.Study, a domain.Audience, personas []domain.Persona, rt domain.Agent) {
	wctx := context.WithoutCancel(ctx)
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("study_id", st.ID.String()).Msg("audience: study panicked")
			s.finishStudy(wctx, st, nil, "The study crashed.")
		}
	}()

	var wg sync.WaitGroup
	for _, p := range personas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.askPersona(ctx, wctx, st, a, p, rt)
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return // cancelled: CancelStudy wrote the outcome
	}

	answers, err := s.d.Store.ListAnswers(wctx, st.ID)
	if err != nil {
		s.finishStudy(wctx, st, nil, err.Error())
		return
	}
	sortAnswers(answers)
	var ok int
	for _, ans := range answers {
		if ans.Status == "ok" {
			ok++
		}
	}
	if ok == 0 {
		s.finishStudy(wctx, st, nil, "No one answered. Check that the agent can run, then plan the study again.")
		return
	}
	cur, err := s.d.Store.GetStudy(wctx, st.ID)
	if err != nil || cur.Status.Terminal() {
		return
	}
	cur.Status = domain.StudySynthesizing
	if err := s.d.Store.SaveStudy(wctx, cur); err != nil {
		log.Warn().Err(err).Msg("audience: marking the study synthesizing failed")
	}
	report := s.synthesize(ctx, st, a, answers, rt)
	if ctx.Err() != nil {
		return
	}
	s.finishStudy(wctx, st, &report, "")
}

// askPersona asks one person the study's questions and keeps the answer; a
// person who cannot answer is recorded as failed, never as a "no".
func (s *Service) askPersona(ctx, wctx context.Context, st domain.Study, a domain.Audience, p domain.Persona, rt domain.Agent) {
	var parsed domain.StudyAnswers
	err := s.askJSON(ctx, rt, askRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: answerSystemKey.Render(answerSystemInput{Sheet: sheetOf(p.Name, p.Segment, p.PersonaProfile), Language: a.Brief.Language})},
			{Role: domain.RoleUser, Content: answerUserKey.Render(answerUserInput{
				Product: st.ProductSnapshot, Currency: st.Currency, PricePoints: priceStrings(st.PricePoints), Questions: st.Questions,
			})},
		},
		Label: "audience-study:" + p.Name, Title: "interview " + p.Name, MaxTurns: 4,
	}, func(raw string) error {
		got, err := parseAnswers(raw, st.PricePoints)
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

func snapshotOf(p domain.Persona) domain.PersonaSnapshot {
	return domain.PersonaSnapshot{Ordinal: p.Ordinal, Name: p.Name, Segment: p.Segment, PersonaProfile: p.PersonaProfile}
}

func priceStrings(prices []float64) []string {
	out := make([]string, 0, len(prices))
	for _, p := range prices {
		out = append(out, trimFloat(p))
	}
	return out
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

func (s *Service) finishStudy(ctx context.Context, st domain.Study, report *domain.Report, failure string) {
	cur, err := s.d.Store.GetStudy(ctx, st.ID)
	if err != nil {
		log.Error().Err(err).Str("study_id", st.ID.String()).Msg("audience: reading the study to finish it failed")
		return
	}
	if cur.Status.Terminal() {
		return // cancelled while the last call was in flight
	}
	now := s.d.Now()
	cur.FinishedAt = &now
	if failure != "" {
		cur.Status, cur.Error = domain.StudyFailed, clip(failure, 500)
	} else {
		cur.Status, cur.Report, cur.Error = domain.StudyDone, report, ""
	}
	if err := s.d.Store.SaveStudy(ctx, cur); err != nil {
		log.Error().Err(err).Str("study_id", st.ID.String()).Msg("audience: saving the study outcome failed")
	}
}

// parseAnswers reads one person's structured answers. The score is the one
// thing it insists on: without it there is nothing to count.
func parseAnswers(raw string, asked []float64) (domain.StudyAnswers, error) {
	var m map[string]any
	if err := decodeObject(raw, &m); err != nil {
		return domain.StudyAnswers{}, err
	}
	score, ok := intOf(m, "intent_score", "intent", "score")
	if !ok {
		return domain.StudyAnswers{}, fmt.Errorf(`"intent_score" is missing`)
	}
	if score < 0 {
		score = 0
	}
	if score > 10 {
		score = 10
	}
	out := domain.StudyAnswers{IntentScore: score}
	out.Verdict = oneOf(strOf(m, "verdict"), "yes", "maybe", "no")
	if out.Verdict == "" {
		out.Verdict = verdictFromScore(score)
	}
	if v, ok := boolOf(m, "would_pay_now", "would_pay", "would_buy"); ok {
		out.WouldPayNow = v
	} else {
		out.WouldPayNow = out.Verdict == "yes"
	}
	out.Reasoning = clip(strOf(m, "reasoning", "reason"), 600)
	if v, ok := floatOf(m, "max_price", "maxPrice", "max_price_per_month"); ok && v >= 0 {
		v = math.Round(v*100) / 100
		out.MaxPrice = &v
	}
	for _, r := range objectsOf(m, "price_reactions", "priceReactions", "prices") {
		price, ok := floatOf(r, "price")
		if !ok {
			continue
		}
		buy, _ := boolOf(r, "would_buy", "buy", "wouldBuy")
		if p, matched := matchPrice(price, asked); matched {
			out.PriceReactions = append(out.PriceReactions, domain.PriceReaction{Price: p, WouldBuy: buy, Comment: clip(strOf(r, "comment"), 240)})
		}
	}
	out.Missing = listOf(m, 6, 160, "missing", "missing_features")
	out.Objections = listOf(m, 6, 160, "objections")
	out.Alternatives = listOf(m, 6, 160, "alternatives")
	out.Quote = clip(strOf(m, "quote"), 300)
	for _, qa := range objectsOf(m, "answers") {
		q, a := strOf(qa, "question"), strOf(qa, "answer")
		if q != "" && a != "" {
			out.Answers = append(out.Answers, domain.QuestionAnswer{Question: clip(q, maxQuestionLen), Answer: clip(a, 600)})
		}
	}
	return out, nil
}

func verdictFromScore(score int) string {
	switch {
	case score >= 7:
		return "yes"
	case score >= 4:
		return "maybe"
	default:
		return "no"
	}
}

func matchPrice(p float64, asked []float64) (float64, bool) {
	for _, a := range asked {
		if math.Abs(a-p) < 0.005 {
			return a, true
		}
	}
	return 0, false
}

// sortAnswers puts a study's answers in the audience's order.
func sortAnswers(answers []domain.StudyAnswer) {
	sort.SliceStable(answers, func(i, j int) bool {
		a, b := answers[i].Persona, answers[j].Persona
		if a.Ordinal != b.Ordinal {
			return a.Ordinal < b.Ordinal
		}
		return a.Name < b.Name
	})
}
