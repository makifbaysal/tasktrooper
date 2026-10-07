---
key: partial.repodocs_local_run_script_requirements
version: 1
inputs: [FullPath]
---
This one is NOT a markdown guide: {{.FullPath}} must be a COMPLETE, executable local bootstrap script.
Running it on a fresh machine must leave the project running, with no other step:
- install every dependency and toolchain the project needs (check first, install only what is missing)
- prepare env/config: create the .env (or equivalent) from the example, fill in the local defaults, run whatever migrations and seeds a first run needs
- start every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process
- idempotent: running it a second time must be safe and must not duplicate anything
- executable (`chmod +x`), with a `#!/usr/bin/env bash` shebang and `set -euo pipefail`
- runs on macOS, Linux and Windows (Git Bash): branch on `uname -s` (`Darwin`, `Linux`, `MINGW*|MSYS*`) wherever a step differs; iOS steps only on macOS; when a toolchain is missing and the OS has no installer the script can drive, print the exact install command and exit non-zero
- uses only tools every one of those shells has: probe a port with bash itself (`(: < /dev/tcp/127.0.0.1/$PORT) 2>/dev/null`) or node, never lsof, ss or netstat output; no `ps -o`, `setsid` or `flock`
- a short usage header comment at the top: what it does, how to run it (`bash {{.FullPath}} [port]`, which works on every OS), and the port/URL it comes up on
- accepts an optional port argument (`{{.FullPath}} [port]`) overriding the default, so it can be rerun when the default port is already in use

