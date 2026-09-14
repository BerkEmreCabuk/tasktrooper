-- A production deploy was dispatched on the task's STATE ("it reached done"),
-- never on the identity of the code it was about to ship. Between the sign-off
-- and the release the task branch can move — a follow-up agent run commits
-- more work, someone force-pushes — and the deploy shipped commits that no
-- reviewer, QA round or PM ever saw.
--
-- verified_sha records the commit the task carried when it reached done. The
-- release gate re-resolves the branch at dispatch time and compares; empty
-- (every pre-existing row, and any task whose sign-off was withdrawn) blocks
-- the release instead of passing it, because a check that succeeds when its
-- input is missing is not a check.
ALTER TABLE board_tasks
    ADD COLUMN IF NOT EXISTS verified_sha TEXT NOT NULL DEFAULT '';
