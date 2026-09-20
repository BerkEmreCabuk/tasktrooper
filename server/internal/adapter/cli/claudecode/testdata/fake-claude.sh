#!/bin/sh
# Stand-in for the real `claude` binary in tests.
#
# Everything it reads and writes is RELATIVE TO ITS WORKING DIRECTORY, which is
# the task workspace the executor starts it in. That is not a convenience: the
# executor builds the child's environment with platform/childenv, which drops
# every variable that is not on the allowlist, so a fixture selected through an
# environment variable would never arrive. Files in the workspace are the one
# channel the scrub does not touch — and having to use it is itself a check that
# the scrub is real.
#
#   fixture.jsonl  what to write to stdout (the stream-json events)
#   stderr.txt     optional, echoed to stderr
#   exit_code      optional, the exit status to end with
#   sleep_seconds  optional, hang this long before answering
#
# A chat turn can spawn the CLI TWICE — a --resume the CLI refuses falls back to
# a fresh session — so the per-call overrides below take precedence when they
# exist, and the plain names above are the fallback for every call:
#
#   fixture.<n>.jsonl  stdout for call n (1-based)
#   stderr.<n>.txt     stderr for call n
#
# It records what it was called with, for the argument and environment
# assertions:
#
#   argv.txt       the arguments of the LAST call, NUL-separated — an argument
#                  can itself contain newlines (the flattened system prompt
#                  does), so a line-per-argument file could not be split back
#                  apart
#   argv.<n>.txt   the same, per call, for the two-spawn cases
#   stdin.txt      the prompt, which arrives on stdin rather than in argv
#   stdin.<n>.txt  the same, per call
#   system-prompt.txt  a COPY of the file --append-system-prompt-file pointed
#                  at, for the same reason mcp-config.json is a copy; removed
#                  when a call carries none, so a stale one cannot pass for it
#   system-prompt.<n>.txt  the same, per call
#   calls.txt      how many times it was invoked
#   env.txt        the environment it was given, sorted
#   mcp-config.json  a COPY of the file --mcp-config pointed at, taken while the
#                  session is still running. The executor deletes the original
#                  when the run ends (a bearer token must not outlive its run),
#                  so a test that wants to see what the session was handed has to
#                  read it from in here, exactly as the real CLI would.

# Call counter. The workspace is a fresh t.TempDir per test, so it starts at 1
# on its own with no reset needed.
calls=1
if [ -f calls.txt ]; then
    calls=$(( $(cat calls.txt) + 1 ))
fi
printf '%s' "$calls" > calls.txt

for arg in "$@"; do
    printf '%s\0' "$arg"
done > argv.txt
cp argv.txt "argv.$calls.txt"

cat > stdin.txt
cp stdin.txt "stdin.$calls.txt"

env | sort > env.txt

rm -f system-prompt.txt
prev=""
for arg in "$@"; do
    if [ "$prev" = "--mcp-config" ] && [ -f "$arg" ]; then
        cat "$arg" > mcp-config.json
    fi
    if [ "$prev" = "--append-system-prompt-file" ] && [ -f "$arg" ]; then
        cat "$arg" > system-prompt.txt
        cp system-prompt.txt "system-prompt.$calls.txt"
    fi
    prev="$arg"
done

# sleep_seconds makes the session hang, for the run-timeout test. Its stdout is
# redirected to /dev/null so the sleeping child does NOT inherit the stream
# pipe: otherwise killing the shell would leave the pipe held open by a process
# nobody is waiting for, and the reader would block until the sleep ended
# instead of seeing EOF when the session was killed.
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
