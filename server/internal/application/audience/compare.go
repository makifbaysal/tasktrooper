package audience

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// StudyRef names one side of a comparison.
type StudyRef struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Respondents int        `json:"respondents"`
	Product     string     `json:"-"`
}

// MetricChange is one number before and after; Delta is after minus before.
type MetricChange struct {
	Key    string  `json:"key"`
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Delta  float64 `json:"delta"`
}

type SegmentChange struct {
	Name         string  `json:"name"`
	BeforeIntent float64 `json:"before_intent"`
	AfterIntent  float64 `json:"after_intent"`
	BeforeYesPct float64 `json:"before_yes_pct"`
	AfterYesPct  float64 `json:"after_yes_pct"`
}

// ThemeChange follows one recurring thing across the two reports. Status is
// resolved (only before), new (only after), down, up or same.
type ThemeChange struct {
	Label       string  `json:"label"`
	BeforeCount int     `json:"before_count"`
	AfterCount  int     `json:"after_count"`
	BeforePct   float64 `json:"before_pct"`
	AfterPct    float64 `json:"after_pct"`
	Status      string  `json:"status"`
}

// PersonChange is one person who answered both studies.
type PersonChange struct {
	PersonaID     uuid.UUID `json:"persona_id"`
	Name          string    `json:"name"`
	Segment       string    `json:"segment,omitempty"`
	BeforeIntent  int       `json:"before_intent"`
	AfterIntent   int       `json:"after_intent"`
	BeforeVerdict string    `json:"before_verdict"`
	AfterVerdict  string    `json:"after_verdict"`
	BeforeQuote   string    `json:"before_quote,omitempty"`
	AfterQuote    string    `json:"after_quote,omitempty"`
}

// StudyComparison is what changed between two reports of the same kind on the
// same audience. Every number is computed from the two reports and their
// answers; no model is asked.
type StudyComparison struct {
	Before  StudyRef         `json:"before"`
	After   StudyRef         `json:"after"`
	Kind    domain.StudyKind `json:"kind"`
	Changes string           `json:"changes,omitempty"`
	// Paired is how many people answered both studies; Improved, Worse and
	// Same count them by the change of their own intent score.
	Paired       int             `json:"paired"`
	Improved     int             `json:"improved"`
	Worse        int             `json:"worse"`
	Same         int             `json:"same"`
	Verdict      MetricChange    `json:"verdict"`
	VerdictLabel [2]string       `json:"verdict_labels"`
	Metrics      []MetricChange  `json:"metrics"`
	Segments     []SegmentChange `json:"segments"`
	Objections   []ThemeChange   `json:"objections"`
	Needs        []ThemeChange   `json:"needs"`
	Alternatives []ThemeChange   `json:"alternatives"`
	Strengths    []ThemeChange   `json:"strengths"`
	People       []PersonChange  `json:"people"`
}

// themeShift is the change in share of respondents (in points) under which a
// theme counts as unchanged.
const themeShift = 10.0

func metric(key string, before, after float64) MetricChange {
	return MetricChange{Key: key, Before: before, After: after, Delta: round(after-before, 1)}
}

// CompareStudies compares two finished studies of one audience and one kind.
func (s *Service) CompareStudies(ctx context.Context, beforeID, afterID uuid.UUID) (StudyComparison, error) {
	if beforeID == afterID {
		return StudyComparison{}, invalid("pick two different studies to compare")
	}
	before, err := s.d.Store.GetStudy(ctx, beforeID)
	if err != nil {
		return StudyComparison{}, err
	}
	after, err := s.d.Store.GetStudy(ctx, afterID)
	if err != nil {
		return StudyComparison{}, err
	}
	switch {
	case before.AudienceID != after.AudienceID:
		return StudyComparison{}, invalid("the two studies ran on different audiences; compare studies of one audience")
	case before.Kind != after.Kind:
		return StudyComparison{}, invalid("the two studies are of different kinds; compare studies of one kind")
	case before.Report == nil || after.Report == nil:
		return StudyComparison{}, invalid("both studies need a finished report to compare")
	}
	beforeAnswers, err := s.d.Store.ListAnswers(ctx, beforeID)
	if err != nil {
		return StudyComparison{}, err
	}
	afterAnswers, err := s.d.Store.ListAnswers(ctx, afterID)
	if err != nil {
		return StudyComparison{}, err
	}
	return Compare(before, after, beforeAnswers, afterAnswers), nil
}

// Compare is CompareStudies on data already read.
func Compare(before, after domain.Study, beforeAnswers, afterAnswers []domain.StudyAnswer) StudyComparison {
	b, a := before.Report, after.Report
	c := StudyComparison{
		Before:  StudyRef{ID: before.ID, Title: before.Title, FinishedAt: before.FinishedAt, Respondents: b.Metrics.Respondents},
		After:   StudyRef{ID: after.ID, Title: after.Title, FinishedAt: after.FinishedAt, Respondents: a.Metrics.Respondents},
		Kind:    after.Kind,
		Changes: after.Changes,
		Verdict: metric("verdict_score", b.Verdict.Score, a.Verdict.Score),
	}
	c.VerdictLabel = [2]string{b.Verdict.Label, a.Verdict.Label}

	if after.Kind == domain.KindUserInterviews {
		bm, am := b.Metrics, a.Metrics
		c.Metrics = []MetricChange{
			metric("intent_mean", round(bm.IntentMean, 1), round(am.IntentMean, 1)),
			metric("yes_pct", pct(bm.Yes, bm.Respondents), pct(am.Yes, am.Respondents)),
			metric("maybe_pct", pct(bm.Maybe, bm.Respondents), pct(am.Maybe, am.Respondents)),
			metric("no_pct", pct(bm.No, bm.Respondents), pct(am.No, am.Respondents)),
			metric("would_pay_now_pct", round(bm.WouldPayNowPct, 1), round(am.WouldPayNowPct, 1)),
		}
		if bm.Price.Answered > 0 || am.Price.Answered > 0 {
			c.Metrics = append(c.Metrics, metric("median_price", bm.Price.Median, am.Price.Median))
		}
	}
	if b.Landing != nil && a.Landing != nil {
		c.Metrics = append(c.Metrics, metric("clarity_mean", round(b.Landing.ClarityMean, 1), round(a.Landing.ClarityMean, 1)))
	}

	c.Segments = compareSegments(b.Segments, a.Segments)
	c.Objections = compareThemes(b.Objections, a.Objections)
	c.Needs = compareThemes(b.Needs, a.Needs)
	c.Alternatives = compareThemes(b.Alternatives, a.Alternatives)
	c.Strengths = compareThemes(b.Strengths, a.Strengths)
	if after.Kind == domain.KindUserInterviews {
		c.People = comparePeople(beforeAnswers, afterAnswers)
		for _, p := range c.People {
			switch {
			case p.AfterIntent > p.BeforeIntent:
				c.Improved++
			case p.AfterIntent < p.BeforeIntent:
				c.Worse++
			default:
				c.Same++
			}
		}
		c.Paired = len(c.People)
	}
	return c
}

func compareSegments(before, after []domain.SegmentReport) []SegmentChange {
	byName := map[string]domain.SegmentReport{}
	for _, s := range before {
		byName[strings.ToLower(s.Name)] = s
	}
	var out []SegmentChange
	for _, a := range after {
		b, ok := byName[strings.ToLower(a.Name)]
		if !ok {
			continue
		}
		out = append(out, SegmentChange{Name: a.Name,
			BeforeIntent: round(b.IntentMean, 1), AfterIntent: round(a.IntentMean, 1),
			BeforeYesPct: round(b.YesPct, 1), AfterYesPct: round(a.YesPct, 1)})
	}
	return out
}

func comparePeople(before, after []domain.StudyAnswer) []PersonChange {
	prior := map[uuid.UUID]domain.StudyAnswer{}
	for _, a := range before {
		if a.Status == "ok" && a.PersonaID != nil {
			prior[*a.PersonaID] = a
		}
	}
	var out []PersonChange
	for _, a := range after {
		if a.Status != "ok" || a.PersonaID == nil {
			continue
		}
		b, ok := prior[*a.PersonaID]
		if !ok {
			continue
		}
		out = append(out, PersonChange{
			PersonaID: *a.PersonaID, Name: a.Persona.Name, Segment: a.Persona.Segment,
			BeforeIntent: b.Answers.IntentScore, AfterIntent: a.Answers.IntentScore,
			BeforeVerdict: b.Answers.Verdict, AfterVerdict: a.Answers.Verdict,
			BeforeQuote: b.Answers.Quote, AfterQuote: a.Answers.Quote,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := abs(out[i].AfterIntent-out[i].BeforeIntent), abs(out[j].AfterIntent-out[j].BeforeIntent)
		return di > dj
	})
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func words(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) > 2 {
			set[w] = true
		}
	}
	return set
}

func similar(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

// themeMatch is how alike two theme labels must be (shared words over all
// words) to count as the same thing said in both reports.
const themeMatch = 0.5

// compareThemes pairs each theme of the new report with the most alike
// unclaimed theme of the old one; what is left over on either side is new or
// resolved.
func compareThemes(before, after []domain.Theme) []ThemeChange {
	claimed := make([]bool, len(before))
	var out []ThemeChange
	for _, a := range after {
		aw := words(a.Label)
		best, bestScore := -1, 0.0
		for i, b := range before {
			if claimed[i] {
				continue
			}
			if score := similar(aw, words(b.Label)); score >= themeMatch && score > bestScore {
				best, bestScore = i, score
			}
		}
		change := ThemeChange{Label: a.Label, AfterCount: a.Count, AfterPct: round(a.Pct, 1), Status: "new"}
		if best >= 0 {
			claimed[best] = true
			b := before[best]
			change.BeforeCount, change.BeforePct = b.Count, round(b.Pct, 1)
			switch d := a.Pct - b.Pct; {
			case d >= themeShift:
				change.Status = "up"
			case d <= -themeShift:
				change.Status = "down"
			default:
				change.Status = "same"
			}
		}
		out = append(out, change)
	}
	for i, b := range before {
		if !claimed[i] {
			out = append(out, ThemeChange{Label: b.Label, BeforeCount: b.Count, BeforePct: round(b.Pct, 1), Status: "resolved"})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return math.Max(out[i].BeforePct, out[i].AfterPct) > math.Max(out[j].BeforePct, out[j].AfterPct)
	})
	return out
}
