package evolution

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// How many earlier lessons a reflection reads, newest first, and how far back it looks for them.
const (
	lessonsInEvidence   = 5
	lessonsScanReflects = 20
)

type lessonChange struct {
	Kind, Action, Name, Dimension, Verdict, Reason string
	Adopted                                        bool
}

type lessonNote struct {
	Label  string
	Golden bool
	Passed bool
	Score  int
	Reason string
}

type lessonCase struct {
	Kind, Title string
	Measured    bool
	Notes       []lessonNote
}

type lessonUserInput struct {
	AgentName string
	Changes   []lessonChange
	Variants  []domain.ReflectionProbeVariant
	Adopted   string
	Cases     []lessonCase
}

type lessonOutput struct {
	Entry      bool   `json:"entry"`
	Cue        string `json:"cue"`
	Lesson     string `json:"lesson"`
	Evidence   string `json:"evidence"`
	Confidence string `json:"confidence"`
}

var (
	lessonSystemKey = prompt.Define[struct{}]("evolution.lesson_system", struct{}{})
	lessonUserKey   = prompt.Define("evolution.lesson_user", lessonUserInput{
		AgentName: "backend-developer",
		Changes: []lessonChange{{
			Kind: "skill", Action: "create", Name: "db-migrations", Dimension: "procedure",
			Verdict: "helped", Reason: "two revisions asked for a down migration", Adopted: true,
		}},
		Variants: []domain.ReflectionProbeVariant{{Label: "all", Changes: 1, Wins: 2, Ties: 1, Accepted: true}},
		Adopted:  "all",
		Cases: []lessonCase{{
			Kind: "task", Title: "ORD-12 Paginate orders", Measured: true,
			Notes: []lessonNote{{Label: "base", Score: 5, Reason: "no rollback"}, {Label: "all", Score: 8, Reason: "adds a down migration"}},
		}},
	})
)

// Harnessing-level recursion: after a change set is probed, one call turns the record into at most one transferable lesson, and later reflections read the library of them. Verdicts are not re-graded here; they come from the probe.
func (s *Service) distillLesson(ctx context.Context, agentRec domain.Agent, plan []plannedChange, out probeOutcome) *domain.ReflectionLesson {
	if out.Unmeasured || len(plan) == 0 {
		return nil
	}
	in := lessonUserInput{AgentName: agentRec.Name, Variants: out.Variants, Adopted: out.Adopted}
	for _, c := range plan {
		in.Changes = append(in.Changes, lessonChange{
			Kind: c.Kind, Action: c.Action, Name: c.Name, Dimension: c.Dimension,
			Verdict: orDefault(out.Verdicts[c.Index], domain.ProbeVerdictNoEffect), Reason: c.Reason,
			Adopted: out.AdoptedSet[c.Index],
		})
	}
	for _, j := range out.Judgements {
		lc := lessonCase{Kind: j.Kind, Title: j.Title, Measured: j.Measured}
		if j.Measured {
			labels := []string{probeBaseLabel}
			for _, v := range out.Variants {
				labels = append(labels, v.Label)
			}
			for _, label := range labels {
				note := lessonNote{Label: label, Golden: j.Kind == domain.ProbeCaseGolden}
				if note.Golden {
					note.Passed = j.Passed[label]
				} else if score, ok := j.Scores[label]; ok {
					note.Score, note.Reason = score, truncate(j.Reasons[label], 300)
				} else {
					continue
				}
				lc.Notes = append(lc.Notes, note)
			}
		}
		in.Cases = append(in.Cases, lc)
	}

	model, provider := s.judgeRouting(agentRec)
	raw, err := s.ask(ctx, askRequest{
		Model: model, Provider: provider,
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: prompt.Text(lessonSystemKey)},
			{Role: domain.RoleUser, Content: lessonUserKey.Render(in)},
		},
		Schema: &evolutionLessonSchemaKey,
		Label:  "lesson:" + agentRec.Name, Title: "self-evolution lesson",
	})
	if err != nil {
		log.Warn().Err(err).Str("agent", agentRec.Name).Msg("evolution lesson call failed; no lesson recorded")
		return nil
	}
	var got lessonOutput
	if err := parsePromotionJSON(raw, &got); err != nil {
		log.Warn().Err(err).Str("agent", agentRec.Name).Msg("evolution lesson unparseable; no lesson recorded")
		return nil
	}
	if !got.Entry || strings.TrimSpace(got.Cue) == "" || strings.TrimSpace(got.Lesson) == "" {
		return nil
	}
	return &domain.ReflectionLesson{
		Cue: strings.TrimSpace(got.Cue), Lesson: strings.TrimSpace(got.Lesson),
		Evidence: strings.TrimSpace(got.Evidence), Confidence: strings.TrimSpace(got.Confidence),
	}
}

type evidenceLesson struct {
	Cue, Lesson, Evidence, Confidence, Date string
}

type evidenceLessonsInput struct{ Lessons []evidenceLesson }

var evidenceLessonsKey = prompt.Define("evolution.evidence_lessons", evidenceLessonsInput{
	Lessons: []evidenceLesson{{
		Cue: "a reviewer keeps asking for the same missing step", Lesson: "move that step into the skill the run loads for this work, not into a rule",
		Evidence: "skill-only variant won 2 of 2 rehearsals", Confidence: "medium", Date: "2026-09-01",
	}},
})

func (s *Service) earlierLessons(ctx context.Context, agentID uuid.UUID) []evidenceLesson {
	reflections, err := s.store.ListReflections(ctx, agentID, lessonsScanReflects)
	if err != nil {
		return nil
	}
	var out []evidenceLesson
	for _, r := range reflections {
		if r.Decision == nil || r.Decision.Lesson == nil {
			continue
		}
		l := r.Decision.Lesson
		out = append(out, evidenceLesson{
			Cue: l.Cue, Lesson: l.Lesson, Evidence: truncate(l.Evidence, 300), Confidence: orDefault(l.Confidence, "low"),
			Date: r.CreatedAt.Format("2006-01-02"),
		})
		if len(out) >= lessonsInEvidence {
			break
		}
	}
	return out
}

func evidenceLessonsSection(lessons []evidenceLesson) string {
	if len(lessons) == 0 {
		return ""
	}
	return evidenceLessonsKey.Render(evidenceLessonsInput{Lessons: lessons})
}
