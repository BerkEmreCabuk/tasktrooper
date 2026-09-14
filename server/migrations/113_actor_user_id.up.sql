-- Every board_events row today only says WHO KIND acted (actor: "human" in the
-- payload) — control.go and the task.moved path never recorded WHICH human.
-- internalauth.Verify only proves "this pod's tenant", so there was nowhere to
-- get a user identity from until the gateway started forwarding one.
--
-- actor_user_id is that user's Firebase UID, read off the signed
-- X-Internal-Actor header (same wire contract as X-Internal-Auth). It is
-- nullable on purpose: self-hosted/desktop runs have no gateway in front of
-- them and so never carry the header, and a human's own agent-run comments and
-- system-authored rows never carry a user identity either.
ALTER TABLE board_events ADD COLUMN IF NOT EXISTS actor_user_id TEXT;
ALTER TABLE task_comments ADD COLUMN IF NOT EXISTS actor_user_id TEXT;
