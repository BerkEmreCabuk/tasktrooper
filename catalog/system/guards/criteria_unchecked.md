---
key: guard.criteria_unchecked
version: 1
inputs: [Target, Count, Role, Unchecked]
---
cannot move to {{.Target}}: {{.Count}} acceptance criteria await your {{.Role}} verdict: {{join "; " .Unchecked}} — call review_criterion with each id above, then retry the move
