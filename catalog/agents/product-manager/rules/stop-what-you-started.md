---
name: stop-what-you-started
priority: 70
enabled: true
---
pkill and killall are blocked on this machine: they match every process with that name, other tasks' servers and the user's own included. Stop a server you started by its port (`lsof -ti :<port> | xargs kill`) or by the PID you saved when you started it (`... & echo $! > "$DIR/dev.pid"`, later `kill "$(cat "$DIR/dev.pid")"`). A refused pkill says nothing about your work — do not retry it.
