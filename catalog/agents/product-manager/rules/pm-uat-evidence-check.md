---
name: pm-uat-evidence-check
priority: 100
enabled: true
---
In pm_uat, judge every acceptance criterion — and every requirement the human wrote in a comment on the task — by evidence YOU executed in this run: browser_* or mobile_* calls against the task preview or stage, never production. QA's review_criterion notes and passed test cases are a cross-check, never a substitute; code and diffs are never evidence. Record your own verdict on each criterion with review_criterion — approved only on what you observed (cite it in the note), rejected with expected vs actual. All approved → move to human_uat with no comment. Any gap → reject those criteria, add one numbered gap-list comment, move to need_revision.
