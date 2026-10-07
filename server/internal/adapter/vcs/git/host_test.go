package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostArgsTurnsOnLongPathsOnlyOnWindows(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want []string
	}{
		{name: "windows", goos: "windows", want: []string{"-c", "core.longpaths=true", "clone", "src", "dst"}},
		{name: "darwin", goos: "darwin", want: []string{"clone", "src", "dst"}},
		{name: "linux", goos: "linux", want: []string{"clone", "src", "dst"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hostArgs(tt.goos, []string{"clone", "src", "dst"}))
		})
	}
}

func pathEntries(env []string) []string {
	var out []string
	for _, kv := range env {
		if key, _, _ := strings.Cut(kv, "="); strings.EqualFold(key, "PATH") {
			out = append(out, kv)
		}
	}
	return out
}

func TestToolPathEnv(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		environ  []string
		wantPath []string
	}{
		{
			name:     "darwin appends both Homebrew prefixes with a colon",
			goos:     "darwin",
			environ:  []string{"HOME=/u", "PATH=/usr/bin:/bin"},
			wantPath: []string{"PATH=/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin"},
		},
		{
			name:     "darwin does not repeat a directory already on PATH",
			goos:     "darwin",
			environ:  []string{"PATH=/opt/homebrew/bin:/usr/bin"},
			wantPath: []string{"PATH=/opt/homebrew/bin:/usr/bin:/usr/local/bin"},
		},
		{
			name:     "linux adds only /usr/local/bin",
			goos:     "linux",
			environ:  []string{"PATH=/usr/bin"},
			wantPath: []string{"PATH=/usr/bin:/usr/local/bin"},
		},
		{
			name:     "a missing PATH is created",
			goos:     "linux",
			environ:  []string{"HOME=/u"},
			wantPath: []string{"PATH=/usr/local/bin"},
		},
		{
			name:     "windows keeps its own Path key untouched and adds no second one",
			goos:     "windows",
			environ:  []string{`Path=C:\Windows\System32;C:\Program Files\Git\cmd`, `USERPROFILE=C:\Users\u`},
			wantPath: []string{`Path=C:\Windows\System32;C:\Program Files\Git\cmd`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolPathEnv(tt.goos, tt.environ)
			assert.Equal(t, tt.wantPath, pathEntries(got))
			assert.Len(t, got, len(tt.environ)+len(tt.wantPath)-len(pathEntries(tt.environ)))
		})
	}
}

func TestToolPathEnvLeavesTheCallersSliceAlone(t *testing.T) {
	environ := []string{"PATH=/usr/bin"}
	toolPathEnv("darwin", environ)
	assert.Equal(t, []string{"PATH=/usr/bin"}, environ)
}

func TestJoinOutputKeepsBothStreamsOfAFailure(t *testing.T) {
	tests := []struct {
		name, stdout, stderr, want string
	}{
		{name: "stderr only", stderr: "fatal: no\n", want: "fatal: no\n"},
		{name: "stdout only", stdout: "CONFLICT x\n", want: "CONFLICT x\n"},
		{name: "both", stdout: "CONFLICT x\n", stderr: "error: could not apply\n", want: "CONFLICT x\nerror: could not apply\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, joinOutput(tt.stdout, tt.stderr))
		})
	}
}

// A workspace on Windows runs with core.autocrlf=true, so git warns on stderr
// about every LF file it diffs; those warnings must never come back as paths.
func TestChangedFilesIgnoreAutocrlfWarnings(t *testing.T) {
	f := newTaskFixture(t)
	require.NoError(t, f.ensure(t))
	base := gitRun(t, f.workspace, "rev-parse", "HEAD")
	gitRun(t, f.workspace, "config", "core.autocrlf", "true")
	require.NoError(t, os.WriteFile(filepath.Join(f.workspace, "first.go"), []byte("package main\n\nfunc x() {}\n"), 0o644))

	c := NewClient()
	changed, err := c.TaskChangedFiles(context.Background(), f.workspace)
	require.NoError(t, err)
	assert.Equal(t, []string{"first.go"}, changed)

	since, err := c.ChangedFilesSince(context.Background(), f.workspace, base)
	require.NoError(t, err)
	assert.Equal(t, []string{"first.go"}, since)
}

func TestChangedFilesReturnNonASCIINamesUnquoted(t *testing.T) {
	f := newTaskFixture(t)
	require.NoError(t, f.ensure(t))
	base := gitRun(t, f.workspace, "rev-parse", "HEAD")
	f.commitLocal(t, "çalışma alanı.go", "package main\n")

	c := NewClient()
	changed, err := c.TaskChangedFiles(context.Background(), f.workspace)
	require.NoError(t, err)
	assert.Equal(t, []string{"çalışma alanı.go"}, changed)

	since, err := c.ChangedFilesSince(context.Background(), f.workspace, base)
	require.NoError(t, err)
	assert.Equal(t, []string{"çalışma alanı.go"}, since)
}

func TestConflictingPathsReturnsNonASCIINamesUnquoted(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "--initial-branch=main")
	name := "ödeme.txt"
	write := func(body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
		gitRun(t, dir, "commit", "-am", body)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("base\n"), 0o644))
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "other")
	write("other\n")
	gitRun(t, dir, "checkout", "main")
	write("main\n")
	_, err := NewClient().run(context.Background(), dir, "git", "merge", "other")
	require.Error(t, err, "fixture invalid: the merge was expected to conflict")

	assert.Equal(t, []string{name}, NewClient().conflictingPaths(context.Background(), dir))
}

func TestCloneArgsPersistLongPathsOnWindows(t *testing.T) {
	if got := cloneArgs("windows", "url", "dest"); strings.Join(got, " ") != "clone --config core.longpaths=true url dest" {
		t.Fatalf("windows clone args = %v", got)
	}
	if got := cloneArgs("linux", "url", "dest"); strings.Join(got, " ") != "clone url dest" {
		t.Fatalf("linux clone args = %v", got)
	}
}
