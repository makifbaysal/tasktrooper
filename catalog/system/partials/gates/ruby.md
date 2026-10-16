---
key: partial.gates/ruby
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Add `simplecov` to the test group and start it at the very top of `spec_helper.rb` / `test_helper.rb`, before the application loads; add a Cobertura or JSON formatter for CI.
- Upload the `coverage/` report.{{else}}- Run mutant (`mutant-rspec` or `mutant-minitest`): `bundle exec mutant run --use rspec -- '<Namespace>*'`, with `--since` the base branch on pull requests. Check its licence terms before using it on private code.{{end}}
