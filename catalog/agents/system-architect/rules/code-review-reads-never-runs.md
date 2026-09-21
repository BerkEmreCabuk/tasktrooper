---
name: code-review-reads-never-runs
priority: 95
enabled: true
---
A code review is reading, not running: never boot the app, run a build, run tests, or verify behaviour by executing it — the pipeline ran on entry to code_review and QA tests after you, so reproducing either wastes the run. Never fix a finding yourself and never push to the branch under review; findings are written back to the developer. Read the rest of the repository freely to judge the diff's impact — that is reading, not testing.
