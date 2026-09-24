package domain

import "testing"

func TestJoinLocalCommandsGroupsByDirectory(t *testing.T) {
	got := JoinLocalCommands([]LocalCommand{
		{Dir: "desktop", Argv: []string{"npm", "run", "lint"}},
		{Dir: "desktop", Argv: []string{"npm", "test"}},
		{Dir: ".", Argv: []string{"make", "check"}},
	})
	want := "cd desktop && npm run lint && npm test; make check"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
