package domain

import "testing"

func TestJoinLocalCommandsGroupsByDirectory(t *testing.T) {
	got := JoinLocalCommands([]LocalCommand{
		{Dir: "desktop", Argv: []string{"npm", "run", "lint"}},
		{Dir: "desktop", Argv: []string{"npm", "test"}},
		{Dir: ".", Argv: []string{"make", "check"}},
	})
	want := "(cd desktop && npm run lint && npm test); make check"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestJoinLocalCommandsSingleGroupNeedsNoSubshell(t *testing.T) {
	got := JoinLocalCommands([]LocalCommand{
		{Dir: "web", Argv: []string{"npm", "ci"}},
		{Dir: "web", Argv: []string{"npm", "test"}},
	})
	if want := "cd web && npm ci && npm test"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestJoinLocalCommandsKeepsEveryGroupRelativeToTheRoot(t *testing.T) {
	got := JoinLocalCommands([]LocalCommand{
		{Dir: "api", Argv: []string{"go", "test", "./..."}},
		{Dir: "web", Argv: []string{"npm", "test"}},
	})
	if want := "(cd api && go test ./...); (cd web && npm test)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
