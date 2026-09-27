---
key: tool.save_memory
version: "1"
params:
    category: Optional category (e.g. preference, lesson, convention, feedback)
    content: 'One concise, self-contained fact or lesson, stated so it still reads true a month from now: no task key, no PR number, no commit SHA, no "this task", no column move. A note that fails that is rejected with the reason.'
    scope: project = bound to the current repository (default when one is in play); global = valid across every repository
    shared: true = team memory visible to all agents; false (default) = your own memory
---
Save a fact a LATER run — on a different task, weeks from now — will need and could not work out for itself. Before saving, apply that test: if the note stops being true once this task is finished, it is not a memory. Progress on the card you are on, what you verified, which commit fixed what, why a check went red, what you moved where: that is the task's story and belongs in its comments (add_task_comment), which is where people and later runs look for it. A memory never names a task key, a PR number, a commit SHA or a column move — the durable version of the same lesson is that sentence with the card taken out of it. Reusable know-how (a procedure you would follow again) is a skill, not a memory; this tool routes those to the skill catalog for you. Two independent choices decide where it lands: scope (project = only valid inside the repository you are working in, e.g. its build command, its architecture quirks; global = valid everywhere, e.g. a user preference or a habit you want to keep) and shared (false = your own memory, true = team memory every agent reads). Default to scope=project while working on a repository — a lesson learned in one codebase is usually wrong in another. Team memory (shared=true) is read by every agent on every run, so it holds only what the whole team would act on: raise the bar there, do not narrate.
