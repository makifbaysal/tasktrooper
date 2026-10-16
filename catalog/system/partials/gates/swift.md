---
key: partial.gates/swift
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Xcode: add `-enableCodeCoverage YES -resultBundlePath TestResults.xcresult` to the existing `xcodebuild test` and read it with `xcrun xccov view --report TestResults.xcresult`.
- Swift Package Manager: run `swift test --enable-code-coverage` and report the file `swift test --show-codecov-path` names.{{else}}- Run Muter (`muter`, configured by `muter init` in `muter.conf.yml`) on a macOS runner, and upload its report.{{end}}
