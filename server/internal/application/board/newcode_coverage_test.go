package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDiffLinesReadsNewSideHunks(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -10,0 +11,3 @@
+one
+two
+three
@@ -40 +43 @@
+replaced
diff --git a/gone.go b/gone.go
--- a/gone.go
+++ /dev/null
@@ -1,5 +0,0 @@
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -7,4 +7,0 @@
`
	got := parseDiffLines(diff)

	assert.Equal(t, map[int]bool{11: true, 12: true, 13: true, 43: true}, got["a.go"])
	assert.NotContains(t, got, "gone.go")
	assert.NotContains(t, got, "b.go")
}

func TestGoProfileLinesStripsTheModulePathAndKeepsTheHighestCount(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module github.com/acme/thing\n\ngo 1.23\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage.out"), []byte(
		"mode: atomic\n"+
			"github.com/acme/thing/internal/a.go:3.10,5.2 2 4\n"+
			"github.com/acme/thing/internal/a.go:4.2,4.20 1 0\n"+
			"github.com/acme/thing/internal/b.go:7.1,7.30 1 0\n"), 0o600))

	hits, ok := goProfileLines(dir)
	require.True(t, ok)

	assert.Equal(t, map[int]int{3: 4, 4: 4, 5: 4}, hits["internal/a.go"])
	assert.Equal(t, map[int]int{7: 0}, hits["internal/b.go"])
}

func TestLcovLinesNormalisesAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "coverage"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage", "lcov.info"), []byte(
		"SF:"+filepath.Join(dir, "src", "app.ts")+"\nDA:1,3\nDA:2,0\nend_of_record\n"+
			"SF:./src/util.ts\nDA:9,1\nend_of_record\n"), 0o600))

	hits, ok := lcovLines(dir)
	require.True(t, ok)

	assert.Equal(t, map[int]int{1: 3, 2: 0}, hits["src/app.ts"])
	assert.Equal(t, map[int]int{9: 1}, hits["src/util.ts"])
}

func TestMeasureNewCodeCountsOnlyInstrumentedChangedLines(t *testing.T) {
	dir := initGitRepoWithChange(t, map[string]int{"a.go": 4})
	hits := lineHits{"a.go": {1: 1, 2: 0, 3: 5}}

	res := measureNewCode(context.Background(), dir, hits)

	require.True(t, res.Measured, res.Detail)
	assert.Equal(t, 3, res.Total)
	assert.Equal(t, 2, res.Covered)
	assert.Equal(t, []string{"a.go:2"}, res.Uncovered)
}

func TestMeasureNewCodeIgnoresFilesTheProfileDoesNotMention(t *testing.T) {
	dir := initGitRepoWithChange(t, map[string]int{"a.go": 2, "docker-compose.yml": 3})
	hits := lineHits{"a.go": {1: 1, 2: 1}}

	res := measureNewCode(context.Background(), dir, hits)

	require.True(t, res.Measured, res.Detail)
	assert.Equal(t, 2, res.Total)
	assert.Equal(t, 100.0, res.Percent)
}

func TestNewCodeCoverageReportStatesTheFigureWithoutGating(t *testing.T) {
	t.Run("no profile reports what could not be measured", func(t *testing.T) {
		report := newCodeCoverageReport(context.Background(), t.TempDir(), nil)
		assert.Contains(t, report, "[unverified] new-code coverage")
	})

	t.Run("a diff too small to mean anything states its counts", func(t *testing.T) {
		dir := initGitRepoWithChange(t, map[string]int{"a.go": 2})
		report := newCodeCoverageReport(context.Background(), dir, lineHits{"a.go": {1: 0, 2: 0}})
		assert.Contains(t, report, "too few to read anything into")
		assert.NotContains(t, report, coverageWarningMarker, "two coverable lines cannot carry a verdict")
	})

	t.Run("under the bar warns and names the lines", func(t *testing.T) {
		dir := initGitRepoWithChange(t, map[string]int{"a.go": 6})
		hits := lineHits{"a.go": {1: 1, 2: 1, 3: 1, 4: 0, 5: 0, 6: 0}}

		report := newCodeCoverageReport(context.Background(), dir, hits)

		assert.Contains(t, report, coverageWarningMarker)
		assert.Contains(t, report, "50.0%")
		assert.Contains(t, report, "a.go:4")
		assert.Contains(t, report, "not the repository's overall figure")
		assert.Contains(t, report, "the task moves on either way")
	})

	t.Run("at the bar reports the figure plainly", func(t *testing.T) {
		dir := initGitRepoWithChange(t, map[string]int{"a.go": 10})
		hits := lineHits{"a.go": {1: 1, 2: 1, 3: 1, 4: 1, 5: 1, 6: 1, 7: 1, 8: 1, 9: 1, 10: 0}}

		report := newCodeCoverageReport(context.Background(), dir, hits)

		assert.Contains(t, report, "90.0%")
		assert.NotContains(t, report, coverageWarningMarker)
	})
}

func TestChangedLinesNameNonASCIIPathsUnquoted(t *testing.T) {
	dir := initGitRepoWithChange(t, map[string]int{"ödeme/sepet.go": 2})

	changed, err := changedLines(context.Background(), dir)

	require.NoError(t, err)
	assert.Equal(t, map[string]map[int]bool{"ödeme/sepet.go": {1: true, 2: true}}, changed)
}

func initGitRepoWithChange(t *testing.T, files map[string]int) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		out, err := runGit(context.Background(), dir, args...)
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("commit", "--allow-empty", "-m", "base")
	run("update-ref", "refs/remotes/origin/main", "HEAD")

	for path, lineCount := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		content := ""
		for i := 0; i < lineCount; i++ {
			content += "x\n"
		}
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	run("add", "-A")
	run("commit", "-m", "change")
	return dir
}
