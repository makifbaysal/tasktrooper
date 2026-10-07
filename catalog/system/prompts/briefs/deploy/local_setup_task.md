---
key: briefs.deploy.local_setup_task
version: 1
inputs: [ScriptPath, Kind]
---
Write {{.ScriptPath}}: a COMPLETE, executable bootstrap script that gets this {{.Kind}} running on a developer's own machine. A script, not a markdown guide — do not write one.

Definition of done — running it once on a fresh machine leaves the project running, with no other step:
- installs every dependency and toolchain the project needs (checks first, installs only what is missing)
- prepares env/config: creates the .env (or equivalent) from the example with local defaults, runs the migrations and seeds a first run needs
- starts every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process
- idempotent: a second run is safe and duplicates nothing
- executable (`chmod +x`), `#!/usr/bin/env bash`, `set -euo pipefail`
- runs on macOS, Linux and Windows (Git Bash): branch on `uname -s` (`Darwin`, `Linux`, `MINGW*|MSYS*`) wherever a step differs — package manager, starting a service, paths. When a toolchain is missing and the OS has no installer the script can drive (no brew, apt or winget), print the exact install command and exit non-zero instead of guessing
- uses only tools every one of those shells has: probe a port with bash itself (`(: < /dev/tcp/127.0.0.1/$PORT) 2>/dev/null`) or node, never by parsing lsof, ss or netstat; no `ps -o`, `setsid` or `flock`, which Git Bash lacks
- a short usage header comment at the top: what it does, how to run it (`bash {{.ScriptPath}} [port]`, which works on every OS — the executable bit means nothing on Windows), and the port/URL it comes up on
- accepts an optional port argument (`{{.ScriptPath}} [port]`) overriding the default, so it can be rerun when the default port is taken
- matches what the repo actually needs today (its real package manager, build tool, ports) — not a generic template
{{if eq .Kind "mobile"}}- covers both iOS (simulator) and Android (emulator) if the repo ships both platforms; the iOS steps run only on macOS (`uname -s` is `Darwin`) — elsewhere they are skipped with a message and Android still comes up
{{else if eq .Kind "worker"}}- starts any queue/broker the worker needs locally (or configures a fake/in-memory mode when there is none to start)
{{end}}