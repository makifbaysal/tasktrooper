---
key: partial.findings/release_minification_disabled
version: 1
---
### The release build is not minified

Why it matters: Without R8/ProGuard the shipped app is larger and its code is trivially readable.

How to fix: Enable minification (and resource shrinking) for release with the keep rules the app's reflection-based libraries need, then build and smoke-test the release variant.
