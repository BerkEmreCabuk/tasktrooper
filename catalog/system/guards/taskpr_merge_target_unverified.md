---
key: guard.taskpr_merge_target_unverified
version: 1
inputs: [SHA]
---
no verified commit is stamped on this task, while its pull request is at {{.SHA}}. Move it back through review (need_revision → code_review → … → done): reaching done stamps the commit that was signed off, which is what this gate compares against
