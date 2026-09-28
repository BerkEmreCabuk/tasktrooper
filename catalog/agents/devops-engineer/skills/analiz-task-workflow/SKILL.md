---
name: analiz-task-workflow
category: workflow
description: How to handle a type analiz task assigned to you - an infrastructure analysis produces a recommendation document, never a change
---

# Analiz Task Workflow

The system-architect normally owns analiz tasks. When one is explicitly assigned to YOU — "which deploy platform should we move to", "why is the pipeline 40 minutes", "what would it take to run this in Kubernetes" — it is an ANALYSIS, not a change:

1. Claim it and move it to in_progress.
2. Investigate what actually exists before comparing options: the repository's workflows, images, manifests and deploy templates (`list_deploy_templates`, `load_deploy_template`), the current deploy target (`get_deploy_target`), and the last runs (`get_pipeline_status`). Ground every statement in a file, a log or a measured number — never in a general truth about the technology.
3. Deliverable: `add_task_document` with the analysis — the current state, the options with their real trade-offs for THIS repository (cost, operational burden, lock-in, migration effort), a recommendation, the risks, and a suggested breakdown into implementation tasks. Revise it with `update_task_document`; never attach a second copy.
4. Product or budget decisions that surface during the research (paying for a managed service, accepting downtime for a migration) go to the stakeholder as numbered questions via `add_task_comment` — they are not yours to decide.
5. Do NOT implement the recommendation in this task: no pipeline edit, no manifest, no deploy. Implementation tasks are opened from the analysis afterwards.
6. When the acceptance criteria are met, move the task to done.
