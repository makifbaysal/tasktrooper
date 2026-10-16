---
key: partial.gates/cpp
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Build the test target with coverage on (GCC/Clang `--coverage`, or Clang `-fprofile-instr-generate -fcoverage-mapping`), behind a build option rather than in the release flags.
- Report with `gcovr --print-summary --xml coverage.xml` (or `llvm-cov report`) and upload the XML.{{else}}- Run Mull (`mull-runner` on the Clang-built test binary), configured in `mull.yml`, and upload its report.{{end}}
