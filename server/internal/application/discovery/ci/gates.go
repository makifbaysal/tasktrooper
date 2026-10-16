package ci

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Toolchain is one language family whose coverage and mutation tools a CI
// step is written for: TypeScript and JavaScript share one, as do Java and
// Kotlin, because one build measures both.
type Toolchain struct {
	ID        string
	Name      string
	languages []string
	coverage  func(ciLine) bool
	mutation  func(ciLine) bool
}

type ciLine struct {
	text   string
	tokens []string
}

func (l ciLine) has(subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(l.text, s) {
			return true
		}
	}
	return false
}

func (l ciLine) tokenPrefix(prefixes ...string) bool {
	for _, tok := range l.tokens {
		for _, p := range prefixes {
			if strings.HasPrefix(tok, p) {
				return true
			}
		}
	}
	return false
}

func (l ciLine) token(words ...string) bool {
	for _, tok := range l.tokens {
		for _, w := range words {
			if tok == w {
				return true
			}
		}
	}
	return false
}

var nodeRunners = []string{"npm", "npx", "pnpm", "yarn", "bun ", "bunx", "deno", "vitest", "jest", "mocha", "turbo", "nx "}

var toolchains = []Toolchain{
	{
		ID: "go", Name: "Go", languages: []string{"Go"},
		coverage: func(l ciLine) bool {
			return (l.has("go test", "gotestsum", "ginkgo") && l.tokenPrefix("-cover", "--cover")) ||
				l.has("go tool cover", "goveralls", "gocov")
		},
		mutation: func(l ciLine) bool { return l.has("gremlins", "go-mutesting") },
	},
	{
		ID: "node", Name: "TypeScript/JavaScript", languages: []string{"TypeScript", "JavaScript", "Vue", "Svelte"},
		coverage: func(l ciLine) bool {
			if l.token("c8", "nyc") {
				return true
			}
			return l.has(nodeRunners...) && (l.tokenPrefix("--coverage", "--collect-coverage", "coverage:", "test:cov", "test-cov") || l.token("coverage"))
		},
		mutation: func(l ciLine) bool { return l.has("stryker") },
	},
	{
		ID: "python", Name: "Python", languages: []string{"Python"},
		coverage: func(l ciLine) bool {
			return l.token("--cov") || l.tokenPrefix("--cov=", "--cov-") ||
				l.has("coverage run", "coverage xml", "coverage report", "coverage json", "coverage lcov", "-m coverage")
		},
		mutation: func(l ciLine) bool { return l.has("mutmut", "cosmic-ray", "cosmic_ray", "mutatest") },
	},
	{
		ID: "jvm", Name: "Java/Kotlin", languages: []string{"Java", "Kotlin"},
		coverage: func(l ciLine) bool { return l.has("jacoco", "kover", "cobertura") },
		mutation: func(l ciLine) bool { return l.has("pitest", "mutationcoverage") },
	},
	{
		ID: "scala", Name: "Scala", languages: []string{"Scala"},
		coverage: func(l ciLine) bool {
			return l.has("scoverage", "coveragereport", "jacoco") || (l.has("sbt") && l.token("coverage"))
		},
		mutation: func(l ciLine) bool { return l.has("stryker4s", "stryker") },
	},
	{
		ID: "dotnet", Name: "C#", languages: []string{"C#"},
		coverage: func(l ciLine) bool {
			return l.has("coverlet", "collectcoverage", "code coverage", "dotcover", "altcover", "dotnet-coverage") ||
				(l.has("dotnet test", "dotnet run") && l.tokenPrefix("--coverage"))
		},
		mutation: func(l ciLine) bool { return l.has("stryker") },
	},
	{
		ID: "rust", Name: "Rust", languages: []string{"Rust"},
		coverage: func(l ciLine) bool { return l.has("tarpaulin", "llvm-cov", "grcov", "kcov") },
		mutation: func(l ciLine) bool { return l.has("cargo mutants", "cargo-mutants") },
	},
	{
		ID: "ruby", Name: "Ruby", languages: []string{"Ruby"},
		coverage: func(l ciLine) bool { return l.has("simplecov", "coverage=true", "coverage=1") },
		mutation: func(l ciLine) bool {
			return l.token("mutant") || l.has("bin/mutant", "mutant-rspec", "mutant-minitest")
		},
	},
	{
		ID: "php", Name: "PHP", languages: []string{"PHP"},
		coverage: func(l ciLine) bool {
			return l.has("pcov", "xdebug_mode=coverage", "xdebug.mode=coverage", "--coverage-") ||
				(l.has("phpunit", "pest", "paratest", "artisan test") && l.tokenPrefix("--coverage"))
		},
		mutation: func(l ciLine) bool { return l.has("infection") },
	},
	{
		ID: "swift", Name: "Swift/Objective-C", languages: []string{"Swift", "Objective-C"},
		coverage: func(l ciLine) bool {
			return l.has("enablecodecoverage", "enable-code-coverage", "xccov", "slather", "xcov", "llvm-cov", "code_coverage")
		},
		mutation: func(l ciLine) bool { return l.token("muter") },
	},
	{
		ID: "dart", Name: "Dart/Flutter", languages: []string{"Dart"},
		coverage: func(l ciLine) bool {
			return (l.has("flutter test", "dart test") && l.tokenPrefix("--coverage")) ||
				l.has("format_coverage", "test_with_coverage")
		},
		mutation: func(l ciLine) bool { return l.has("mutation_test") },
	},
	{
		ID: "elixir", Name: "Elixir", languages: []string{"Elixir"},
		coverage: func(l ciLine) bool {
			return (l.has("mix test") && l.tokenPrefix("--cover")) || l.has("mix coveralls", "excoveralls")
		},
		mutation: func(l ciLine) bool { return l.has("muzak", "exavier") },
	},
	{
		ID: "cpp", Name: "C/C++", languages: []string{"C", "C++"},
		coverage: func(l ciLine) bool {
			return l.has("gcov", "llvm-cov", "fprofile-arcs", "ftest-coverage", "fprofile-instr-generate", "fcoverage-mapping") ||
				(l.has("lcov") && l.tokenPrefix("--capture", "-c"))
		},
		mutation: func(l ciLine) bool { return l.has("mull-runner", "mull-cxx", "dextool mutate") },
	},
}

// ToolchainsFor maps a component's stack languages onto toolchains, in the
// stack's order; a language no toolchain covers (Shell, SQL, …) has no
// coverage or mutation step to add and is left out.
func ToolchainsFor(languages []string) []Toolchain {
	var out []Toolchain
	seen := map[string]bool{}
	for _, lang := range languages {
		for _, tc := range toolchains {
			if seen[tc.ID] || !containsString(tc.languages, lang) {
				continue
			}
			seen[tc.ID] = true
			out = append(out, tc)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// SplitByGate separates the toolchains a CI step in checks already measures
// gate for from the ones none does. A step that names no language — a
// coverage upload action, a `make coverage` target, a job called "coverage" —
// counts only when the component has a single toolchain: with several, it
// cannot say which language it measured.
func SplitByGate(gate domain.QualityGateKind, toolchains []Toolchain, checks []domain.ComponentCheck) (measured, missing []Toolchain) {
	lines, names, uses := ciText(checks)
	generic := len(toolchains) == 1 && genericallyMeasured(gate, lines, names, uses)
	for _, tc := range toolchains {
		match := tc.coverage
		if gate == domain.QualityGateMutation {
			match = tc.mutation
		}
		if generic || anyLine(lines, match) {
			measured = append(measured, tc)
		} else {
			missing = append(missing, tc)
		}
	}
	return measured, missing
}

func anyLine(lines []ciLine, match func(ciLine) bool) bool {
	for _, l := range lines {
		if match(l) {
			return true
		}
	}
	return false
}

var genericCoverageRun = []string{"codecov", "coveralls", "make cover", "make test-cover", "make test_cover", "just cover", "task cover", "mage cover"}

func genericallyMeasured(gate domain.QualityGateKind, lines []ciLine, names, uses []string) bool {
	word := "coverage"
	runMarkers := genericCoverageRun
	useMarkers := []string{"codecov", "coveralls", "coverage", "codeclimate"}
	if gate == domain.QualityGateMutation {
		word = "mutation"
		runMarkers = []string{"mutation"}
		useMarkers = []string{"mutation", "stryker"}
	}
	for _, n := range names {
		if strings.Contains(n, word) {
			return true
		}
	}
	for _, u := range uses {
		for _, m := range useMarkers {
			if strings.Contains(u, m) {
				return true
			}
		}
	}
	return anyLine(lines, func(l ciLine) bool { return l.has(runMarkers...) })
}

// ciText lowercases every run line, name and `uses` of the checks' steps; a
// continued line is joined first, so a flag on the next line still belongs to
// its command.
func ciText(checks []domain.ComponentCheck) (lines []ciLine, names, uses []string) {
	for _, c := range checks {
		names = append(names, strings.ToLower(c.WorkflowName), strings.ToLower(c.JobKey), strings.ToLower(c.JobName))
		for _, s := range c.Steps {
			names = append(names, strings.ToLower(s.Name))
			if s.Uses != "" {
				uses = append(uses, strings.ToLower(s.Uses))
			}
			for _, text := range joinContinuations(s.Run) {
				text = strings.ToLower(strings.TrimSpace(text))
				if text == "" {
					continue
				}
				lines = append(lines, ciLine{text: text, tokens: strings.Fields(text)})
			}
		}
	}
	return lines, names, uses
}
