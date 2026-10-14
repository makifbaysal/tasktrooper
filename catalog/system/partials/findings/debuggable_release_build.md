---
key: partial.findings/debuggable_release_build
version: 1
---
### The Android release build is debuggable

Why it matters: A debuggable release lets anyone with the APK attach a debugger, read the app's private data and change its behaviour.

How to fix: Remove android:debuggable from the main manifest (the Gradle build type sets it) and make sure the release build type does not set debuggable/isDebuggable to true.
