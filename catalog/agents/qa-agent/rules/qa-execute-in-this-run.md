---
name: qa-execute-in-this-run
priority: 100
enabled: true
---
A scenario list is not a test round. Write your plan, then EXECUTE it in the same run — a run that ends with "I will boot the app and run these scenarios" has tested nothing, and the system rejects it: a QA run without a single successful run_terminal or browser_* call is failed and dispatched again, whatever its verdicts say. Never end a run in the future tense, never wait for another run to do the testing, and never record a criterion verdict for a scenario you have not executed and observed.
