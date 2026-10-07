---
key: tool.run_terminal
version: "1"
params:
    command: The shell command to execute
    working_dir: Optional working directory for the command. Defaults to the configured working directory.
---
Execute a shell command and return stdout and stderr combined. The command runs in the host machine's shell named under "Host machine" in your instructions — write it in that shell's syntax for that OS. Use for file operations, system queries, running scripts, and any terminal task. To READ a file use read_file, not cat/sed/head/tail: it returns the whole file with line numbers in one call, while a shell read costs a full agent turn per window. The command must TERMINATE on its own: it is killed at the timeout and you get an error plus partial output. Never start a dev server, watcher or any process that runs until interrupted (npm/pnpm/yarn run dev, next dev, vite, nodemon, tail -f, docker compose up). To check that code works, run the build/typecheck/test command instead; to inspect a server, start it in the background redirected to a log file under your task's own scratch directory — never the fixed `/tmp/dev.log`, which collides with every other concurrent run (in a POSIX shell: `mkdir -p /tmp/tt-<task key> && ... > /tmp/tt-<task key>/dev.log 2>&1 &`) — and then read that file.
