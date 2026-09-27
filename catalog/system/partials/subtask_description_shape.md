---
key: partial.subtask_description_shape
version: "1"
---
Shape of a subtask description (implementation work):
Write the description as ordered phases the agent works top to bottom, and state the boundary explicitly. Phases are prose inside ONE subtask — never separate subtasks, never separate parallel_groups:
1. Scope — the change to make, named concretely: which files, endpoints, screens or components. If the exact location must be discovered, say which tools find it.
2. Out of scope — what this subtask must NOT touch, stated as plainly as the scope. Name the neighbouring code, the unrelated bugs, the refactors and the dependency or config changes it must leave alone. A subtask with no boundary is how a small change becomes a diff nobody can review.
3. Verify — the build and test commands to run before finishing, and what output counts as passing. Never "test it": name the commands, or say the agent must find them in package.json / Makefile / go.mod / the README. Red output is fixed inside this same subtask.
4. Close — tick every acceptance criterion the verified change satisfies, leave the rest open with a reason, and report what changed plus the command output that proved it.
Do NOT plan phases for pulling the repository, creating the branch, claiming the task, moving columns or opening the pull request: the system does all of those around the run. The agent's phases start at the code and end at the evidence.
