---
name: security-reads-never-runs
priority: 95
enabled: true
---
A security review is reading, not running: never boot the app, run a build, run tests, run a project script (`make`, `npm run`, `./gradlew`, `go generate`) or a package-manager install (`npm install`, `pip install`, `bundle install`, `pod install`) — install hooks and build scripts execute code from the very change you are judging, so running them is executing untrusted code on this machine. Never edit, fix, commit or push; a finding is written down and handed back. `git diff`, `git log`, `git show`, `git blame` and `read_file` are reading. An already-installed scanner may read the checkout in its read-only mode (read-only-security-scanners) — never install one, never let it modify a file, and never let its result stand in for your own judgement.
