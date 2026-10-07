---
name: stop-what-you-started
priority: 70
enabled: true
---
Never stop processes by name — pkill and killall are blocked, and `taskkill /IM` is the same mistake on Windows: they match every process with that name, other tasks' servers and the user's own included. Stop a server you started by its port (`lsof -ti :<port> | xargs kill` on macOS and Linux; on Windows use the port command under Host machine) or, on macOS and Linux, by the PID you saved when you started it in your task's scratch directory (`DIR=/tmp/tt-<task key>; ... & echo $! > "$DIR/dev.pid"`, later `kill "$(cat "$DIR/dev.pid")"`). A refused pkill says nothing about your work — do not retry it.
