package audience

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type personasUserInput struct {
	Name, Product, Context, Locations, CurrentSolutions, Pricing, Topic, Language string
	Count                                                                         int
	SegmentLines, Existing                                                        []string
}

var (
	personasSystemKey = prompt.Define[struct{}]("audience.personas_system", struct{}{})
	personasUserKey   = prompt.Define("audience.personas_user", personasUserInput{
		Name: "Designers", Count: 4, Product: "A kanban for AI agents.", Context: "Go + React.",
		SegmentLines: []string{"- Freelancers: 2"}, Locations: "Europe", Language: "English", Existing: []string{"- Ayse, UX designer"},
	})
)

type segmentSlot struct {
	name, description string
	n                 int
}

// allocateSegments scales the segments' counts to total people. Counts of zero
// share what the others leave; no segments is one anonymous slot.
func allocateSegments(segments []domain.AudienceSegment, total int) []segmentSlot {
	if len(segments) == 0 {
		return []segmentSlot{{n: total}}
	}
	sum, unset := 0, 0
	for _, s := range segments {
		if s.Count > 0 {
			sum += s.Count
		} else {
			unset++
		}
	}
	slots := make([]segmentSlot, len(segments))
	for i, s := range segments {
		slots[i] = segmentSlot{name: s.Name, description: s.Description}
	}
	weights := make([]float64, len(segments))
	weightSum := 0.0
	for i, s := range segments {
		w := float64(s.Count)
		if s.Count <= 0 {
			// an unset segment weighs as much as the average set one
			if sum > 0 && len(segments) > unset {
				w = float64(sum) / float64(len(segments)-unset)
			} else {
				w = 1
			}
		}
		weights[i] = w
		weightSum += w
	}
	assigned := 0
	type frac struct {
		i int
		f float64
	}
	var fracs []frac
	for i, w := range weights {
		exact := w / weightSum * float64(total)
		slots[i].n = int(math.Floor(exact))
		assigned += slots[i].n
		fracs = append(fracs, frac{i, exact - math.Floor(exact)})
	}
	for assigned < total {
		best := 0
		for j := range fracs {
			if fracs[j].f > fracs[best].f {
				best = j
			}
		}
		slots[fracs[best].i].n++
		fracs[best].f = -1
		assigned++
	}
	return slots
}

// Build writes (or rewrites) the audience's people in the background and
// answers at once. It is the call that costs LLM calls — BuildCalls of them.
func (s *Service) Build(ctx context.Context, id uuid.UUID) (domain.Audience, error) {
	a, err := s.d.Store.GetAudience(ctx, id)
	if err != nil {
		return domain.Audience{}, err
	}
	if a.Status == domain.AudienceBuilding || s.running(id) {
		return domain.Audience{}, ErrBusy
	}
	if _, err := NormalizeBrief(a.Brief); err != nil {
		return domain.Audience{}, err
	}
	for _, st := range s.studiesRunning(ctx, id) {
		return domain.Audience{}, invalid("a study (%s) is still running on this audience", st.Title)
	}
	rt, err := s.agentFor(ctx)
	if err != nil {
		return domain.Audience{}, err
	}
	runCtx, done, err := s.begin(ctx, id)
	if err != nil {
		return domain.Audience{}, err
	}
	a.ProductContext = s.projectContext(ctx, a.RepositoryID)
	a.Status = domain.AudienceBuilding
	a.Error = ""
	if err := s.d.Store.SaveAudience(ctx, a); err != nil {
		done()
		return domain.Audience{}, err
	}
	go func() {
		defer done()
		s.runBuild(runCtx, a, rt)
	}()
	return a, nil
}

func (s *Service) studiesRunning(ctx context.Context, audienceID uuid.UUID) []domain.Study {
	studies, err := s.d.Store.ListStudies(ctx, &audienceID)
	if err != nil {
		return nil
	}
	var out []domain.Study
	for _, st := range studies {
		if st.Status == domain.StudyRunning || st.Status == domain.StudySynthesizing {
			out = append(out, st)
		}
	}
	return out
}

// CancelBuild stops a build in progress; the audience keeps the people it had.
func (s *Service) CancelBuild(ctx context.Context, id uuid.UUID) error {
	if _, err := s.d.Store.GetAudience(ctx, id); err != nil {
		return err
	}
	s.cancelRun(id)
	return nil
}

func (s *Service) runBuild(ctx context.Context, a domain.Audience, rt domain.Agent) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("audience_id", a.ID.String()).Msg("audience: build panicked")
			s.finishBuild(ctx, a, nil, fmt.Errorf("building the audience crashed"))
		}
	}()
	personas, err := s.writePersonas(ctx, a, rt)
	s.finishBuild(ctx, a, personas, err)
}

// finishBuild records the outcome. A cancelled build keeps the people the
// audience already had; the outcome is written even though the run's context
// is done.
func (s *Service) finishBuild(ctx context.Context, a domain.Audience, personas []domain.Persona, buildErr error) {
	wctx := context.WithoutCancel(ctx)
	prev, err := s.d.Store.ListPersonas(wctx, a.ID)
	if err != nil {
		log.Warn().Err(err).Msg("audience: reading the previous personas failed")
	}
	switch {
	case buildErr == nil:
		a.Generation++
		for i := range personas {
			personas[i].Generation = a.Generation
		}
		if err := s.d.Store.ReplacePersonas(wctx, a.ID, personas); err != nil {
			buildErr = err
			break
		}
		now := s.d.Now()
		a.Status, a.Error, a.BuiltAt = domain.AudienceReady, "", &now
	}
	if buildErr != nil {
		a.Status = domain.AudienceFailed
		if ctx.Err() != nil {
			// cancelled: back to what it was
			a.Error = "The build was cancelled."
			if len(prev) > 0 {
				a.Status = domain.AudienceReady
				a.Error = ""
			} else {
				a.Status = domain.AudienceDraft
			}
		} else {
			a.Error = clip(buildErr.Error(), 500)
		}
	}
	if err := s.d.Store.SaveAudience(wctx, a); err != nil {
		log.Error().Err(err).Str("audience_id", a.ID.String()).Msg("audience: saving the build outcome failed")
	}
}

// writePersonas writes the audience's people, personasPerCall at a time, each
// call told which segments it covers and who already exists so the next ones
// differ.
func (s *Service) writePersonas(ctx context.Context, a domain.Audience, rt domain.Agent) ([]domain.Persona, error) {
	var plan []string
	descriptions := map[string]string{}
	for _, slot := range allocateSegments(a.Brief.Segments, a.PersonaTarget) {
		descriptions[slot.name] = slot.description
		for i := 0; i < slot.n; i++ {
			plan = append(plan, slot.name)
		}
	}
	var out []domain.Persona
	var existing []string
	names := map[string]bool{}
	for start := 0; start < len(plan); start += personasPerCall {
		end := start + personasPerCall
		if end > len(plan) {
			end = len(plan)
		}
		chunk := plan[start:end]
		batch, err := s.writeBatch(ctx, a, rt, chunk, descriptions, existing)
		if err != nil {
			if len(out) > 0 && ctx.Err() == nil {
				// keep what was written when it turns out to be most of the audience
				break
			}
			return nil, err
		}
		left := tally(chunk)
		added := 0
		for _, p := range batch {
			if added >= len(chunk) {
				break
			}
			if names[strings.ToLower(p.Name)] {
				continue
			}
			names[strings.ToLower(p.Name)] = true
			p.Segment = left.take(p.Segment)
			out = append(out, p)
			existing = append(existing, fmt.Sprintf("- %s, %s", p.Name, p.Role))
			added++
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the model wrote no usable people")
	}
	if len(out) < (a.PersonaTarget+1)/2 {
		return nil, fmt.Errorf("the model wrote only %d of %d people; try again", len(out), a.PersonaTarget)
	}
	return out, nil
}

// segmentTally counts people per segment in order of first appearance.
type segmentTally struct {
	order  []string
	counts map[string]int
}

func tally(names []string) *segmentTally {
	t := &segmentTally{counts: map[string]int{}}
	for _, n := range names {
		if _, ok := t.counts[n]; !ok {
			t.order = append(t.order, n)
		}
		t.counts[n]++
	}
	return t
}

// take hands out a segment: the one the model named when it still has room,
// else the first with room, else the model's own word.
func (t *segmentTally) take(named string) string {
	for _, n := range t.order {
		if strings.EqualFold(n, named) && t.counts[n] > 0 {
			t.counts[n]--
			return n
		}
	}
	for _, n := range t.order {
		if t.counts[n] > 0 {
			t.counts[n]--
			return n
		}
	}
	return named
}

func (s *Service) writeBatch(ctx context.Context, a domain.Audience, rt domain.Agent, chunk []string, descriptions map[string]string, existing []string) ([]domain.Persona, error) {
	in := personasUserInput{
		Name: a.Name, Count: len(chunk), Product: a.Brief.Product, Context: a.ProductContext, Locations: a.Brief.Locations,
		CurrentSolutions: a.Brief.CurrentSolutions, Pricing: a.Brief.Pricing, Topic: a.Brief.Topic, Language: a.Brief.Language,
		Existing: lastN(existing, 40),
	}
	t := tally(chunk)
	for _, name := range t.order {
		if name == "" {
			continue
		}
		line := fmt.Sprintf("- %s: %d", name, t.counts[name])
		if d := descriptions[name]; d != "" {
			line += " (" + d + ")"
		}
		in.SegmentLines = append(in.SegmentLines, line)
	}
	var personas []domain.Persona
	err := s.askJSON(ctx, rt, askRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: prompt.Text(personasSystemKey)},
			{Role: domain.RoleUser, Content: personasUserKey.Render(in)},
		},
		Label: "audience:" + a.Name, Title: "write audience personas", MaxTurns: 6,
	}, func(raw string) error {
		parsed, err := parsePersonas(raw)
		if err != nil {
			return err
		}
		personas = parsed
		return nil
	})
	return personas, err
}

func lastN(l []string, n int) []string {
	if len(l) > n {
		return l[len(l)-n:]
	}
	return l
}

// parsePersonas reads {"personas":[...]} and drops what is not a person.
func parsePersonas(raw string) ([]domain.Persona, error) {
	var obj map[string]any
	if err := decodeObject(raw, &obj); err != nil {
		return nil, err
	}
	list := objectsOf(obj, "personas", "people", "audience")
	if len(list) == 0 {
		return nil, fmt.Errorf("no \"personas\" list")
	}
	seen := map[string]bool{}
	var out []domain.Persona
	for _, m := range list {
		name := clip(strOf(m, "name", "full_name"), 80)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		age, _ := intOf(m, "age")
		if age < 0 || age > 120 {
			age = 0
		}
		out = append(out, domain.Persona{Name: name, Segment: clip(strOf(m, "segment"), 100), PersonaProfile: domain.PersonaProfile{
			Age: age, Role: clip(strOf(m, "role", "job", "occupation"), 120), Location: clip(strOf(m, "location", "city"), 120),
			Bio: clip(strOf(m, "bio", "background"), 600), Goals: listOf(m, 4, 200, "goals"), Pains: listOf(m, 4, 200, "pains", "pain_points"),
			CurrentSolution: clip(strOf(m, "current_solution", "currentSolution"), 300),
			Budget:          oneOf(strOf(m, "budget"), "tight", "moderate", "comfortable"),
			TechSavviness:   oneOf(strOf(m, "tech_savviness", "techSavviness"), "low", "medium", "high"),
			Personality:     clip(strOf(m, "personality"), 300), Quote: clip(strOf(m, "quote"), 300),
		}})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("none of the people had a name")
	}
	return out, nil
}

func oneOf(v string, allowed ...string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return ""
}
