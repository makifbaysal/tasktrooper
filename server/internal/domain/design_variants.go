package domain

import (
	"sort"
	"strings"
)

// ChosenVariantMarker starts the comment the review page posts when the human
// picks one of a screen's variants: "Chosen variant: design: export · B".
const ChosenVariantMarker = "Chosen variant: "

const (
	designDocPrefix  = "design:"
	variantSeparator = " · "
)

// ChosenVariants is the document title the human chose last for each screen,
// read from the design task's comments, in title order; empty when nobody
// chose. A choice is per screen — picking the gallery's variant does not undo
// the home page's. Only the marker's own line names the document: the review
// page puts the human's note on the choice on the lines after it.
func ChosenVariants(comments []TaskComment) []string {
	type choice struct {
		title string
		at    int64
	}
	latest := map[string]choice{}
	for _, c := range comments {
		if c.AuthorType != "human" && c.AuthorType != "user" {
			continue
		}
		content := strings.TrimSpace(c.Content)
		if !strings.HasPrefix(content, ChosenVariantMarker) {
			continue
		}
		title, _, _ := strings.Cut(strings.TrimPrefix(content, ChosenVariantMarker), "\n")
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		key := title
		if screen, ok := designScreen(title); ok {
			key = screen
		}
		ts := c.CreatedAt.UnixNano()
		if prev, ok := latest[key]; !ok || ts >= prev.at {
			latest[key] = choice{title: title, at: ts}
		}
	}
	out := make([]string, 0, len(latest))
	for _, c := range latest {
		out = append(out, c.title)
	}
	sort.Strings(out)
	return out
}

// designScreen splits a mockup title "design: <screen> · <variant>" into its
// screen; ok is false for any other document.
func designScreen(title string) (string, bool) {
	t := strings.TrimSpace(title)
	if !strings.HasPrefix(strings.ToLower(t), designDocPrefix) {
		return "", false
	}
	i := strings.LastIndex(t, variantSeparator)
	if i < 0 {
		return "", false
	}
	return strings.TrimSpace(t[len(designDocPrefix):i]), true
}

// WithoutUnchosenVariants drops each chosen screen's other variants, so what
// is built from an approved design is the variant the human picked for every
// screen they chose on. Unchosen screens' mockups, hand-off specs and reports
// stay; a choice naming no document filters nothing.
func WithoutUnchosenVariants(docs []TaskDocument, chosen []string) []TaskDocument {
	titles := make(map[string]bool, len(docs))
	for _, d := range docs {
		titles[strings.TrimSpace(d.Title)] = true
	}
	keep := map[string]string{}
	for _, title := range chosen {
		screen, ok := designScreen(title)
		if ok && titles[title] {
			keep[screen] = title
		}
	}
	if len(keep) == 0 {
		return docs
	}
	out := make([]TaskDocument, 0, len(docs))
	for _, d := range docs {
		if s, ok := designScreen(d.Title); ok {
			if want, chosenScreen := keep[s]; chosenScreen && strings.TrimSpace(d.Title) != want {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}
