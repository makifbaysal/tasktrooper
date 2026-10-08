package domain

import (
	"strings"
)

// ChosenVariantMarker starts the comment the review page posts when the human
// picks one of a screen's variants: "Chosen variant: design: export · B".
const ChosenVariantMarker = "Chosen variant: "

const (
	designDocPrefix  = "design:"
	variantSeparator = " · "
)

// ChosenVariant is the document title the human chose last, read from the
// design task's comments; "" when nobody chose.
func ChosenVariant(comments []TaskComment) string {
	var chosen string
	var at int64
	for _, c := range comments {
		if c.AuthorType != "human" && c.AuthorType != "user" {
			continue
		}
		content := strings.TrimSpace(c.Content)
		if !strings.HasPrefix(content, ChosenVariantMarker) {
			continue
		}
		if ts := c.CreatedAt.UnixNano(); chosen == "" || ts >= at {
			chosen, at = strings.TrimSpace(strings.TrimPrefix(content, ChosenVariantMarker)), ts
		}
	}
	return chosen
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

// WithoutUnchosenVariants drops the chosen screen's other variants, so what
// is built from an approved design is the variant the human picked. Other
// screens' mockups, hand-off specs and reports stay.
func WithoutUnchosenVariants(docs []TaskDocument, chosen string) []TaskDocument {
	screen, ok := designScreen(chosen)
	if !ok {
		return docs
	}
	present := false
	for _, d := range docs {
		if strings.TrimSpace(d.Title) == chosen {
			present = true
			break
		}
	}
	if !present {
		return docs
	}
	out := make([]TaskDocument, 0, len(docs))
	for _, d := range docs {
		if s, ok := designScreen(d.Title); ok && s == screen && strings.TrimSpace(d.Title) != chosen {
			continue
		}
		out = append(out, d)
	}
	return out
}
