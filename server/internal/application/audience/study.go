package audience

import (
	"context"
	"encoding/json"
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
	// Kind is a domain.StudyKind; empty is user_interviews.
	Kind   string
	Inputs domain.StudyInputs
	// Script, when given, is the interview script itself and nothing is
	// written for the study; so are Questions.
	Script *domain.StudyScript
	// ParentStudyID and Changes make the study a re-run of an earlier one.
	ParentStudyID *uuid.UUID
	Changes       string
}

// PlanStudy writes a study down without running it. The interview script is
// written by the person's agent in the background (one model call) unless the
// plan brings its own questions; the people are asked nothing until
// StartStudy, which is where the rest of the calls are spent.
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
	kind, ok := domain.ParseStudyKind(in.Kind)
	if !ok {
		return domain.Study{}, invalid("unknown study kind %q; use user_interviews, landing_page_test, message_testing or ab_test", in.Kind)
	}
	inputs, err := s.normalizeInputs(ctx, kind, in.Inputs)
	if err != nil {
		return domain.Study{}, err
	}
	st, err := normalizeStudy(domain.Study{AudienceID: audienceID, Kind: kind, Title: in.Title, Goal: in.Goal, Questions: in.Questions, PricePoints: in.PricePoints, Currency: in.Currency})
	if err != nil {
		return domain.Study{}, err
	}
	st.Inputs = inputs
	st.ParentStudyID, st.Changes = in.ParentStudyID, strings.TrimSpace(in.Changes)
	if st.Title == "" {
		st.Title = clip(a.Name+": "+defaultTitleSuffix(kind), 120)
	}
	st.PersonaTotal = len(personas)

	generate := false
	switch {
	case in.Script != nil:
		sc, err := normalizeScript(in.Script)
		if err != nil {
			return domain.Study{}, err
		}
		setScript(&st, sc)
	case len(st.Questions) > 0:
		setScript(&st, scriptFromQuestions(st.Questions))
	default:
		st.ScriptState = domain.ScriptNone
		if _, err := s.agentFor(ctx); err == nil {
			st.ScriptState, st.ScriptCalls = domain.ScriptGenerating, 1
			generate = true
		}
	}
	st.CallsEstimate = StudyCallsFor(kind, len(personas)) + st.ScriptCalls
	created, err := s.d.Store.CreateStudy(ctx, st)
	if err != nil {
		return domain.Study{}, err
	}
	if generate {
		if err := s.beginScript(ctx, created, a); err != nil {
			created.ScriptState, created.ScriptCalls, created.Error = domain.ScriptFailed, 0, clip(err.Error(), 300)
			created.CallsEstimate = StudyCallsFor(kind, len(personas))
			if serr := s.d.Store.SaveStudy(ctx, created); serr != nil {
				return domain.Study{}, serr
			}
		}
	}
	return s.d.Store.GetStudy(ctx, created.ID)
}

// defaultTitleSuffix is the part of a study's default title that depends on its
// kind; it is a name, not a sentence the model reads.
func defaultTitleSuffix(kind domain.StudyKind) string {
	switch kind {
	case domain.KindLandingPageTest:
		return "landing page test"
	case domain.KindMessageTesting:
		return "message test"
	case domain.KindABTest:
		return "A/B test"
	default:
		return "would they buy?"
	}
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
	if st.Kind != "" && st.Kind != domain.KindUserInterviews {
		prices = nil
	}
	st.PricePoints = prices
	st.Currency = strings.ToUpper(clip(strings.TrimSpace(st.Currency), 3))
	if st.Currency == "" {
		st.Currency = "USD"
	}
	return st, nil
}

// UpdateStudy edits a study that has not started. A field left empty (nil)
// keeps its value; a script or a list of questions replaces the script.
func (s *Service) UpdateStudy(ctx context.Context, id uuid.UUID, in PlanStudyInput) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyDraft {
		return domain.Study{}, invalid("a study that has started cannot be edited; plan a new one")
	}
	if st.ScriptState == domain.ScriptGenerating {
		return domain.Study{}, invalid("the script is being written; wait for it")
	}
	if in.Kind != "" {
		kind, ok := domain.ParseStudyKind(in.Kind)
		if !ok {
			return domain.Study{}, invalid("unknown study kind %q", in.Kind)
		}
		st.Kind = kind
	}
	if in.Kind != "" || len(in.Inputs.Pages) > 0 || len(in.Inputs.Variants) > 0 || in.Inputs.Intent != "" {
		inputs := in.Inputs
		if in.Kind == "" && inputs.Intent == "" {
			inputs.Intent = st.Inputs.Intent
		}
		if inputs, err = s.normalizeInputs(ctx, st.Kind, inputs); err != nil {
			return domain.Study{}, err
		}
		if st.ScriptState == domain.ScriptApproved && !inputsEqual(st.Inputs, inputs) {
			setScript(&st, st.Script)
		}
		st.Inputs = inputs
	}
	next, err := normalizeStudy(domain.Study{Kind: st.Kind, Title: firstNonEmpty(in.Title, st.Title), Goal: firstNonEmpty(in.Goal, st.Goal), Questions: in.Questions,
		PricePoints: in.PricePoints, Currency: firstNonEmpty(in.Currency, st.Currency)})
	if err != nil {
		return domain.Study{}, err
	}
	st.Title, st.Goal, st.Currency = next.Title, next.Goal, next.Currency
	if in.PricePoints != nil || st.Kind != domain.KindUserInterviews {
		st.PricePoints = next.PricePoints
	}
	switch {
	case in.Script != nil:
		sc, err := normalizeScript(in.Script)
		if err != nil {
			return domain.Study{}, err
		}
		setScript(&st, sc)
	case in.Questions != nil:
		setScript(&st, scriptFromQuestions(next.Questions))
	}
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

// GenerateScript has the person's agent write the interview script again (one
// model call). Only a study that has not started can have it.
func (s *Service) GenerateScript(ctx context.Context, id uuid.UUID) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyDraft {
		return domain.Study{}, invalid("a study that has started has its script")
	}
	if st.ScriptState == domain.ScriptGenerating {
		return domain.Study{}, ErrBusy
	}
	a, err := s.d.Store.GetAudience(ctx, st.AudienceID)
	if err != nil {
		return domain.Study{}, err
	}
	if _, err := s.agentFor(ctx); err != nil {
		return domain.Study{}, err
	}
	st.ScriptState, st.ScriptCalls, st.Error = domain.ScriptGenerating, st.ScriptCalls+1, ""
	st.ScriptApprovedVersion, st.ScriptApprovedAt, st.ScriptApprovedBy = 0, nil, ""
	st.CallsEstimate = StudyCallsFor(st.Kind, st.PersonaTotal) + st.ScriptCalls
	if err := s.d.Store.SaveStudy(ctx, st); err != nil {
		return domain.Study{}, err
	}
	if err := s.beginScript(ctx, st, a); err != nil {
		st.ScriptState, st.ScriptCalls, st.Error = domain.ScriptFailed, st.ScriptCalls-1, clip(err.Error(), 300)
		st.CallsEstimate = StudyCallsFor(st.Kind, st.PersonaTotal) + st.ScriptCalls
		if serr := s.d.Store.SaveStudy(ctx, st); serr != nil {
			return domain.Study{}, serr
		}
	}
	return s.d.Store.GetStudy(ctx, id)
}

// setScript replaces the script. Every replacement is a new version that waits
// for the person's approval; an earlier approval is gone.
func setScript(st *domain.Study, sc *domain.StudyScript) {
	st.Script, st.Questions, st.Error = sc, sc.Flat(), ""
	st.ScriptState = domain.ScriptAwaitingApproval
	st.ScriptVersion++
	st.ScriptApprovedVersion, st.ScriptApprovedAt, st.ScriptApprovedBy = 0, nil, ""
}

func inputsEqual(a, b domain.StudyInputs) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// ApproveScript records the person's OK to one version of the script. version
// is the one they were shown; if the script has changed since, nothing is
// approved. A study with no script at all can be approved too: it then asks
// only the kind's built-in questions.
func (s *Service) ApproveScript(ctx context.Context, id uuid.UUID, version int, by string) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyDraft {
		return domain.Study{}, invalid("a study that has started has its script")
	}
	switch st.ScriptState {
	case domain.ScriptGenerating:
		return domain.Study{}, invalid("the interview script is still being written")
	case domain.ScriptFailed:
		return domain.Study{}, invalid("the interview script could not be written; write it again or add your own questions")
	case domain.ScriptApproved:
		if st.ScriptApprovedVersion == st.ScriptVersion && version == st.ScriptVersion {
			return st, nil
		}
	}
	if version != st.ScriptVersion {
		return domain.Study{}, ErrScriptChanged
	}
	now := s.d.Now()
	st.ScriptState, st.ScriptApprovedVersion, st.ScriptApprovedAt, st.ScriptApprovedBy = domain.ScriptApproved, st.ScriptVersion, &now, clip(strings.TrimSpace(by), 120)
	if err := s.d.Store.SaveStudy(ctx, st); err != nil {
		return domain.Study{}, err
	}
	return st, nil
}

// StartOptions says how a study runs.
type StartOptions struct {
	// ManualReport stops after the interviews, with the study interviewed,
	// until GenerateReport is asked for; otherwise the report follows at once.
	ManualReport bool
}

// StartStudy asks every person of the audience, in the background, and then
// writes the report. It answers at once.
func (s *Service) StartStudy(ctx context.Context, id uuid.UUID) (domain.Study, error) {
	return s.StartStudyWith(ctx, id, StartOptions{})
}

func (s *Service) StartStudyWith(ctx context.Context, id uuid.UUID, opts StartOptions) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyDraft {
		return domain.Study{}, invalid("this study already ran; plan a new one to ask again")
	}
	switch st.ScriptState {
	case domain.ScriptGenerating:
		return domain.Study{}, invalid("the interview script is still being written")
	case domain.ScriptFailed:
		return domain.Study{}, invalid("the interview script could not be written; write it again or add your own questions")
	case domain.ScriptApproved:
		if st.ScriptApprovedVersion != st.ScriptVersion {
			return domain.Study{}, ErrScriptNotApproved
		}
	default:
		return domain.Study{}, ErrScriptNotApproved
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
	st.ProductSnapshot = withChanges(productText(a, s.projectContext(ctx, a.RepositoryID)), st.Changes)
	st.Status = domain.StudyRunning
	st.PersonaTotal, st.PersonaDone, st.PersonaFailed = len(personas), 0, 0
	st.CallsEstimate = StudyCallsFor(st.Kind, len(personas)) + st.ScriptCalls
	st.StartedAt, st.FinishedAt, st.Error = &now, nil, ""
	if err := s.d.Store.SaveStudy(ctx, st); err != nil {
		done()
		return domain.Study{}, err
	}
	go func() {
		defer done()
		s.runStudy(runCtx, st, a, personas, rt, opts.ManualReport)
	}()
	return st, nil
}

// GenerateReport writes the report of a study whose interviews are over (one
// model call). It answers at once; the report follows in the background.
func (s *Service) GenerateReport(ctx context.Context, id uuid.UUID) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyInterviewed {
		return domain.Study{}, invalid("the report can be generated once the interviews are over")
	}
	answers, err := s.d.Store.ListAnswers(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if len(okAnswers(answers)) == 0 {
		return domain.Study{}, invalid("no interview was completed; retry the failed ones first")
	}
	a, err := s.d.Store.GetAudience(ctx, st.AudienceID)
	if err != nil {
		return domain.Study{}, err
	}
	rt, err := s.agentFor(ctx)
	if err != nil {
		return domain.Study{}, err
	}
	runCtx, done, err := s.begin(ctx, st.ID)
	if err != nil {
		return domain.Study{}, err
	}
	st.Status, st.Error = domain.StudySynthesizing, ""
	if err := s.d.Store.SaveStudy(ctx, st); err != nil {
		done()
		return domain.Study{}, err
	}
	go func() {
		defer done()
		wctx := context.WithoutCancel(runCtx)
		defer s.recoverStudy(wctx, st)
		s.writeReport(runCtx, wctx, st, a, rt)
	}()
	return st, nil
}

// RetryFailed asks again the people whose interview failed, in a study that is
// waiting for its report. It answers at once.
func (s *Service) RetryFailed(ctx context.Context, id uuid.UUID) (domain.Study, error) {
	st, err := s.d.Store.GetStudy(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	if st.Status != domain.StudyInterviewed {
		return domain.Study{}, invalid("failed interviews can be retried once the interviews are over")
	}
	answers, err := s.d.Store.ListAnswers(ctx, id)
	if err != nil {
		return domain.Study{}, err
	}
	failed := map[uuid.UUID]bool{}
	for _, ans := range answers {
		if ans.Status == "failed" && ans.PersonaID != nil {
			failed[*ans.PersonaID] = true
		}
	}
	if len(failed) == 0 {
		return domain.Study{}, invalid("no interview failed")
	}
	a, err := s.d.Store.GetAudience(ctx, st.AudienceID)
	if err != nil {
		return domain.Study{}, err
	}
	all, err := s.d.Store.ListPersonas(ctx, a.ID)
	if err != nil {
		return domain.Study{}, err
	}
	var again []domain.Persona
	for _, p := range all {
		if failed[p.ID] {
			again = append(again, p)
		}
	}
	if len(again) == 0 {
		return domain.Study{}, invalid("those people are no longer in the audience")
	}
	rt, err := s.agentFor(ctx)
	if err != nil {
		return domain.Study{}, err
	}
	runCtx, done, err := s.begin(ctx, st.ID)
	if err != nil {
		return domain.Study{}, err
	}
	if _, err := s.d.Store.DeleteFailedAnswers(ctx, id); err != nil {
		done()
		return domain.Study{}, err
	}
	st.Status, st.PersonaFailed, st.Error = domain.StudyRunning, 0, ""
	st.CallsEstimate += len(again)
	if err := s.d.Store.SaveStudy(ctx, st); err != nil {
		done()
		return domain.Study{}, err
	}
	go func() {
		defer done()
		wctx := context.WithoutCancel(runCtx)
		defer s.recoverStudy(wctx, st)
		s.askAll(runCtx, wctx, st, a, again, rt)
		if runCtx.Err() != nil {
			return
		}
		s.holdForReport(wctx, st.ID)
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

func (s *Service) recoverStudy(wctx context.Context, st domain.Study) {
	if r := recover(); r != nil {
		log.Error().Interface("panic", r).Str("study_id", st.ID.String()).Msg("audience: study panicked")
		s.finishStudy(wctx, st, nil, "The study crashed.")
	}
}

func (s *Service) runStudy(ctx context.Context, st domain.Study, a domain.Audience, personas []domain.Persona, rt domain.Agent, manualReport bool) {
	wctx := context.WithoutCancel(ctx)
	defer s.recoverStudy(wctx, st)

	s.askAll(ctx, wctx, st, a, personas, rt)
	if ctx.Err() != nil {
		return // cancelled: CancelStudy wrote the outcome
	}
	if manualReport {
		s.holdForReport(wctx, st.ID)
		return
	}
	s.writeReport(ctx, wctx, st, a, rt)
}

// askAll interviews these people, a few at a time, and returns when all are done.
func (s *Service) askAll(ctx, wctx context.Context, st domain.Study, a domain.Audience, personas []domain.Persona, rt domain.Agent) {
	var wg sync.WaitGroup
	for _, p := range personas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.askPersona(ctx, wctx, st, a, p, rt)
		}()
	}
	wg.Wait()
}

// holdForReport moves a study whose interviews are over to interviewed.
func (s *Service) holdForReport(wctx context.Context, id uuid.UUID) {
	cur, err := s.d.Store.GetStudy(wctx, id)
	if err != nil || cur.Status.Terminal() {
		return
	}
	cur.Status = domain.StudyInterviewed
	if err := s.d.Store.SaveStudy(wctx, cur); err != nil {
		log.Warn().Err(err).Msg("audience: marking the study interviewed failed")
	}
}

// writeReport turns the answers into the report and finishes the study.
func (s *Service) writeReport(ctx, wctx context.Context, st domain.Study, a domain.Audience, rt domain.Agent) {
	answers, err := s.d.Store.ListAnswers(wctx, st.ID)
	if err != nil {
		s.finishStudy(wctx, st, nil, err.Error())
		return
	}
	sortAnswers(answers)
	if len(okAnswers(answers)) == 0 {
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
	st = cur
	report := s.synthesize(ctx, st, a, answers, rt)
	if ctx.Err() != nil {
		return
	}
	s.finishStudy(wctx, st, &report, "")
}

// beginScript writes the script in the background.
func (s *Service) beginScript(ctx context.Context, st domain.Study, a domain.Audience) error {
	rt, err := s.agentFor(ctx)
	if err != nil {
		return err
	}
	runCtx, done, err := s.begin(ctx, st.ID)
	if err != nil {
		return err
	}
	go func() {
		defer done()
		s.runScript(runCtx, st, a, rt)
	}()
	return nil
}

func (s *Service) runScript(ctx context.Context, st domain.Study, a domain.Audience, rt domain.Agent) {
	wctx := context.WithoutCancel(ctx)
	var script *domain.StudyScript
	system, user := s.scriptPrompt(st, a)
	err := s.askJSON(ctx, rt, askRequest{
		Messages: []domain.Message{{Role: domain.RoleSystem, Content: system}, {Role: domain.RoleUser, Content: user}},
		Label:    "audience-study:script", Title: "write interview script", MaxTurns: 4,
	}, func(raw string) error {
		got, err := parseScript(raw)
		if err != nil {
			return err
		}
		script = got
		return nil
	})
	if ctx.Err() != nil {
		return
	}
	cur, gerr := s.d.Store.GetStudy(wctx, st.ID)
	if gerr != nil || cur.Status != domain.StudyDraft || cur.ScriptState != domain.ScriptGenerating {
		return
	}
	if err != nil {
		log.Warn().Err(err).Str("study_id", st.ID.String()).Msg("audience: writing the interview script failed")
		cur.ScriptState, cur.Error = domain.ScriptFailed, clip(err.Error(), 300)
	} else {
		setScript(&cur, script)
	}
	if serr := s.d.Store.SaveStudy(wctx, cur); serr != nil {
		log.Error().Err(serr).Str("study_id", st.ID.String()).Msg("audience: saving the interview script failed")
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
	if v, ok := boolOf(m, "would_pay_now", "would_pay", "would_buy", "would_act_now", "would_act"); ok {
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
