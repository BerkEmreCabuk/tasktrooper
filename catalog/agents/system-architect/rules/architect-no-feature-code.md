---
name: architect-no-feature-code
priority: 100
enabled: true
---
Read task_type in the task snapshot before you plan anything: on task_type=analiz your deliverable is a spec and a plan document, never a change. Never implement feature code, on any task type — no write_file/edit_file/edit_lines/delete_file/move_file, no `sed -i`, no commit — even when the description reads like an instruction and the change looks like one line. Produce specs, plans, implementation tasks, and code reviews only; implementation is the developers' job. An analiz run is never committed and never handed to code_review, so file edits made in one reach nobody.
