package repodocs_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGoldenCreateDocTaskDescription pins the exact wording of the
// single-doc task description CreateDocTask writes, byte-for-byte, before
// that prose moves into catalog/system/prompts/briefs/**. It must render
// identically once the move lands.
func TestGoldenCreateDocTaskDescription(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want string
	}{
		{
			name: "coding standards on an existing repository",
			kind: domain.RepoDocCodingStandards,
			want: "Write or refresh .ai/coding-standards.md for this backend repository. If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.\n\n" +
				"Cover: the formatting/lint tooling actually configured, naming and file-layout conventions, error-handling style, and anything this codebase does differently from a generic style guide. Read the existing code before writing — describe what it does, don't prescribe a generic standard.\n" +
				"\nAlso make sure the agent instructions file at the repository root (CLAUDE.md, AGENTS.md or the equivalent the agents on this repo read) has a short docs index; create a minimal one if it's missing, and add (or update) a line linking to this file so agents find it.\n",
		},
		{
			name: "test standards on an existing repository",
			kind: domain.RepoDocTestStandards,
			want: "Write or refresh .ai/test-standards.md for this backend repository. If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.\n\n" +
				"Cover: the test runner and how to invoke it, the testing pyramid this repo actually follows (unit/integration/e2e — only the layers that exist), coverage expectations if any, and how a new feature's tests should be structured here.\n" +
				"\nAlso make sure the agent instructions file at the repository root (CLAUDE.md, AGENTS.md or the equivalent the agents on this repo read) has a short docs index; create a minimal one if it's missing, and add (or update) a line linking to this file so agents find it.\n",
		},
		{
			name: "architecture on an existing repository",
			kind: domain.RepoDocArchitecture,
			want: "Write or refresh .ai/architecture.md for this backend repository. If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.\n\n" +
				"Cover: the major components/layers and how they depend on each other, the data flow for a typical request or task, and the boundaries that must not be crossed (e.g. hexagonal layering, module isolation).\n" +
				"\nAlso make sure the agent instructions file at the repository root (CLAUDE.md, AGENTS.md or the equivalent the agents on this repo read) has a short docs index; create a minimal one if it's missing, and add (or update) a line linking to this file so agents find it.\n",
		},
		{
			name: "local run script has no read-first framing and a different closing sentence",
			kind: domain.RepoDocLocalRun,
			want: "Write or refresh scripts/dev.sh for this backend repository.\n\n" +
				"This one is NOT a markdown guide: scripts/dev.sh must be a COMPLETE, executable local bootstrap script.\n" +
				"Running it on a fresh machine must leave the project running, with no other step:\n" +
				"- install every dependency and toolchain the project needs (check first, install only what is missing)\n" +
				"- prepare env/config: create the .env (or equivalent) from the example, fill in the local defaults, run whatever migrations and seeds a first run needs\n" +
				"- start every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process\n" +
				"- idempotent: running it a second time must be safe and must not duplicate anything\n" +
				"- executable (`chmod +x`), with a `#!/usr/bin/env bash` shebang and `set -euo pipefail`\n" +
				"- runs on macOS, Linux and Windows (Git Bash): branch on `uname -s` (`Darwin`, `Linux`, `MINGW*|MSYS*`) wherever a step differs; iOS steps only on macOS; when a toolchain is missing and the OS has no installer the script can drive, print the exact install command and exit non-zero\n" +
				"- uses only tools every one of those shells has: probe a port with bash itself (`(: < /dev/tcp/127.0.0.1/$PORT) 2>/dev/null`) or node, never lsof, ss or netstat output; no `ps -o`, `setsid` or `flock`\n" +
				"- a short usage header comment at the top: what it does, how to run it (`bash scripts/dev.sh [port]`, which works on every OS), and the port/URL it comes up on\n" +
				"- accepts an optional port argument (`scripts/dev.sh [port]`) overriding the default, so it can be rerun when the default port is already in use\n" +
				"Everything in it must match what this repository actually needs today — its real package manager, build tool and ports — not a generic template. Do not write a markdown guide instead of, or alongside, the script.\n" +
				"\nAlso make sure the agent instructions file at the repository root (CLAUDE.md, AGENTS.md or the equivalent the agents on this repo read) has a short docs index; create a minimal one if it's missing, and add (or update) a line linking to this file so agents find it.\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
			_, err := svc.CreateDocTask(context.Background(), repos.repo.ID, "", tc.kind, "")
			require.NoError(t, err)
			require.Len(t, tasks.created, 1)
			require.Equal(t, tc.want, tasks.created[0].Description)
		})
	}
}

// TestGoldenCreateDocsBundleTaskDescription pins CreateDocsBundleTask's
// exact bundle wording (bundleDescription).
func TestGoldenCreateDocsBundleTaskDescription(t *testing.T) {
	svc, repos, tasks := newFixture(t, domain.Repository{Kind: domain.RepoKindBackend})
	_, err := svc.CreateDocsBundleTask(context.Background(), repos.repo.ID, []repodocs.DocItem{
		{Kind: domain.RepoDocCodingStandards},
		{Kind: domain.RepoDocArchitecture},
	})
	require.NoError(t, err)
	require.Len(t, tasks.created, 1)

	want := "Author the reference docs listed below for this backend repository, ALL of them in a single branch so exactly one pull request contains every file.\n\n" +
		"Do not open a pull request per document, and do not stop after the first one — the task is finished when every path below exists and is correct.\n\n" +
		"1. `.ai/coding-standards.md` — coding standards for the repository (backend)\n" +
		"2. `.ai/architecture.md` — architecture for the repository (backend)\n" +
		"\n---\n\n## `.ai/coding-standards.md`\n\n" +
		"If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.\n" +
		"Cover: the formatting/lint tooling actually configured, naming and file-layout conventions, error-handling style, and anything this codebase does differently from a generic style guide. Read the existing code before writing — describe what it does, don't prescribe a generic standard.\n" +
		"\n---\n\n## `.ai/architecture.md`\n\n" +
		"If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.\n" +
		"Cover: the major components/layers and how they depend on each other, the data flow for a typical request or task, and the boundaries that must not be crossed (e.g. hexagonal layering, module isolation).\n" +
		"\n---\n\nAlso make sure the agent instructions file at the repository root (CLAUDE.md, AGENTS.md or the equivalent the agents on this repo read) has a short docs index, and that it links to every file above so agents find them; create a minimal one if it is missing.\n"
	require.Equal(t, want, tasks.created[0].Description)
}

// TestGoldenNewRepoDocInstructions pins NewRepoDocInstructions (the
// no-code-yet counterpart of docInstructions), used by the newrepo package.
func TestGoldenNewRepoDocInstructions(t *testing.T) {
	cases := []struct {
		name string
		kind string
		path string
		want string
	}{
		{
			name: "coding standards",
			kind: domain.RepoDocCodingStandards,
			path: ".ai/coding-standards.md",
			want: "Prescribe the conventions for the chosen stack: the formatter and linter to use (add their config files to the repository so they actually run), naming and file-layout conventions, the error-handling style, and the few rules that matter most for this kind of project. Keep it short and concrete — rules, not a tutorial.\n",
		},
		{
			name: "test standards",
			kind: domain.RepoDocTestStandards,
			path: ".ai/test-standards.md",
			want: "Prescribe how this project is tested: the test runner and the exact command to run it (wire it up, with at least one passing example test if there is code to test), which layers to use (unit/integration/e2e — pick what fits this stack), where test files live and how they are named, and how a new feature's tests should be structured.\n",
		},
		{
			name: "architecture",
			kind: domain.RepoDocArchitecture,
			path: ".ai/architecture.md",
			want: "Prescribe the architecture: the layers or modules and the direction of dependencies between them, where each kind of code goes in the directory layout, the data flow for a typical request or job, and the boundaries that must not be crossed.\n",
		},
		{
			name: "local run",
			kind: domain.RepoDocLocalRun,
			path: "scripts/dev.sh",
			want: "This one is NOT a markdown guide: scripts/dev.sh must be a COMPLETE, executable local bootstrap script.\n" +
				"Running it on a fresh machine must leave the project running, with no other step:\n" +
				"- install every dependency and toolchain the project needs (check first, install only what is missing)\n" +
				"- prepare env/config: create the .env (or equivalent) from the example, fill in the local defaults, run whatever migrations and seeds a first run needs\n" +
				"- start every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process\n" +
				"- idempotent: running it a second time must be safe and must not duplicate anything\n" +
				"- executable (`chmod +x`), with a `#!/usr/bin/env bash` shebang and `set -euo pipefail`\n" +
				"- runs on macOS, Linux and Windows (Git Bash): branch on `uname -s` (`Darwin`, `Linux`, `MINGW*|MSYS*`) wherever a step differs; iOS steps only on macOS; when a toolchain is missing and the OS has no installer the script can drive, print the exact install command and exit non-zero\n" +
				"- uses only tools every one of those shells has: probe a port with bash itself (`(: < /dev/tcp/127.0.0.1/$PORT) 2>/dev/null`) or node, never lsof, ss or netstat output; no `ps -o`, `setsid` or `flock`\n" +
				"- a short usage header comment at the top: what it does, how to run it (`bash scripts/dev.sh [port]`, which works on every OS), and the port/URL it comes up on\n" +
				"- accepts an optional port argument (`scripts/dev.sh [port]`) overriding the default, so it can be rerun when the default port is already in use\n" +
				"Everything in it must match the chosen stack and what this pull request adds — the real package manager, build tool and ports — not a generic template. Do not write a markdown guide instead of, or alongside, the script.\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := repodocs.NewRepoDocInstructions(tc.kind, tc.path)
			require.Equal(t, tc.want, got)
		})
	}
}
