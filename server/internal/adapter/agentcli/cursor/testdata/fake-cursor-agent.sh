#!/bin/sh
# Stand-in for the real `cursor-agent` binary in tests. Same design as
# claudecode's testdata/fake-claude.sh — see that script's header for why
# fixtures live as FILES in the workspace rather than environment variables:
# the executor scrubs the child's environment, so anything selected through it
# would never arrive.
#
#   fixture.jsonl      stdout stream-json events for a `-p` run
#   fixture.<n>.jsonl  the same, per call (1-based)
#   stderr.txt          echoed to stderr
#   stderr.<n>.txt
#   exit_code            the exit status to end with
#   sleep_seconds         hang this long before answering (own stdout goes to
#                          /dev/null while sleeping — see fake-claude.sh)
#
# probe_test.go does not use this script: probeVersion/probeAuth pin cmd.Dir
# to os.TempDir() rather than the test's own working directory, so its own
# fakeCursorAgent helper bakes the --version/status responses directly into a
# per-test script instead of reading them from a fixture file here.
#
# The prompt arrives in argv (`-p <prompt>`), not stdin — cursor.go never sets
# cmd.Stdin — so there is no stdin.txt the way fake-claude.sh has one.
#
#   argv.txt / argv.<n>.txt   NUL-separated, since the flattened prompt can
#                              itself contain newlines
#   env.txt                    sorted
#   calls.txt                  invocation count

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

if [ -f sleep_seconds ]; then
    sleep "$(cat sleep_seconds)" >/dev/null 2>&1
fi

if [ -f "stderr.$calls.txt" ]; then
    cat "stderr.$calls.txt" >&2
elif [ -f stderr.txt ]; then
    cat stderr.txt >&2
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
