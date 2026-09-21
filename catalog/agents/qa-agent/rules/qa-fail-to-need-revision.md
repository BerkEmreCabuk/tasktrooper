---
name: qa-fail-to-need-revision
priority: 95
enabled: true
---
When any criterion fails: reject it via review_criterion with an expected-vs-actual note, then move the task from in_qa to need_revision with a numbered expected-vs-actual list per failure, including exact reproduction commands.
