#!/bin/sh
# Stand-in for the real `opencode` binary in tests.
#
# Everything it reads and writes is RELATIVE TO ITS WORKING DIRECTORY, which is
# the task workspace the executor starts it in — the same reason
# claudecode/testdata/fake-claude.sh works this way: the executor's childEnv
# only ever carries PATH and HOME, so a fixture selected through an
# environment variable would never arrive.
#
#   fixture.jsonl      what to write to stdout (the JSONL event stream)
#   stderr.txt         optional, echoed to stderr BEFORE sleep_seconds — unlike
#                       fake-claude.sh's ordering, this script writes stderr
#                       first on purpose: the rateLimitWatcher test needs the
#                       CLI's error line to arrive while the process is still
#                       "hanging" in sleep, the way the real known-bug opencode
#                       process prints its provider error and then never exits.
#   exit_code           optional, the exit status to end with
#   sleep_seconds        optional, hang this long after stderr and before stdout
#
#   fixture.<n>.jsonl / stderr.<n>.txt   per-call overrides (1-based), for
#                       tests that spawn the fake CLI more than once
#
# It records what it was called with:
#
#   argv.txt / argv.<n>.txt   the arguments of the call, NUL-separated — the
#                       prompt is a positional argument here (opencode run
#                       takes it that way, unlike claudecode's stdin) and can
#                       itself contain newlines
#   calls.txt           how many times it was invoked
#   env.txt              the environment it was given, sorted — this is also
#                       where OPENCODE_CONFIG_CONTENT shows up, since the
#                       executor sets it directly on cmd.Env rather than
#                       writing it to a file

# The executor asks for the version (in a scratch dir) before its first run;
# 1.x prints the bare version.
if [ "$1" = "--version" ]; then
    echo "1.18.33"
    exit 0
fi

calls=1
if [ -f calls.txt ]; then
    calls=$(( $(cat calls.txt) + 1 ))
fi
printf '%s' "$calls" > calls.txt

for arg in "$@"; do
    printf '%s\0' "$arg"
done > argv.txt
cp argv.txt "argv.$calls.txt"

env | sort > env.txt

if [ -f "stderr.$calls.txt" ]; then
    cat "stderr.$calls.txt" >&2
elif [ -f stderr.txt ]; then
    cat stderr.txt >&2
fi

# sleep_seconds' own stdout/stderr are redirected away from the pipes this
# script inherited, so a run cancelled mid-sleep does not leave sleep holding
# the stdout pipe open after the shell above it has been killed — see
# fake-claude.sh's identical note.
if [ -f sleep_seconds ]; then
    sleep "$(cat sleep_seconds)" >/dev/null 2>&1
fi

if [ -f "fixture.$calls.jsonl" ]; then
    cat "fixture.$calls.jsonl"
elif [ -f fixture.jsonl ]; then
    cat fixture.jsonl
fi

if [ -f exit_code ]; then
    exit "$(cat exit_code)"
fi
exit 0
