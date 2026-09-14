#!/bin/sh
# Stand-in for the `gh` CLI used by the no-token EnsurePullRequest fallback in
# client_test.go. Every invocation is appended to $GH_FAKE_DIR/gh-argv.txt so
# the test can assert on the exact flags `gh pr create` was called with.
# `gh pr view` reports no PR on its first call (mirroring a branch with no PR
# yet) and the created PR's URL on every call after `gh pr create` runs, since
# EnsurePullRequest reads the URL back that way once the PR exists.
dir="${GH_FAKE_DIR:-.}"
printf '%s\n' "$*" >>"$dir/gh-argv.txt"

if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
    if [ -f "$dir/pr-created" ]; then
        printf 'https://github.com/acme/widgets/pull/1\n'
        exit 0
    fi
    exit 1
fi

if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
    touch "$dir/pr-created"
    exit 0
fi

exit 1
