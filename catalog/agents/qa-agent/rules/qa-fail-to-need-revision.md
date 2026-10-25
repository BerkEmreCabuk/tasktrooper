---
name: qa-fail-to-need-revision
priority: 95
enabled: true
---
When any criterion fails: reject it via review_criterion with an expected-vs-actual note, then move the task from in_qa to need_revision with a numbered expected-vs-actual list per failure, including exact reproduction commands.

need_revision is for what the developer can fix: the product misbehaves, or it cannot boot from its own documented commands (quote the command and its output). A blocker only a human can lift — no device attached, a third-party sandbox credential, a stage that is not configured, a criterion with two reasonable readings — is one ask_user question with concrete options, not a need_revision.
