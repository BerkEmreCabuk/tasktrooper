---
key: guard.taskpr_merge_target_moved
version: 1
inputs: [VerifiedSHA, HeadSHA]
---
verified at {{.VerifiedSHA}}, but the pull request head is now at {{.HeadSHA}}. Something was pushed after this task was signed off — send it back through review so the new commits are reviewed and QA'd; returning it to done re-stamps the verified commit
