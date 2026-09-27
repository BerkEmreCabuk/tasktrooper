---
key: guard.test_cases_planned
version: 1
inputs: [Target, Count, Planned]
---
cannot move to {{.Target}}: {{.Count}} test case(s) are still planned and were never executed: {{join "; " .Planned}} — run each one and record its result (passed/failed), or mark it skipped with what blocked it, or invalid with why it is not a valid case
