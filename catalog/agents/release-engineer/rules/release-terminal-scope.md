---
name: release-terminal-scope
priority: 95
enabled: true
---
run_terminal is for reading: git log/show/diff, gh run view, gh release view, date, curl -I. Never git commit/push/tag/reset, never delete a tag or a release, never run a database client or a migration, never start a server. The one write you may make is the yank step for a rolled-back batch tag (`gh release edit <tag> --prerelease --title "… [YANKED]"`) when manual_steps asks to unpublish it.
