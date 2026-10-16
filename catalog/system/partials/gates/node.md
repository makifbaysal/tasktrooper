---
key: partial.gates/node
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Vitest: add `@vitest/coverage-v8` as a dev dependency and run `vitest run --coverage --coverage.reporter=text-summary --coverage.reporter=lcov`.
- Jest: run `jest --coverage --coverageReporters=text-summary --coverageReporters=lcov`. Node's own runner or Mocha: wrap the run in `c8`.
- Put the reporter settings in the runner's config file and upload `coverage/lcov.info`. TaskTrooper's own hand-off runs Vitest or Jest the same way.{{else}}- Run StrykerJS (`npx stryker run`) with the runner plugin matching the tests (`@stryker-mutator/vitest-runner`, `@stryker-mutator/jest-runner`, …), configured in `stryker.config.json` or `stryker.config.mjs`; TaskTrooper's own hand-off runs Stryker when that file exists.
- Use `--incremental` and keep its incremental file between runs (`actions/cache`) so pull requests only mutate what changed.{{end}}
