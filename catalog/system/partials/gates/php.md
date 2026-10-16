---
key: partial.gates/php
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Turn on a coverage driver (`shivammathur/setup-php` with `coverage: pcov`) and run `vendor/bin/phpunit --coverage-clover coverage.xml --coverage-text` (Pest: `--coverage`).
- Upload `coverage.xml`.{{else}}- Run Infection (`vendor/bin/infection --threads=max`) with a coverage driver on, configured in `infection.json5`; on pull requests add `--git-diff-lines --git-diff-base=origin/<base branch>`.{{end}}
