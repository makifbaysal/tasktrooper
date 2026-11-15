package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func TestScopeWorkingTreeKeepsOnlyTheAutomationProject(t *testing.T) {
	dir := t.TempDir()
	sh := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sh("init", "-q")
	write("app/main.go", "package main\n")
	write("qa-automation/README.md", "suite\n")
	sh("add", "-A")
	sh("commit", "-q", "-m", "init")

	write("app/main.go", "package main // patched by QA\n")
	write("app/new.go", "package main\n")
	write("qa-automation/api/export_test.go", "package api\n")
	write("qa-automation/README.md", "suite, more\n")

	kept, discarded, err := NewClient().ScopeWorkingTree(context.Background(), dir, "qa-automation")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(kept)
	sort.Strings(discarded)
	if len(kept) != 2 || kept[0] != "qa-automation/README.md" || kept[1] != "qa-automation/api/export_test.go" {
		t.Fatalf("kept %v", kept)
	}
	if len(discarded) != 2 || discarded[0] != "app/main.go" || discarded[1] != "app/new.go" {
		t.Fatalf("discarded %v", discarded)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "app/main.go")); string(body) != "package main\n" {
		t.Fatalf("app/main.go not restored: %q", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "app/new.go")); !os.IsNotExist(err) {
		t.Fatalf("app/new.go still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "qa-automation/api/export_test.go")); err != nil {
		t.Fatalf("automation test removed: %v", err)
	}
}
