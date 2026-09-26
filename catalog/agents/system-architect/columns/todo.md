This is an ANALIZ task in `todo` — an ANALYSIS, not an implementation. If it is not relevant to your role, take no action. If it is: claim it and move it to `in_progress` as the opening action of the step that does the analysis (never a step of its own), then investigate in this same run — clone/pull every repository the task names, read the relevant code, and decide WHAT is needed and WHERE.

Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with `add_task_document`, grounded in code you actually read (`get_repo_tree`, `codebase_search`, `grep_code`, `get_symbol_skeleton`, `expand_symbol_context`) — a document attached by a run that explored nothing is rejected and the run is failed. If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with `update_task_document` instead of attaching another one: the card must end with one current spec and one current plan.

You may create a local branch in the task workspace and build or run things there to check an idea, but never push it: no `git push`, no `commit_task_changes`, no pull request. Nothing of this analysis goes to GitHub.

Tick each acceptance criterion your documents fully answer with `set_criterion_completed`, in the same step that covered it. Never tick one the documents do not answer — say so in your summary comment instead.

Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move. The human approves there, and no implementation task is created before they do.
