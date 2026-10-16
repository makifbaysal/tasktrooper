---
key: partial.gates/jvm
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Gradle: apply the `jacoco` plugin and make `jacocoTestReport` (XML on) run after `test`; for Kotlin, Kover (`org.jetbrains.kotlinx.kover`, `koverXmlReport`) is the native choice.
- Maven: add `jacoco-maven-plugin` with the `prepare-agent` and `report` goals.
- Upload the XML report.{{else}}- Run PIT: the `info.solidsoft.pitest` Gradle plugin (`pitest` task) or `org.pitest:pitest-maven` (`mutationCoverage` goal), with `pitest-junit5-plugin` for JUnit 5 tests.
- Limit `targetClasses` to the component's own packages, and on pull requests use its history or changed-code options so only changed classes are mutated.{{end}}
