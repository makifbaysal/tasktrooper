package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChosenVariantsIsTheLatestHumanChoice(t *testing.T) {
	now := time.Now()
	comments := []TaskComment{
		{AuthorType: "user", Content: "Chosen variant: design: export · A", CreatedAt: now.Add(-time.Hour)},
		{AuthorType: "agent", Content: "Chosen variant: design: export · C", CreatedAt: now.Add(time.Hour)},
		{AuthorType: "human", Content: "  Chosen variant: design: export · B\n", CreatedAt: now},
		{AuthorType: "user", Content: "looks good", CreatedAt: now.Add(2 * time.Hour)},
	}
	assert.Equal(t, []string{"design: export · B"}, ChosenVariants(comments), "an agent cannot choose for the human")
	assert.Empty(t, ChosenVariants(nil))
}

func TestChosenVariantsReadsOnlyTheMarkerLine(t *testing.T) {
	now := time.Now()
	comments := []TaskComment{
		{AuthorType: "user", Content: "Chosen variant: design: export · A\n\nKeep B's empty state.", CreatedAt: now},
		{AuthorType: "user", Content: "Chosen variant: \nno title on the marker line", CreatedAt: now.Add(time.Hour)},
	}
	assert.Equal(t, []string{"design: export · A"}, ChosenVariants(comments))
}

func TestChosenVariantsKeepsOneChoicePerScreen(t *testing.T) {
	now := time.Now()
	comments := []TaskComment{
		{AuthorType: "user", Content: "Chosen variant: design: Home · B", CreatedAt: now},
		{AuthorType: "user", Content: "Chosen variant: design: Gallery · A", CreatedAt: now.Add(time.Minute)},
		{AuthorType: "user", Content: "Chosen variant: design: Gallery · B", CreatedAt: now.Add(2 * time.Minute)},
	}
	assert.Equal(t, []string{"design: Gallery · B", "design: Home · B"}, ChosenVariants(comments),
		"choosing the gallery's variant does not undo the home page's")
}

func TestWithoutUnchosenVariantsKeepsTheChoiceAndEverythingElse(t *testing.T) {
	docs := []TaskDocument{
		{Title: "design: export dialog · A"},
		{Title: "design: export dialog · B"},
		{Title: "design: settings · A"},
		{Title: "design: settings · B"},
		{Title: "design: profile · A"},
		{Title: "handoff: export dialog"},
		{Title: "design system: Shop v2"},
	}
	titles := func(in []TaskDocument) []string {
		out := make([]string, 0, len(in))
		for _, d := range in {
			out = append(out, d.Title)
		}
		return out
	}

	assert.Equal(t,
		[]string{"design: export dialog · B", "design: settings · A", "design: settings · B", "design: profile · A", "handoff: export dialog", "design system: Shop v2"},
		titles(WithoutUnchosenVariants(docs, []string{"design: export dialog · B"})))
	assert.Equal(t,
		[]string{"design: export dialog · B", "design: settings · A", "design: profile · A", "handoff: export dialog", "design system: Shop v2"},
		titles(WithoutUnchosenVariants(docs, []string{"design: export dialog · B", "design: settings · A"})),
		"every screen with a choice keeps only its chosen variant")

	assert.Len(t, WithoutUnchosenVariants(docs, nil), 7)
	assert.Len(t, WithoutUnchosenVariants(docs, []string{"design: export dialog · Z"}), 7, "a choice naming no document filters nothing")
}
