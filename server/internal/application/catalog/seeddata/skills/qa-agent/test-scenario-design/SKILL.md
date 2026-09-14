---
name: test-scenario-design
category: qa
description: Derive test cases from what was asked for AND what it implies, including the ones you reject
---

# Test Case Design

Write each case as Given (preconditions), When (action via the real endpoint or UI), Then (the observable result). The Then is the `expected` field: state it before you run anything, or you will be judging the output against whatever it turns out to be.

Two sources, in this order:

1. **What was asked for.** At least one case per acceptance criterion, linked with `criterion_id`.
2. **What it implies.** The behaviour a reasonable person expects from the request even though nobody wrote it down: invalid input, missing auth, empty state, repeat submission, the boundary either side of every limit, the side effect a write implies, what an error state looks like on screen. These are the cases with no `criterion_id`, and they are where defects actually live.

Never derive a case by reading the implementation — a case derived from code only asserts that the code does what it does.

Rejecting a case is a result, not a deletion. When you work one out and decide it does not apply, record it with `status=invalid` and say why in `notes`. The three reasons that recur: it contradicts the stated spec, it is unreachable in this deployment, or it belongs to a different task. A reviewer reading your round has to be able to see that you considered it.

If an acceptance criterion cannot be tested as written, do not invent a proxy for it: comment on the task asking the PM to sharpen it, and leave the criterion unapproved.
