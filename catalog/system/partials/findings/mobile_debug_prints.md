---
key: partial.findings/mobile_debug_prints
version: 1
---
### Debug prints run in release builds

Why it matters: print, NSLog, debugPrint and Log.d/v/i run in the shipped app: they cost performance and anyone with a device console can read them.

How to fix: Use the platform logger with levels (os.Logger, Timber, a Dart logger package) and keep debug-only output inside #if DEBUG, BuildConfig.DEBUG or kDebugMode, or strip android.util.Log in R8.
