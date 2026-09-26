---
name: pm-uat-review
category: pm
description: How to conduct PM UAT review
---
# PM UAT Review

When a task reaches pm_uat:

1. Read the ORIGINAL task description, every acceptance criterion, and the human's requirement comments (injected as "The human's requirements written on this task"). The human's comments amend the description and the criteria — where they disagree the comment wins, and what it asks for needs evidence like any criterion.
2. Read QA's evidence: the `review_criterion` note on each criterion (commands run, observed outputs, screenshot paths) — that is where a passing QA round records what it executed, and it no longer duplicates it in a comment. A comment from QA exists only when something failed.
3. **Cross-check, not a substitute.** Call `list_test_cases` and, criterion by criterion, note whether it has a `passed` case linked to it (matching `criterion_id`). QA's `review_criterion=approved` note and a passed case both tell you it was tested — neither tells you it works. You still walk every criterion yourself in step 5, uncovered or not.
4. Map each AC to a piece of executed evidence. Reading source code is NOT verification — only executed evidence counts. You have no code-reading tools in this column anyway.
5. Walk EVERY open criterion's flow YOURSELF, this run — no exception for one QA already passed. Get a URL in this order, first one that applies, and never production: `get_task_preview` — a preview that is `ready` AND `built_from_pr_head` (its `open_url`); else `get_deploy_target` env="stage", if this repository has one recorded; else `start_task_preview` and use its `url` once one appears (call it again if there's no URL yet — it will not restart a preview already starting). Then `browser_navigate` → `browser_wait_for` → `browser_fill`/`browser_click` through each user-facing AC and `browser_screenshot` the outcome. Put the screenshot path in the `review_criterion` note of the criterion it proves. There is no backend exception: if the repository has nothing to click, say so in the gap list instead of approving on QA's word alone.
6. Every AC your own walk-through in step 5 confirms: approve each criterion with its evidence in the note and move the task to human_uat. Write NO comment — "PM UAT passed" says nothing the approved criteria and the column do not already say.
7. Any AC failing your own walk-through: move to need_revision. add_task_comment with a numbered gap list — quote the unmet criterion, expected vs actual, with the screenshot for visual gaps.

Never reject with vague feedback, never approve without evidence.
