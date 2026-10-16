package board

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return dir
}

func TestDetectCoverageCoversJestRustAndDotnet(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"jest config", map[string]string{"package.json": `{}`, "jest.config.ts": ""},
			[]string{"npx", "jest", "--coverage", "--coverageReporters=text-summary", "--coverageReporters=lcov"}},
		{"jest in package.json", map[string]string{"package.json": `{"devDependencies":{"jest":"^29"}}`},
			[]string{"npx", "jest", "--coverage", "--coverageReporters=text-summary", "--coverageReporters=lcov"}},
		{"vitest wins over jest", map[string]string{"package.json": `{"devDependencies":{"jest":"^29"}}`, "vitest.config.ts": ""},
			[]string{"npx", "vitest", "run", "--coverage", "--coverage.reporter=text-summary", "--coverage.reporter=lcov"}},
		{"rust", map[string]string{"Cargo.toml": "[package]\nname = \"api\"\n"},
			[]string{"cargo", "llvm-cov", "--lcov", "--output-path", "lcov.info"}},
		{"dotnet solution", map[string]string{"Api.sln": ""},
			[]string{"dotnet", "test", "--collect:XPlat Code Coverage", "--results-directory", ".tasktrooper-coverage"}},
		{"dotnet project", map[string]string{"Api.csproj": "<Project/>"},
			[]string{"dotnet", "test", "--collect:XPlat Code Coverage", "--results-directory", ".tasktrooper-coverage"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stage := detectCoverage(writeFiles(t, tc.files))
			require.NotNil(t, stage)
			assert.Equal(t, tc.want, stage.command)
		})
	}
	assert.Nil(t, detectCoverage(writeFiles(t, map[string]string{"package.json": `{"devDependencies":{"mocha":"^10"}}`})))
}

func TestDetectMutationRunsRustAndDotnetOnlyWhereTheRepositoryConfiguredIt(t *testing.T) {
	assert.Nil(t, detectMutation(writeFiles(t, map[string]string{"Cargo.toml": ""})))
	assert.Nil(t, detectMutation(writeFiles(t, map[string]string{"Api.sln": ""})))

	stage := detectMutation(writeFiles(t, map[string]string{"Cargo.toml": "", ".cargo/mutants.toml": ""}))
	require.NotNil(t, stage)
	assert.Equal(t, []string{"cargo", "mutants"}, stage.command)

	stage = detectMutation(writeFiles(t, map[string]string{"Api.sln": "", "stryker-config.json": "{}"}))
	require.NotNil(t, stage)
	assert.Equal(t, []string{"dotnet", "stryker"}, stage.command)
}

func TestParseCargoMutantsScore(t *testing.T) {
	out := "Found 25 mutants to test\nok       Unmutated baseline in 1.2s build + 0.3s test\n25 mutants tested in 15s: 3 missed, 18 caught, 2 timeouts, 2 unviable\n"
	pct, ok := parseCargoMutantsScore("", out)
	require.True(t, ok)
	assert.InDelta(t, 87.0, pct, 0.1)

	pct, ok = parseCargoMutantsScore("", "4 mutants tested in 3s: 4 caught\n")
	require.True(t, ok)
	assert.InDelta(t, 100.0, pct, 0.01)

	_, ok = parseCargoMutantsScore("", "3 mutants tested in 1s: 3 unviable\n")
	assert.False(t, ok, "nothing the tests could kill is no score")
	_, ok = parseCargoMutantsScore("", "error: no such command: `mutants`")
	assert.False(t, ok)
}

func TestParseStrykerScoreReadsStrykerNet(t *testing.T) {
	pct, ok := parseStrykerScore("", "[12:00:00 INF] The final mutation score is 72.41 %")
	require.True(t, ok)
	assert.InDelta(t, 72.41, pct, 0.001)
}

func TestCoberturaMergesTestProjectsByLine(t *testing.T) {
	dir := t.TempDir()
	report := func(runID, body string) {
		path := filepath.Join(dir, dotnetCoverageDir, runID, "coverage.cobertura.xml")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	src := filepath.Join(dir, "src") + string(filepath.Separator)
	report("a", `<?xml version="1.0"?>
<coverage line-rate="0.5" lines-covered="2" lines-valid="4">
  <sources><source>`+src+`</source></sources>
  <packages><package name="Api"><classes>
    <class name="Api.Tasks" filename="Api/Tasks.cs" line-rate="0.5">
      <methods><method name="Create"><lines><line number="10" hits="3"/></lines></method></methods>
      <lines><line number="10" hits="3"/><line number="11" hits="0"/><line number="12" hits="1"/><line number="13" hits="0"/></lines>
    </class>
  </classes></package></packages>
</coverage>`)
	report("b", `<?xml version="1.0"?>
<coverage line-rate="0.25">
  <sources><source>`+src+`</source></sources>
  <packages><package name="Api"><classes>
    <class name="Api.Tasks" filename="Api/Tasks.cs"><lines><line number="11" hits="2"/></lines></class>
  </classes></package></packages>
</coverage>`)

	hits, ok := coberturaLines(dir)
	require.True(t, ok)
	assert.Equal(t, lineHits{"src/Api/Tasks.cs": {10: 3, 11: 2, 12: 1, 13: 0}}, hits)

	pct, ok := parseCoberturaCoverage(dir, "")
	require.True(t, ok)
	assert.InDelta(t, 75.0, pct, 0.01)

	_, ok = parseCoberturaCoverage(t.TempDir(), "")
	assert.False(t, ok)
}

func TestRunCoverageNamesAMissingRunnerPlugin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	stage := &coverageStage{
		name:        "coverage",
		command:     []string{"sh", "-c", "echo 'error: no such command: `llvm-cov`' >&2; exit 101"},
		parse:       func(string, string) (float64, bool) { return 0, false },
		missingTool: []string{"no such command: `llvm-cov`"},
	}
	res := runCoverage(t.Context(), t.TempDir(), stage, coverageTimeout)
	assert.False(t, res.Measured)
	assert.True(t, res.ToolMissing)
	assert.Equal(t, "sh -c is not installed in this environment", res.Detail)
}

func TestRunCoverageExplainsAPassingRunWithNoReport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	stage := &coverageStage{
		name:      "coverage",
		command:   []string{"sh", "-c", "mkdir -p out && echo passed"},
		parse:     func(string, string) (float64, bool) { return 0, false },
		noNumber:  "no Cobertura report was written",
		artifacts: []string{"out"},
	}
	dir := t.TempDir()
	res := runCoverage(t.Context(), dir, stage, coverageTimeout)
	assert.Equal(t, "no Cobertura report was written", res.Detail)
	assert.NoDirExists(t, filepath.Join(dir, "out"), "a directory artifact is removed too")
}
