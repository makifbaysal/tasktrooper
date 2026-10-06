package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultBoardTransitionsAreWellFormed(t *testing.T) {
	stock := map[string]bool{string(TaskColumnBacklog): true}
	for _, c := range BoardColumns {
		stock[string(c)] = true
	}
	seen := map[BoardTransition]bool{}
	for _, tr := range DefaultBoardTransitions() {
		assert.True(t, stock[tr.From], "unknown source %q", tr.From)
		assert.True(t, stock[tr.To], "unknown target %q", tr.To)
		assert.NotEqual(t, tr.From, tr.To)
		assert.False(t, seen[tr], "duplicate %v", tr)
		seen[tr] = true
		assert.NotEqual(t, "blocked", tr.From, "a park must be able to return to any column")
	}
	for _, tr := range RequiredBoardTransitions() {
		assert.True(t, seen[tr], "required %v missing from the defaults", tr)
	}
}

// The moves the automation makes on its own; dropping one stalls every task
// in that column (see ValidateTransition's callers).
func TestDefaultBoardTransitionsCoverThePipeline(t *testing.T) {
	seen := map[BoardTransition]bool{}
	for _, tr := range DefaultBoardTransitions() {
		seen[tr] = true
	}
	for _, tr := range []BoardTransition{
		{From: "code_review", To: "ready_for_qa"},
		{From: "ready_for_qa", To: "in_qa"},
		{From: "in_progress", To: "code_review"},
		{From: "need_revision", To: "code_review"},
		{From: "done", To: "released"},
		{From: "released", To: "need_revision"},
	} {
		assert.True(t, seen[tr], "%v", tr)
	}
	assert.False(t, seen[BoardTransition{From: "in_progress", To: "ready_for_qa"}], "code review must not be skippable")
}
