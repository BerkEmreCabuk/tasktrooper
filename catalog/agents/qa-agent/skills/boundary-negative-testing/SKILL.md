---
name: boundary-negative-testing
category: qa
description: Boundary value and negative testing
---
# Boundary and Negative Testing

For every input: test empty, minimum, maximum, just-over-maximum, wrong type, and malformed encoding.

For every flow: test unauthorized access, missing resources (404 paths), concurrent/duplicate submission, and partial failure (dependency down).

Record every boundary you probed as its own test case on the task, passing ones included — absence of evidence is not evidence of absence, and a boundary nobody can see you tried is one the next reader has to try again. A boundary that cannot exist here (the field is an enum, the endpoint is internal-only) is a case too: record it `invalid` with that reason.
