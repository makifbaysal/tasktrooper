---
key: partial.findings/no_linter_configured
version: 1
---
### No linter is configured

Why it matters: Without a linter, style drift and common bug patterns are caught only in review, if at all.

How to fix: Add the ecosystem's standard linter (ESLint or Biome, Ruff, golangci-lint, SwiftLint, ktlint or detekt, RuboCop, PHPStan) with its recommended rules and a lint command. Fix what is quick; baseline the rest instead of mass-disabling rules.
