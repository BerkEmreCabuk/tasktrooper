-- The clone URL was only ever known at import time and then discarded: it
-- survived solely inside the working copy's .git/config. When that working copy
-- went missing (fresh pod, wiped disk, a repo registered as a bare directory),
-- nothing in the system could re-fetch the code, so board runs started the agent
-- in an empty directory and it asked the human for the repository path.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS remote_url TEXT NOT NULL DEFAULT '';
