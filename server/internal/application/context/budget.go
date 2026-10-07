package context

import (
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Budget struct {
	MaxTokens          int
	ReserveOutput      int
	SummarizeThreshold int
	KeepRecentMessages int
}

// Apply trims messages to the budget. A budget with no MaxTokens is no budget
// (context.max_tokens left to the model default), not a one-token window.
func (b Budget) Apply(messages []domain.Message) []domain.Message {
	limit := b.TokenLimit()
	if b.MaxTokens <= 0 || CountTokens(messages) <= limit {
		out := make([]domain.Message, len(messages))
		copy(out, messages)
		return out
	}

	working := make([]domain.Message, len(messages))
	copy(working, messages)

	for CountTokens(working) > limit {
		if idx, ok := b.findRemovableTool(working); ok {
			working = append(working[:idx], working[idx+1:]...)
			continue
		}
		if idx, ok := b.findRemovableMessage(working); ok {
			working = append(working[:idx], working[idx+1:]...)
			continue
		}
		// A protected turn can still exceed the limit on pictures alone; shedding the oldest image keeps the words, which the model cannot guess.
		if dropOldestImage(working) {
			continue
		}
		break
	}
	return working
}

// dropOldestImage sheds the single oldest image while keeping the message and its text intact. The Images slice is cloned because Apply's messages are a shallow copy — writing through the shared backing array would strip the caller's own history.
func dropOldestImage(messages []domain.Message) bool {
	for i := range messages {
		if len(messages[i].Images) == 0 {
			continue
		}
		remaining := make([]domain.ToolResultImage, 0, len(messages[i].Images)-1)
		remaining = append(remaining, messages[i].Images[1:]...)
		messages[i].Images = remaining
		return true
	}
	return false
}

// TokenLimit is the whole window minus the room the answer needs; exported so callers that trim before handing a history over ask the same question Apply asks.
func (b Budget) TokenLimit() int {
	limit := b.MaxTokens - b.ReserveOutput
	if limit < 1 {
		return 1
	}
	return limit
}

func (b Budget) findRemovableTool(messages []domain.Message) (int, bool) {
	lastUser := lastUserIndex(messages)
	recent := recentIndexSet(len(messages), b.KeepRecentMessages)
	for i, m := range messages {
		if m.Role != domain.RoleTool {
			continue
		}
		if i == lastUser {
			continue
		}
		if _, ok := recent[i]; ok {
			continue
		}
		return i, true
	}
	return 0, false
}

func (b Budget) findRemovableMessage(messages []domain.Message) (int, bool) {
	lastUser := lastUserIndex(messages)
	recent := recentIndexSet(len(messages), b.KeepRecentMessages)
	for i, m := range messages {
		if m.Role == domain.RoleSystem {
			continue
		}
		if i == lastUser {
			continue
		}
		if _, ok := recent[i]; ok {
			continue
		}
		return i, true
	}
	return 0, false
}

func lastUserIndex(messages []domain.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			return i
		}
	}
	return -1
}

func recentIndexSet(n, keepRecent int) map[int]struct{} {
	if keepRecent <= 0 || n == 0 {
		return map[int]struct{}{}
	}
	start := 0
	if n > keepRecent {
		start = n - keepRecent
	}
	set := make(map[int]struct{}, n-start)
	for i := start; i < n; i++ {
		set[i] = struct{}{}
	}
	return set
}
