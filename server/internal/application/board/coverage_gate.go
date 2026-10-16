package board

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/toolchain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/hostshell"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
)

const DefaultCoverageThreshold = 90.0

const coverageWarningMarker = "[coverage warning]"

const mutationWarningMarker = "[mutation warning]"

const coverageTimeout = 20 * time.Minute

const mutationTimeout = 25 * time.Minute

type CoverageResult struct {
	Percent  float64
	Measured bool
	Detail   string
	Output   string
	Lines    lineHits
	// ToolMissing: the recipe's tool is not on this machine, which only
	// matters to a repository that switched the gate on.
	ToolMissing bool
}

type coverageStage struct {
	name      string
	command   []string
	parse     func(dir, out string) (float64, bool)
	lines     func(dir string) (lineHits, bool)
	artifacts []string
	// missingTool is what a runner prints when the plugin it dispatches to
	// is not installed (cargo's "no such command"): the exit alone reads
	// like a failed suite.
	missingTool []string
	// noNumber says why a run that passed left nothing to read, when the
	// usual cause is one setup step (a .NET test project without coverlet).
	noNumber string
}

const (
	rustLcovReport    = "lcov.info"
	dotnetCoverageDir = ".tasktrooper-coverage"
)

func detectCoverage(dir string) *coverageStage {
	switch {
	case markerExists(dir, "go.mod"):
		return &coverageStage{
			name:      "coverage",
			command:   []string{"go", "test", "-coverprofile=coverage.out", "-covermode=atomic", "./..."},
			parse:     parseGoCoverage,
			lines:     goProfileLines,
			artifacts: []string{"coverage.out"},
		}
	case markerExists(dir, "pubspec.yaml"):
		return &coverageStage{
			name:    "coverage",
			command: []string{"flutter", "test", "--coverage"},
			parse:   parseLcovCoverage,
			lines:   lcovLines,
		}
	case hasVitest(dir):
		return &coverageStage{
			name: "coverage",
			command: []string{
				"npx", "vitest", "run", "--coverage",
				"--coverage.reporter=text-summary", "--coverage.reporter=lcov",
			},
			parse: parseReportedCoverage,
			lines: lcovLines,
		}
	case hasJest(dir):
		return &coverageStage{
			name:    "coverage",
			command: []string{"npx", "jest", "--coverage", "--coverageReporters=text-summary", "--coverageReporters=lcov"},
			parse:   parseReportedCoverage,
			lines:   lcovLines,
		}
	case markerExists(dir, "Cargo.toml"):
		return &coverageStage{
			name:        "coverage",
			command:     []string{"cargo", "llvm-cov", "--lcov", "--output-path", rustLcovReport},
			parse:       func(dir, _ string) (float64, bool) { return lcovPercent(filepath.Join(dir, rustLcovReport)) },
			lines:       func(dir string) (lineHits, bool) { return lcovFileLines(dir, rustLcovReport) },
			artifacts:   []string{rustLcovReport},
			missingTool: []string{"no such command: `llvm-cov`", "llvm-tools-preview"},
		}
	case hasDotnetProject(dir):
		return &coverageStage{
			name:      "coverage",
			command:   []string{"dotnet", "test", "--collect:XPlat Code Coverage", "--results-directory", dotnetCoverageDir},
			parse:     parseCoberturaCoverage,
			lines:     coberturaLines,
			artifacts: []string{dotnetCoverageDir},
			noNumber:  "no Cobertura report was written; the test projects need the coverlet.collector package",
		}
	}
	return nil
}

func detectMutation(dir string) *coverageStage {
	switch {
	case markerExists(dir, "go.mod"):
		return &coverageStage{
			name:    "mutation",
			command: []string{"gremlins", "unleash", "--dry-run=false", "--output=json"},
			parse:   parseGremlinsScore,
		}
	case hasStryker(dir):
		return &coverageStage{
			name:    "mutation",
			command: []string{"npx", "stryker", "run"},
			parse:   parseStrykerScore,
		}
	case markerExists(dir, filepath.Join(".cargo", "mutants.toml")) && markerExists(dir, "Cargo.toml"):
		return &coverageStage{
			name:        "mutation",
			command:     []string{"cargo", "mutants"},
			parse:       parseCargoMutantsScore,
			artifacts:   []string{"mutants.out", "mutants.out.old"},
			missingTool: []string{"no such command: `mutants`"},
		}
	case markerExists(dir, "stryker-config.json", "stryker-config.yaml", "stryker-config.yml") && hasDotnetProject(dir):
		return &coverageStage{
			name:        "mutation",
			command:     []string{"dotnet", "stryker"},
			parse:       parseStrykerScore,
			artifacts:   []string{"StrykerOutput"},
			missingTool: []string{"could not execute because the specified command or file was not found"},
		}
	}
	return nil
}

var jestConfigs = []string{"jest.config.js", "jest.config.ts", "jest.config.mjs", "jest.config.cjs", "jest.config.json"}

func hasJest(dir string) bool {
	if markerExists(dir, jestConfigs...) {
		return true
	}
	pkg, err := os.ReadFile(filepath.Join(dir, "package.json"))
	return err == nil && bytes.Contains(pkg, []byte(`"jest"`))
}

func hasDotnetProject(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".sln", ".slnx", ".csproj", ".fsproj":
			return true
		}
	}
	return false
}

func hasVitest(dir string) bool {
	return markerExists(dir,
		"vitest.config.ts", "vitest.config.js", "vitest.config.mts",
		"vite.config.ts", "vite.config.js") && markerExists(dir, "package.json")
}

func hasStryker(dir string) bool {
	return markerExists(dir, "stryker.config.json", "stryker.conf.json", "stryker.config.mjs", ".stryker.conf.json")
}

func runCoverage(ctx context.Context, dir string, stage *coverageStage, timeout time.Duration) CoverageResult {
	if stage == nil {
		return CoverageResult{Detail: "no coverage recipe for this project type"}
	}
	overlay := toolchain.Default.Overlay(dir)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := hostshell.ResolveArgv(stage.command)
	cmd := exec.CommandContext(cmdCtx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = childenv.For(os.Environ(), overlay.Env)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := runTree(cmd)
	out := buf.String()

	defer func() {
		for _, artifact := range stage.artifacts {
			_ = os.RemoveAll(filepath.Join(dir, artifact))
		}
	}()
	var lines lineHits
	if stage.lines != nil {
		if read, ok := stage.lines(dir); ok {
			lines = read
		}
	}

	if err != nil && isToolMissing(err) {
		return CoverageResult{Detail: stage.command[0] + " is not installed in this environment", ToolMissing: true}
	}
	if err != nil && printsMissingTool(out, stage.missingTool) {
		return CoverageResult{Detail: strings.Join(stage.command[:2], " ") + " is not installed in this environment", ToolMissing: true}
	}
	pct, ok := stage.parse(dir, out)
	if !ok {
		detail := "could not read a coverage number from the run"
		if err != nil {
			detail = "the test suite failed"
		} else if stage.noNumber != "" {
			detail = stage.noNumber
		}
		return CoverageResult{Detail: detail, Output: truncateTail(out, 6000), Lines: lines}
	}
	if err != nil {

		log.Debug().Err(err).Str("stage", stage.name).Msg("coverage command exited non-zero but reported a number")
	}
	return CoverageResult{Percent: pct, Measured: true, Output: truncateTail(out, 4000), Lines: lines}
}

func printsMissingTool(out string, markers []string) bool {
	lower := strings.ToLower(out)
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func isToolMissing(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "executable file not found") ||
		strings.Contains(err.Error(), "no such file or directory") ||
		missingToolExit(runtime.GOOS, err))
}

// windowsCommandNotFoundExit is what the python/python3 App Execution Alias
// stubs exit with on a Windows machine without Python (they only point at
// the Microsoft Store), and what cmd.exe exits with for an unknown command.
// It says the tool is absent, not that the build failed.
const windowsCommandNotFoundExit = 9009

func missingToolExit(goos string, err error) bool {
	var exit interface{ ExitCode() int }
	return goos == "windows" && errors.As(err, &exit) && exit.ExitCode() == windowsCommandNotFoundExit
}

func parseGoCoverage(dir, out string) (float64, bool) {
	profile := filepath.Join(dir, "coverage.out")
	if _, err := os.Stat(profile); err != nil {
		return 0, false
	}

	cmd := exec.Command("go", "tool", "cover", "-func="+profile)
	cmd.Dir = dir
	funcOut, err := cmd.CombinedOutput()
	if err != nil {
		return 0, false
	}

	return parseReportedCoverage("", string(funcOut))
}

func parseLcovCoverage(dir, _ string) (float64, bool) {
	return lcovPercent(filepath.Join(dir, "coverage", "lcov.info"))
}

func lcovPercent(path string) (float64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var total, covered int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "DA:") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(line, "DA:"), ",")
		if len(parts) != 2 {
			continue
		}
		total++
		if hits, err := strconv.Atoi(parts[1]); err == nil && hits > 0 {
			covered++
		}
	}
	if total == 0 {
		return 0, false
	}
	return float64(covered) * 100 / float64(total), true
}

func parseReportedCoverage(_, out string) (float64, bool) {
	if pct := ParseCoverage(out); pct != nil {
		return *pct, true
	}
	return 0, false
}

var gremlinsScore = regexp.MustCompile(`(?i)"mutation\s*score"?\s*:?\s*([0-9.]+)`)

func parseGremlinsScore(_, out string) (float64, bool) {
	m := gremlinsScore.FindStringSubmatch(out)
	if len(m) != 2 {
		return 0, false
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	if pct <= 1 {
		pct *= 100
	}
	return pct, true
}

// cargoMutantsCount reads cargo-mutants' closing line, "25 mutants tested in
// 15s: 3 missed, 20 caught, 2 unviable", which leaves out a zero count.
var cargoMutantsCount = regexp.MustCompile(`(\d+) (missed|caught|timeouts?|unviable)\b`)

// parseCargoMutantsScore counts a timeout as killed, as Stryker does: the
// tests noticed the mutant, they just never finished.
func parseCargoMutantsScore(_, out string) (float64, bool) {
	summary := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "mutants tested") {
			summary = line
		}
	}
	var killed, missed int
	for _, m := range cargoMutantsCount.FindAllStringSubmatch(summary, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		switch m[2] {
		case "missed":
			missed += n
		case "caught", "timeout", "timeouts":
			killed += n
		}
	}
	if killed+missed == 0 {
		return 0, false
	}
	return float64(killed) * 100 / float64(killed+missed), true
}

var strykerScore = regexp.MustCompile(`(?i)mutation score[^0-9]*([0-9.]+)`)

func parseStrykerScore(_, out string) (float64, bool) {
	m := strykerScore.FindStringSubmatch(out)
	if len(m) != 2 {
		return 0, false
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	return pct, err == nil
}

func coverageThreshold(repo domain.Repository, subProjectPath string) float64 {
	if gate := repo.EffectiveCoverageGate(subProjectPath); gate.Threshold > 0 {
		return gate.Threshold
	}
	return DefaultCoverageThreshold
}

func coverageReport(ctx context.Context, dir string, repo domain.Repository, subProjectPath string) string {
	stage := detectCoverage(dir)
	if stage == nil {
		return ""
	}
	res := runCoverage(ctx, dir, stage, coverageTimeout)
	if !res.Measured {
		if res.ToolMissing && !repo.EffectiveCoverageGate(subProjectPath).Enabled {
			return ""
		}
		note := "[unverified] coverage: " + res.Detail
		if res.Output != "" {
			note += "\n" + res.Output
		}
		return note
	}

	notes := []string{overallCoverageNote(repo, subProjectPath, res.Percent)}
	if newCodeNote := newCodeCoverageReport(ctx, dir, res.Lines); newCodeNote != "" {
		notes = append(notes, newCodeNote)
	}

	threshold := coverageThreshold(repo, subProjectPath)
	if repo.EffectiveCoverageGate(subProjectPath).Enabled && res.Percent+0.005 < threshold {
		notes = append(notes, coverageOverallWarning(res.Percent, threshold))
	}
	return strings.Join(notes, "\n")
}

// coverageOverallWarning is advisory-only: the caller never holds the task
// on it, so it just says what is missing and why it does not matter here.
func coverageOverallWarning(percent, threshold float64) string {
	return coverageOverallWarningKey.Render(coverageOverallWarningData{
		Marker:    coverageWarningMarker,
		Percent:   fmt.Sprintf("%.1f", percent),
		Threshold: fmt.Sprintf("%.0f", threshold),
	})
}

func overallCoverageNote(repo domain.Repository, subProjectPath string, percent float64) string {
	threshold := coverageThreshold(repo, subProjectPath)
	if repo.EffectiveCoverageGate(subProjectPath).Enabled {
		return coverageOverallNoteAdvisoryKey.Render(coverageOverallNoteAdvisoryData{
			Percent: fmt.Sprintf("%.1f", percent), Threshold: fmt.Sprintf("%.0f", threshold),
		})
	}
	return coverageOverallNoteUnsetKey.Render(coverageOverallNoteUnsetData{Percent: fmt.Sprintf("%.1f", percent)})
}

func runMutation(ctx context.Context, dir string, repo domain.Repository, subProjectPath string) string {
	stage := detectMutation(dir)
	if stage == nil {
		return ""
	}
	res := runCoverage(ctx, dir, stage, mutationTimeout)
	if !res.Measured {
		log.Debug().Str("dir", dir).Str("detail", res.Detail).Msg("mutation testing not run")
		return ""
	}
	note := mutationNote(res.Percent)
	gate := repo.EffectiveMutationGate(subProjectPath)
	if !gate.Enabled || gate.Threshold <= 0 {
		return note
	}
	if res.Percent+0.005 < gate.Threshold {
		return note + mutationWarning(res.Percent, gate.Threshold)
	}
	return note + mutationGateMet(gate.Threshold)
}

func mutationGateMet(threshold float64) string {
	return mutationGateMetKey.Render(mutationGateMetData{Threshold: fmt.Sprintf("%.0f", threshold)})
}

// mutationNote always runs — reported score, not a verdict.
func mutationNote(percent float64) string {
	return mutationNoteKey.Render(mutationNoteData{Percent: fmt.Sprintf("%.1f", percent)})
}

// mutationWarning is advisory-only, same as coverageOverallWarning.
func mutationWarning(percent, threshold float64) string {
	return mutationWarningKey.Render(mutationWarningData{
		Marker:    mutationWarningMarker,
		Percent:   fmt.Sprintf("%.1f", percent),
		Threshold: fmt.Sprintf("%.0f", threshold),
	})
}

// runTree runs cmd and kills whatever it left behind, so a test runner's
// workers cannot outlive the step that started them.
func runTree(cmd *exec.Cmd) error {
	tree, err := proctree.Start(cmd)
	if err != nil {
		return err
	}
	defer tree.Close()
	return cmd.Wait()
}
