package audience

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	maxPageLen         = 12000
	minVariants        = 2
	maxVariants        = 5
	maxVariantLen      = 300
	maxIntentLen       = 200
	maxScriptSections  = 8
	maxScriptQuestions = 12
	maxSectionTitleLen = 100

	defaultIntent = "evaluate what it offers"
)

// PageReader reads a page's text from its URL (the guarded fetch the agents'
// fetch_url tool uses).
type PageReader interface {
	Read(ctx context.Context, url string) (string, error)
}

// StudyCallsFor is the model calls a study of n people takes, the script not
// counted (see Study.ScriptCalls): every kind interviews each person in one
// call and writes the report in one more.
func StudyCallsFor(_ domain.StudyKind, n int) int { return StudyCalls(n) }

func pageSource(p domain.PageInput) string {
	if p.URL != "" {
		return p.URL
	}
	return ""
}

// normalizeInputs checks the inputs a kind needs and reads pages that were
// given as a URL. Inputs a kind does not use are dropped.
func (s *Service) normalizeInputs(ctx context.Context, kind domain.StudyKind, in domain.StudyInputs) (domain.StudyInputs, error) {
	out := domain.StudyInputs{}
	switch kind {
	case domain.KindLandingPageTest, domain.KindABTest:
		want := 1
		if kind == domain.KindABTest {
			want = 2
		}
		if len(in.Pages) != want {
			if kind == domain.KindABTest {
				return out, invalid("an A/B test needs exactly two pages (A and B)")
			}
			return out, invalid("a landing page test needs exactly one page")
		}
		out.Intent = clip(strings.TrimSpace(in.Intent), maxIntentLen)
		if out.Intent == "" {
			out.Intent = defaultIntent
		}
		for i, p := range in.Pages {
			page, err := s.resolvePage(ctx, i, p)
			if err != nil {
				return out, err
			}
			out.Pages = append(out.Pages, page)
		}
	case domain.KindMessageTesting:
		seen := map[string]bool{}
		for _, v := range in.Variants {
			v = clip(strings.TrimSpace(v), maxVariantLen)
			if v == "" || seen[strings.ToLower(v)] {
				continue
			}
			seen[strings.ToLower(v)] = true
			out.Variants = append(out.Variants, v)
		}
		if len(out.Variants) < minVariants || len(out.Variants) > maxVariants {
			return out, invalid("message testing needs %d to %d different lines", minVariants, maxVariants)
		}
	}
	return out, nil
}

func (s *Service) resolvePage(ctx context.Context, i int, p domain.PageInput) (domain.PageInput, error) {
	p.URL = strings.TrimSpace(p.URL)
	p.Copy = clip(strings.TrimSpace(p.Copy), maxPageLen)
	label := fmt.Sprintf("page %d", i+1)
	if p.URL == "" && p.Copy == "" {
		return p, invalid("%s needs a URL or its text", label)
	}
	if p.URL != "" {
		u, err := url.Parse(p.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return p, invalid("%s: the URL must start with http:// or https://", label)
		}
	}
	if p.Copy != "" {
		return p, nil
	}
	if s.d.Pages == nil {
		return p, invalid("%s: pages cannot be read from a URL here; paste its text", label)
	}
	text, err := s.d.Pages.Read(ctx, p.URL)
	if err != nil {
		return p, invalid("%s: that URL could not be read; paste the page text instead", label)
	}
	p.Copy = clip(strings.TrimSpace(text), maxPageLen)
	if len([]rune(p.Copy)) < 40 {
		p.Copy = ""
		return p, invalid("%s: that URL had no readable text (it may need JavaScript); paste the page text instead", label)
	}
	return p, nil
}

// ---- script

func scriptFromQuestions(qs []string) *domain.StudyScript {
	if len(qs) == 0 {
		return &domain.StudyScript{Sections: []domain.ScriptSection{}}
	}
	return &domain.StudyScript{Sections: []domain.ScriptSection{{Questions: qs}}}
}

// normalizeScript trims, drops what is empty and bounds the script.
func normalizeScript(sc *domain.StudyScript) (*domain.StudyScript, error) {
	out := &domain.StudyScript{Sections: []domain.ScriptSection{}}
	if sc == nil {
		return out, nil
	}
	total := 0
	for _, sec := range sc.Sections {
		var qs []string
		for _, q := range sec.Questions {
			if q = clip(strings.TrimSpace(q), maxQuestionLen); q != "" {
				qs = append(qs, q)
			}
		}
		if len(qs) == 0 {
			continue
		}
		total += len(qs)
		out.Sections = append(out.Sections, domain.ScriptSection{Title: clip(strings.TrimSpace(sec.Title), maxSectionTitleLen), Questions: qs})
	}
	if len(out.Sections) > maxScriptSections {
		return nil, invalid("at most %d script sections", maxScriptSections)
	}
	if total > maxScriptQuestions {
		return nil, invalid("at most %d script questions", maxScriptQuestions)
	}
	return out, nil
}

// parseScript reads the script a model wrote.
func parseScript(raw string) (*domain.StudyScript, error) {
	var m map[string]any
	if err := decodeObject(raw, &m); err != nil {
		return nil, err
	}
	sc := &domain.StudyScript{}
	for _, sec := range objectsOf(m, "sections", "groups") {
		sc.Sections = append(sc.Sections, domain.ScriptSection{
			Title: strOf(sec, "title", "name"), Questions: listOf(sec, maxScriptQuestions, maxQuestionLen, "questions"),
		})
	}
	if len(sc.Sections) == 0 {
		if qs := listOf(m, maxScriptQuestions, maxQuestionLen, "questions"); len(qs) > 0 {
			sc.Sections = []domain.ScriptSection{{Questions: qs}}
		}
	}
	norm := &domain.StudyScript{Sections: []domain.ScriptSection{}}
	total := 0
	for _, sec := range sc.Sections {
		if total >= maxScriptQuestions || len(norm.Sections) >= maxScriptSections {
			break
		}
		qs := sec.Questions
		if room := maxScriptQuestions - total; len(qs) > room {
			qs = qs[:room]
		}
		if n, err := normalizeScript(&domain.StudyScript{Sections: []domain.ScriptSection{{Title: sec.Title, Questions: qs}}}); err == nil && len(n.Sections) == 1 {
			norm.Sections = append(norm.Sections, n.Sections[0])
			total += len(n.Sections[0].Questions)
		}
	}
	if total == 0 {
		return nil, fmt.Errorf(`"sections" has no questions`)
	}
	return norm, nil
}

// ---- presentation order

// abSwapped: whether this person sees page B first. Alternating by the
// person's place in the audience balances the order across people.
func abSwapped(ordinal int) bool { return ordinal%2 == 0 && ordinal > 0 }

// rotation is the order candidate lines are shown to one person: presented
// position i shows line perm[i] (0-based). Rotating by the person's place
// gives every line every position about equally often.
func rotation(n, ordinal int) []int {
	perm := make([]int, n)
	if ordinal < 0 {
		ordinal = 0
	}
	for i := range perm {
		perm[i] = (i + ordinal) % n
	}
	return perm
}
