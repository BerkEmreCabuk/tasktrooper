#!/bin/sh
# Stand-in for `xcrun` in the localdevice tests.
#
# Everything it reads and writes lives in $LOCALDEVICE_FAKE_DIR — a per-test
# temp directory. An environment variable rather than the working directory
# (which is how the Claude Code fake does it) because this package does NOT
# scrub the child's environment and does not set its working directory: these
# are inventory commands run from wherever the server happens to be, so the
# temp dir has to be named rather than inherited.
#
#   simctl-devices.json  what `simctl list devices --json` answers
#   boot-error           optional; if present, `simctl boot` fails with its text
#   xcrun-argv.txt       every invocation, one line each, for the argument
#                        assertions — this is where "no shell interpolation"
#                        is actually proven: a metacharacter in a UDID has to
#                        arrive as one literal argument or not at all
dir="${LOCALDEVICE_FAKE_DIR:-.}"
printf '%s\n' "$*" >> "$dir/xcrun-argv.txt"

case "$2" in
    list)
        if [ -f "$dir/simctl-devices.json" ]; then
            cat "$dir/simctl-devices.json"
        else
            printf '{"devices":{}}'
        fi
        ;;
    boot)
        if [ -f "$dir/boot-error" ]; then
            cat "$dir/boot-error" >&2
            exit 1
        fi
        ;;
    bootstatus)
        if [ -f "$dir/bootstatus-error" ]; then
            cat "$dir/bootstatus-error" >&2
            exit 1
        fi
        ;;
    shutdown)
        if [ -f "$dir/shutdown-error" ]; then
            cat "$dir/shutdown-error" >&2
            exit 1
        fi
        ;;
esac
exit 0
