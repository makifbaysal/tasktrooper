package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChosenVariantIsTheLatestHumanChoice(t *testing.T) {
	now := time.Now()
	comments := []TaskComment{
		{AuthorType: "user", Content: "Chosen variant: design: export · A", CreatedAt: now.Add(-time.Hour)},
		{AuthorType: "agent", Content: "Chosen variant: design: export · C", CreatedAt: now.Add(time.Hour)},
		{AuthorType: "human", Content: "  Chosen variant: design: export · B\n", CreatedAt: now},
		{AuthorType: "user", Content: "looks good", CreatedAt: now.Add(2 * time.Hour)},
	}
	assert.Equal(t, "design: export · B", ChosenVariant(comments), "an agent cannot choose for the human")
	assert.Empty(t, ChosenVariant(nil))
}

func TestChosenVariantReadsOnlyTheMarkerLine(t *testing.T) {
	now := time.Now()
	comments := []TaskComment{
		{AuthorType: "user", Content: "Chosen variant: design: export · A\n\nKeep B's empty state.", CreatedAt: now},
		{AuthorType: "user", Content: "Chosen variant: \nno title on the marker line", CreatedAt: now.Add(time.Hour)},
	}
	assert.Equal(t, "design: export · A", ChosenVariant(comments))
}

func TestWithoutUnchosenVariantsKeepsTheChoiceAndEverythingElse(t *testing.T) {
	docs := []TaskDocument{
		{Title: "design: export dialog · A"},
		{Title: "design: export dialog · B"},
		{Title: "design: settings · A"},
		{Title: "handoff: export dialog"},
		{Title: "design system: Shop v2"},
	}
	got := WithoutUnchosenVariants(docs, "design: export dialog · B")
	titles := make([]string, 0, len(got))
	for _, d := range got {
		titles = append(titles, d.Title)
	}
	assert.Equal(t, []string{"design: export dialog · B", "design: settings · A", "handoff: export dialog", "design system: Shop v2"}, titles)

	assert.Len(t, WithoutUnchosenVariants(docs, ""), 5)
	assert.Len(t, WithoutUnchosenVariants(docs, "design: export dialog · Z"), 5, "a choice naming no document filters nothing")
}
