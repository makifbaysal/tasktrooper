---
key: partial.findings/no_crash_reporting
version: 1
---
### Crashes and client errors are not reported

Why it matters: Without crash or error reporting, problems that users hit in production are invisible until someone complains.

How to fix: Integrate the team's error tracker (Sentry, Crashlytics, Bugsnag, Datadog RUM or similar) with release and version tagging and personal data scrubbed. If the team already reports errors another way, the finding can be accepted.
