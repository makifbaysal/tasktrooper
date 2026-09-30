#!/bin/sh
# Stand-in for an opencode 2.x binary. `--version` answers the 2.x way,
# `serve` plays the private server the executor starts for each run, and
# everything else is fake-opencode.sh's run.
#
# serve reads and writes, relative to its working directory (the workspace):
#
#   serve_url.txt       the address to report as ready, a stand-in API server
#   serve_argv.txt      its arguments, NUL-separated
#   serve_env.txt       its environment, sorted
#   serve_stopped       written once its stdin closed, i.e. its lease ended

case "$1" in
--version)
    echo "opencode v2.0.13"
    exit 0
    ;;
serve)
    for arg in "$@"; do
        printf '%s\0' "$arg"
    done > serve_argv.txt
    env | sort > serve_env.txt
    printf '{"url":"%s"}\n' "$(cat serve_url.txt)"
    cat > /dev/null
    : > serve_stopped
    exit 0
    ;;
esac

exec "$(dirname "$0")/fake-opencode.sh" "$@"
