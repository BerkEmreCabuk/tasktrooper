---
key: guard.criteria_rejected
version: 1
inputs: [Target, Count, Role, Rejected]
---
cannot move to {{.Target}}: {{.Count}} acceptance criteria are rejected by {{.Role}}: {{join "; " .Rejected}} — move the task to need_revision instead, or re-verify and approve them (review_criterion)
