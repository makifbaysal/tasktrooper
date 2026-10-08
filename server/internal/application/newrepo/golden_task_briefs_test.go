package newrepo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGoldenBootstrapDescription pins bootstrapDescription's exact wording,
// byte-for-byte, across its Scaffold/Stack/Docs branches, before that prose
// moves into catalog/system/prompts/briefs/**.
func TestGoldenBootstrapDescription(t *testing.T) {
	t.Run("scaffold with docs and a named stack", func(t *testing.T) {
		req := domain.NewRepositoryRequest{
			Description: "A payments backend.",
			Stack:       "Go + Postgres",
			Notes:       "Needs Stripe webhooks.",
			Scaffold:    true,
			Docs:        []string{domain.RepoDocCodingStandards, domain.RepoDocArchitecture},
		}
		got := bootstrapDescription("payments-api", domain.ComponentRoleBackend, req, req.Docs, nil)

		want := "Set up the brand-new repository payments-api. It was just created empty — an initial commit and nothing else — so there is no existing code to read or follow: what this task writes sets the conventions for everything after it.\n\n" +
			"## What the person asked for\n\n" +
			"Their answers, verbatim:\n\n" +
			"**Description**\nA payments backend.\n\n" +
			"**Role**\nbackend\n\n" +
			"**Stack**\nGo + Postgres\n\n" +
			"**Notes**\nNeeds Stripe webhooks.\n\n" +
			"## What to deliver\n\n" +
			"Do ALL of it on a single branch, in exactly one pull request. Do not open a pull request per item and do not stop after the first one — the task is finished when every item below exists and is correct.\n\n" +
			"1. the initial project skeleton in the repository root\n" +
			"2. `.ai/coding-standards.md` — coding standards\n" +
			"3. `.ai/architecture.md` — architecture\n" +
			"4. `CLAUDE.md` and `AGENTS.md` at the repository root\n" +
			"\n---\n\n## The project skeleton\n\n" +
			"Create the initial project skeleton for this stack in the repository root: minimal but runnable — it installs, builds and starts (a library: builds and runs its tests) with the stack's standard commands — plus a README that says what the project is and how to install, run and test it. Nothing beyond what proves it runs.\n" +
			"Before writing it by hand, call search_boilerplate_catalog with the stack and the role; when a starter there fits, build on it instead of starting from scratch, and say in the pull request which one you used.\n" +
			"The skeleton must already follow every convention the documents below prescribe.\n" +
			"\n---\n\n## `.ai/coding-standards.md`\n\n" +
			"Prescribe the conventions for the chosen stack: the formatter and linter to use (add their config files to the repository so they actually run), naming and file-layout conventions, the error-handling style, and the few rules that matter most for this kind of project. Keep it short and concrete — rules, not a tutorial.\n" +
			"\n---\n\n## `.ai/architecture.md`\n\n" +
			"Prescribe the architecture: the layers or modules and the direction of dependencies between them, where each kind of code goes in the directory layout, the data flow for a typical request or job, and the boundaries that must not be crossed.\n" +
			"\n---\n\n## `CLAUDE.md` and `AGENTS.md`\n\n" +
			"Create both at the repository root, or refresh them if the skeleton already produced one: a one-paragraph summary of what this project is, then a short docs index linking every document written in this pull request and the README, so every agent that opens this repository finds them. Keep the two files' content the same.\n"
		require.Equal(t, want, got)
	})

	t.Run("no scaffold, no docs, no stack named", func(t *testing.T) {
		req := domain.NewRepositoryRequest{Scaffold: false}
		got := bootstrapDescription("scratch-lib", domain.ComponentRoleLibrary, req, nil, nil)

		want := "Set up the brand-new repository scratch-lib. It was just created empty — an initial commit and nothing else — so there is no existing code to read or follow: what this task writes sets the conventions for everything after it.\n\n" +
			"## What the person asked for\n\n" +
			"Their answers, verbatim:\n\n" +
			"**Description**\n(not given)\n\n" +
			"**Role**\nlibrary\n\n" +
			"**Stack**\n(not given)\n\n" +
			"**Notes**\n(not given)\n\n" +
			"No stack was named: choose a mainstream, well-supported stack for a library project that fits the description, and state the choice and the reason in the README.\n\n" +
			"## What to deliver\n\n" +
			"Do ALL of it on a single branch, in exactly one pull request. Do not open a pull request per item and do not stop after the first one — the task is finished when every item below exists and is correct.\n\n" +
			"1. `CLAUDE.md` and `AGENTS.md` at the repository root\n" +
			"\n---\n\n## `CLAUDE.md` and `AGENTS.md`\n\n" +
			"Create both at the repository root, or refresh them if the skeleton already produced one: a one-paragraph summary of what this project is, then a short docs index linking every document written in this pull request, so every agent that opens this repository finds them. Keep the two files' content the same.\n"
		require.Equal(t, want, got)
	})

	t.Run("docs only, no scaffold", func(t *testing.T) {
		req := domain.NewRepositoryRequest{Description: "A worker.", Scaffold: false, Docs: []string{domain.RepoDocTestStandards}}
		got := bootstrapDescription("jobs-worker", domain.ComponentRoleWorker, req, req.Docs, nil)

		want := "Set up the brand-new repository jobs-worker. It was just created empty — an initial commit and nothing else — so there is no existing code to read or follow: what this task writes sets the conventions for everything after it.\n\n" +
			"## What the person asked for\n\n" +
			"Their answers, verbatim:\n\n" +
			"**Description**\nA worker.\n\n" +
			"**Role**\nworker\n\n" +
			"**Stack**\n(not given)\n\n" +
			"**Notes**\n(not given)\n\n" +
			"No stack was named: choose a mainstream, well-supported stack for a worker project that fits the description, and state the choice and the reason in the README.\n\n" +
			"## What to deliver\n\n" +
			"Do ALL of it on a single branch, in exactly one pull request. Do not open a pull request per item and do not stop after the first one — the task is finished when every item below exists and is correct.\n\n" +
			"1. `.ai/test-standards.md` — test standards\n" +
			"2. `CLAUDE.md` and `AGENTS.md` at the repository root\n" +
			"\n---\n\n## `.ai/test-standards.md`\n\n" +
			"Prescribe how this project is tested: the test runner and the exact command to run it (wire it up, with at least one passing example test if there is code to test), which layers to use (unit/integration/e2e — pick what fits this stack), where test files live and how they are named, and how a new feature's tests should be structured.\n" +
			"\n---\n\n## `CLAUDE.md` and `AGENTS.md`\n\n" +
			"Create both at the repository root, or refresh them if the skeleton already produced one: a one-paragraph summary of what this project is, then a short docs index linking every document written in this pull request, so every agent that opens this repository finds them. Keep the two files' content the same.\n"
		require.Equal(t, want, got)
	})
}
