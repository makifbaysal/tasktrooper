---
key: partial.gates/rust
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Install cargo-llvm-cov (`taiki-e/install-action`) and run the tests through it: `cargo llvm-cov --lcov --output-path lcov.info`, plus `cargo llvm-cov report --summary-only` for the total. TaskTrooper's own hand-off runs the same command.
- Upload `lcov.info`.{{else}}- Install cargo-mutants (`taiki-e/install-action`) and run `cargo mutants`; on pull requests pass the diff with `--in-diff` so only changed code is mutated.
- Keep its settings in `.cargo/mutants.toml` (create it even when empty): TaskTrooper's own hand-off runs `cargo mutants` when that file exists.
- Upload `mutants.out`.{{end}}
