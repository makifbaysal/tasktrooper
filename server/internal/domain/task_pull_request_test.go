package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// The number is what every PR API call is keyed by, and it is parsed out of a URL
// GitHub wrote — so the failure cases matter as much as the happy one: a URL that
// does not parse must report so (the caller then stores the link alone) instead of
// yielding a plausible-looking wrong number.
func TestParsePullRequestNumber(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   int
		wantOK bool
	}{
		{"html url", "https://github.com/acme/widget/pull/123", 123, true},
		{"api url uses the plural", "https://api.github.com/repos/acme/widget/pulls/7", 7, true},
		{"trailing sub-path", "https://github.com/acme/widget/pull/12/files", 12, true},
		{"trailing slash", "https://github.com/acme/widget/pull/12/", 12, true},
		{"review comment fragment", "https://github.com/acme/widget/pull/12#discussion_r99", 12, true},
		{"query string", "https://github.com/acme/widget/pull/12?w=1", 12, true},
		{"surrounding whitespace", "  https://github.com/acme/widget/pull/5\n", 5, true},

		{"empty", "", 0, false},
		{"not a pull request url", "https://github.com/acme/widget", 0, false},
		{"an issue is not a pull request", "https://github.com/acme/widget/issues/12", 0, false},
		{"no number after pull", "https://github.com/acme/widget/pull", 0, false},
		{"number is not a number", "https://github.com/acme/widget/pull/abc", 0, false},
		{"zero is not a pull request number", "https://github.com/acme/widget/pull/0", 0, false},
		{"negative", "https://github.com/acme/widget/pull/-3", 0, false},
		{"a repository literally named pull", "https://github.com/acme/pull", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParsePullRequestNumber(tc.in)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// The branch name is shared by the runner, the pipeline, the task chat and the
// commit tool. They must agree exactly, so the shape is pinned here: the
// `feature/` prefix (CI triggers on `feature/**`) and the task key.
func TestTaskBranchName(t *testing.T) {
	id := uuid.MustParse("11112222-3333-4444-5555-666677778888")

	// The title is not part of the name any more: it is written in the human's
	// language, and a Turkish one produced an unreadable branch.
	assert.Equal(t, "feature/de-12",
		TaskBranchName(BoardTask{ID: id, Key: "DE-12", TaskNumber: 12, Title: "Wishlist kısmını kaldır"}))
	// A task read without the board prefix still has its number.
	assert.Equal(t, "feature/task-12",
		TaskBranchName(BoardTask{ID: id, TaskNumber: 12, Title: "Add the store link"}))
	// Neither key nor number: the id keeps the branch legal and unique.
	assert.Equal(t, "feature/task-11112222",
		TaskBranchName(BoardTask{ID: id, Title: "Add the store link"}))
	// A key with anything git would refuse collapses to hyphens.
	assert.Equal(t, "feature/de-12",
		TaskBranchName(BoardTask{ID: id, Key: " DE  #12 ", TaskNumber: 12}))
}

// The key carries the task's type, so "B-4" and "A-2" are legible on their own
// — in a branch, a commit trailer or a chat — without looking the task up.
func TestTaskKeyPrefixes(t *testing.T) {
	assert.Equal(t, "T-7", FormatTaskKey(TaskTypeTask, 7))
	assert.Equal(t, "B-7", FormatTaskKey(TaskTypeBug, 7))
	assert.Equal(t, "A-7", FormatTaskKey(TaskTypeAnaliz, 7))
	assert.Equal(t, "TC-7", FormatTaskKey(TaskTypeTechnical, 7))
	// An unset type is ordinary work rather than an error: the board created
	// tasks before types were mandatory.
	assert.Equal(t, "T-7", FormatTaskKey("", 7))

	for prefix, want := range map[string]TaskType{"T": TaskTypeTask, "b": TaskTypeBug, " a ": TaskTypeAnaliz, "tc": TaskTypeTechnical} {
		got, ok := TaskTypeForKeyPrefix(prefix)
		assert.True(t, ok, prefix)
		assert.Equal(t, want, got)
	}
	// An unknown prefix must not fall through to "task": a lookup for X-1 has
	// to fail as unknown rather than answer with T-1.
	_, ok := TaskTypeForKeyPrefix("DE")
	assert.False(t, ok)
}
