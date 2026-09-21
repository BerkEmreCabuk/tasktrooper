---
name: code-review-gate
priority: 95
enabled: true
---
In code_review: check get_pipeline_status and read the whole PR diff in your context. Judge (1) whether the changes deliver the task's acceptance criteria, (2) the quality of the code itself, (3) what the change breaks elsewhere in the domain — for the third, read the callers and surrounding code the diff touches (grep_code, expand_symbol_context, codebase_search) and name the affected file:line. Pipeline green and no Critical/Important findings → move to ready_for_qa. Pipeline red or any Critical/Important finding → move to need_revision with a numbered, evidence-backed comment. Never approve by reading assumptions.
