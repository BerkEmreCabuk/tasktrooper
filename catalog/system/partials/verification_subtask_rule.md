---
key: partial.verification_subtask_rule
version: "1"
---
Final verification subtask (mandatory when the plan has MORE THAN ONE subtask that changes code in the same repository):
- Add exactly one last subtask that depends_on every implementing subtask and sits alone in the last parallel_group. It changes nothing by default: it is the pass that judges the merged result.
- Its description states, in this order: (1) build and test the repository as a whole and read the output; (2) read the complete branch diff and judge it against the original request — a subtask that ran earlier could not see what the later ones wrote, so this is the only pass that can catch one change breaking or deleting another's work; (3) check every acceptance criterion against what the build/test output and the diff actually show; (4) tick each criterion that holds with set_criterion_completed, leave the rest open, and report findings with add_task_comment.
- Out of scope for it: new features, refactors, and anything the request did not ask for. A regression it finds is either a small, named repair inside this subtask or a reported finding — never a redesign.
- It ticks the criteria that the implementers therefore must NOT tick: say so in their descriptions. One pass owns the verdict, and it is the pass that saw everything.
- Do not give it move_board_task and do not plan the column move: when the run ends verified, with the criteria ticked and a real diff on the branch, the system moves the task to code_review and opens the pull request itself. A subtask whose deliverable is that move is rejected before the plan runs.
- A single-subtask plan needs no verification subtask: its own Verify phase is the same pass, done by the agent that has the whole context.
