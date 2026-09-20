#!/bin/sh
# Stand-in for the real `agy` binary in tests.
#
# Everything it reads and writes is RELATIVE TO ITS WORKING DIRECTORY, which is
# the task workspace the executor starts it in — see fake-claude.sh in the
# claudecode package for the fuller reasoning (the same childenv scrub applies
# here). Differences from that script, both driven by AGY's own contract:
#
#   - the prompt arrives as the "-p" ARGUMENT, not on stdin: antigravity.spawn
#     sets no cmd.Stdin at all, so there is nothing to read there.
#   - AGY has no --mcp-config flag; it reads a fixed workspace-relative path
#     the executor writes ahead of the run (see mcp.go). This script copies it
#     out to mcp-config.json (mirroring claudecode's own mcp-config.json copy)
#     WHILE the session is still "running", since the executor removes the
#     original the moment Execute returns.
#
#   fixture.jsonl / fixture.<n>.jsonl   what to write to stdout
#   stderr.txt / stderr.<n>.txt         optional, echoed to stderr
#   exit_code                           optional, the exit status to end with
#   sleep_seconds                       optional, hang this long before answering
#   argv.txt / argv.<n>.txt             the arguments of the call, NUL-separated
#   calls.txt                           how many times it was invoked
#   env.txt                             the environment it was given, sorted
#   mcp-config.json                     a COPY of .agents/mcp_config.json, taken
#                                        while the session is still running

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

if [ -f .agents/mcp_config.json ]; then
    cp .agents/mcp_config.json mcp-config.json
fi

# sleep_seconds makes the session hang, for the run-timeout test. Its stdout is
# redirected to /dev/null so the sleeping child does NOT inherit the stream
# pipe — see fake-claude.sh for why that matters.
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
