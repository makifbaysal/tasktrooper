package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func runCheck(runs ...string) domain.ComponentCheck {
	c := domain.ComponentCheck{Workflow: ".github/workflows/ci.yml", JobKey: "test"}
	for _, r := range runs {
		c.Steps = append(c.Steps, domain.CheckStep{Run: r})
	}
	return c
}

func names(tcs []Toolchain) []string {
	out := []string{}
	for _, tc := range tcs {
		out = append(out, tc.ID)
	}
	return out
}

func TestToolchainsForMapsLanguagesOnceInStackOrder(t *testing.T) {
	got := ToolchainsFor([]string{"TypeScript", "Go", "JavaScript", "Shell", "Kotlin", "Java"})
	assert.Equal(t, []string{"node", "go", "jvm"}, names(got))
	assert.Empty(t, ToolchainsFor([]string{"Shell", "SQL", "Terraform"}))
}

func TestEveryToolchainRecognisesItsOwnCoverageAndMutationSteps(t *testing.T) {
	cases := []struct {
		toolchain string
		coverage  []string
		mutation  []string
		neither   []string
	}{
		{"go",
			[]string{"go test -race -coverprofile=coverage.out ./...", "go test ./... \\\n  -covermode=atomic", "gotestsum -- -cover ./..."},
			[]string{"gremlins unleash --diff origin/main", "go-mutesting ./..."},
			[]string{"go test -race ./...", "go vet ./..."}},
		{"node",
			[]string{"npx vitest run --coverage", "pnpm test -- --coverage", "yarn jest --collect-coverage", "npm run test:coverage", "c8 node --test", "pnpm coverage"},
			[]string{"npx stryker run --incremental"},
			[]string{"npm test", "pnpm run build", "npx vitest run"}},
		{"python",
			[]string{"pytest --cov=app --cov-report=xml", "uv run pytest --cov", "coverage run -m pytest", "python -m coverage xml"},
			[]string{"mutmut run", "cosmic-ray exec config.toml session.sqlite"},
			[]string{"pytest -q", "npx vitest run --coverage"}},
		{"jvm",
			[]string{"./gradlew test jacocoTestReport", "./gradlew koverXmlReport", "mvn verify jacoco:report"},
			[]string{"./gradlew pitest", "mvn org.pitest:pitest-maven:mutationCoverage"},
			[]string{"./gradlew test", "mvn -B verify"}},
		{"scala",
			[]string{"sbt clean coverage test coverageReport"},
			[]string{"sbt stryker"},
			[]string{"sbt test"}},
		{"dotnet",
			[]string{`dotnet test --collect:"XPlat Code Coverage"`, "dotnet test /p:CollectCoverage=true", "dotnet test --coverage --coverage-output-format cobertura"},
			[]string{"dotnet stryker --since:main"},
			[]string{"dotnet test --no-build"}},
		{"rust",
			[]string{"cargo llvm-cov --lcov --output-path lcov.info", "cargo tarpaulin --out Xml"},
			[]string{"cargo mutants --in-diff git.diff"},
			[]string{"cargo test --all-features", "cargo clippy"}},
		{"ruby",
			[]string{"COVERAGE=true bundle exec rspec"},
			[]string{"bundle exec mutant run --use rspec -- 'App*'"},
			[]string{"bundle exec rspec"}},
		{"php",
			[]string{"vendor/bin/phpunit --coverage-clover coverage.xml", "XDEBUG_MODE=coverage vendor/bin/pest --coverage"},
			[]string{"vendor/bin/infection --threads=max"},
			[]string{"vendor/bin/phpunit"}},
		{"swift",
			[]string{"xcodebuild test -scheme App -enableCodeCoverage YES", "swift test --enable-code-coverage"},
			[]string{"muter --format xcode"},
			[]string{"xcodebuild test -scheme App", "swift build"}},
		{"dart",
			[]string{"flutter test --coverage", "dart run coverage:test_with_coverage"},
			[]string{"dart run mutation_test"},
			[]string{"flutter test", "dart analyze"}},
		{"elixir",
			[]string{"mix test --cover", "mix coveralls.json"},
			[]string{"mix muzak"},
			[]string{"mix test"}},
		{"cpp",
			[]string{"gcovr --print-summary --xml coverage.xml", "cmake -DCMAKE_CXX_FLAGS=--coverage -fprofile-arcs -ftest-coverage .."},
			[]string{"mull-runner ./build/tests"},
			[]string{"ctest --output-on-failure"}},
	}
	for _, tc := range cases {
		t.Run(tc.toolchain, func(t *testing.T) {
			var chain Toolchain
			for _, c := range toolchains {
				if c.ID == tc.toolchain {
					chain = c
				}
			}
			require.NotEmpty(t, chain.ID)
			one := []Toolchain{chain, {ID: "other", coverage: func(ciLine) bool { return false }, mutation: func(ciLine) bool { return false }}}

			for _, run := range tc.coverage {
				measured, _ := SplitByGate(domain.QualityGateCoverage, one, []domain.ComponentCheck{runCheck(run)})
				assert.Equal(t, []string{tc.toolchain}, names(measured), "coverage: %q", run)
			}
			for _, run := range tc.mutation {
				measured, _ := SplitByGate(domain.QualityGateMutation, one, []domain.ComponentCheck{runCheck(run)})
				assert.Equal(t, []string{tc.toolchain}, names(measured), "mutation: %q", run)
			}
			for _, run := range tc.neither {
				for _, gate := range []domain.QualityGateKind{domain.QualityGateCoverage, domain.QualityGateMutation} {
					measured, _ := SplitByGate(gate, one, []domain.ComponentCheck{runCheck(run)})
					assert.Empty(t, names(measured), "%s: %q", gate, run)
				}
			}
		})
	}
}

func TestSplitByGateIsPerLanguage(t *testing.T) {
	chains := ToolchainsFor([]string{"Go", "TypeScript"})
	checks := []domain.ComponentCheck{runCheck("go test -coverprofile=coverage.out ./...", "npx vitest run")}

	measured, missing := SplitByGate(domain.QualityGateCoverage, chains, checks)
	assert.Equal(t, []string{"go"}, names(measured))
	assert.Equal(t, []string{"node"}, names(missing))

	measured, missing = SplitByGate(domain.QualityGateMutation, chains, checks)
	assert.Empty(t, names(measured))
	assert.Equal(t, []string{"go", "node"}, names(missing))
}

func TestSplitByGateCountsALanguageFreeStepOnlyForOneToolchain(t *testing.T) {
	upload := runCheck("go test ./...")
	upload.Steps = append(upload.Steps, domain.CheckStep{Name: "Upload", Uses: "codecov/codecov-action@v5"})
	named := runCheck("make test")
	named.JobName = "Mutation tests"

	measured, _ := SplitByGate(domain.QualityGateCoverage, ToolchainsFor([]string{"Go"}), []domain.ComponentCheck{upload})
	assert.Equal(t, []string{"go"}, names(measured))
	measured, _ = SplitByGate(domain.QualityGateMutation, ToolchainsFor([]string{"Go"}), []domain.ComponentCheck{named})
	assert.Equal(t, []string{"go"}, names(measured))

	_, missing := SplitByGate(domain.QualityGateCoverage, ToolchainsFor([]string{"Go", "Python"}), []domain.ComponentCheck{upload})
	assert.Equal(t, []string{"go", "python"}, names(missing), "with two languages an upload step cannot say which one it measured")
}

func TestSplitByGateWithNoChecksMissesEveryToolchain(t *testing.T) {
	_, missing := SplitByGate(domain.QualityGateCoverage, ToolchainsFor([]string{"Rust"}), nil)
	assert.Equal(t, []string{"rust"}, names(missing))
}
