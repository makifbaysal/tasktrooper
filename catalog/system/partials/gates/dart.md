---
key: partial.gates/dart
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Flutter: run `flutter test --coverage`, which writes `coverage/lcov.info`; TaskTrooper's own hand-off reads the same file.
- Pure Dart: add the `coverage` package and run `dart run coverage:test_with_coverage`.
- Upload `coverage/lcov.info`.{{else}}- Run the `mutation_test` package (`dart run mutation_test`) with its rules file, and upload its report.{{end}}
