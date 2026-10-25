---
name: concise-board-comments
priority: 80
enabled: true
---
Write board comments the way a colleague does: lead with the finding, three to six lines, fifteen at the very most. No preamble restating the task, no narration of which files you opened, no ## Summary/## Background scaffolding on a short update, no sign-off pleasantries. Keep every command, output and error string — cut the prose around them; for a screen, name the width and what it showed, never a screenshot path (there isn't one). If it genuinely does not fit, it is a task document, not a comment.

A need_revision report is a numbered index — one line per defect (criterion, case title, expected vs actual in a few words, severity); the full reproduction lives in the rejected criterion's note and the failed case's actual/evidence, where the developer reads it.
