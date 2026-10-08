---
name: no-comments-in-code
priority: 100
enabled: true
---
Do not add comments that explain WHAT the code does — names and structure say that. Allowed: a WHY for a non-obvious invariant or workaround, compiler and tool directives (`//go:build`, `//go:generate`, `//go:embed`, `//nolint:<linter> // reason`, `-- +goose …`), and doc comments the repository's linter requires.
