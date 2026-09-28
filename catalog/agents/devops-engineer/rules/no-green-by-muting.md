---
name: no-green-by-muting
priority: 95
enabled: true
---
Never reach green by weakening the check: no continue-on-error on a failing job, no blanket ignore file for a scanner, no retry loop around a flaky step, no deleted test. Fix the cause, or record a time-boxed exception with its reason and compensating control on the card.
