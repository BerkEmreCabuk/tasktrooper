-- Three cleanups, confirmed by grepping every runtime call site:
--
-- 1. enforce_review_chain (workflow_stages, done/released) is defined in the
--    behaviour registry and was seeded onto done/released for every task
--    type, but nothing ever reads it — the actual review-chain enforcement
--    is review_chain_stage plus the repository's require_review_chain flag.
--    Dead weight, never wired.
--
-- 2. document_deliverable (task_types.behaviours, analiz) is the same
--    situation: defined, seeded onto analiz, never read anywhere.
--
-- 3. commit_on_finish / build_verify on backlog and blocked: the dispatcher
--    hardcodes both columns as dispatch-suspended (board/dispatcher.go), so
--    in the normal engine no run ever starts while a task sits there and
--    these two behaviours can never fire — no work happens before a task
--    reaches todo. Unlike done/released (which stay reachable through
--    merge/deploy-watch wake events and keep their behaviours), backlog and
--    blocked have no such path back into the dispatcher.

UPDATE task_types
SET behaviours = COALESCE(
    (SELECT jsonb_agg(elem) FROM jsonb_array_elements(behaviours) elem
     WHERE elem->>'key' <> 'document_deliverable'),
    '[]'::jsonb
)
WHERE behaviours @> '[{"key":"document_deliverable"}]';

UPDATE workflow_stages
SET behaviours = COALESCE(
    (SELECT jsonb_agg(elem) FROM jsonb_array_elements(behaviours) elem
     WHERE elem->>'key' <> 'enforce_review_chain')
    , '[]'::jsonb
)
WHERE behaviours @> '[{"key":"enforce_review_chain"}]';

UPDATE workflow_stages
SET behaviours = COALESCE(
    (SELECT jsonb_agg(elem) FROM jsonb_array_elements(behaviours) elem
     WHERE elem->>'key' NOT IN ('commit_on_finish', 'build_verify'))
    , '[]'::jsonb
)
WHERE column_slug IN ('backlog', 'blocked')
  AND (behaviours @> '[{"key":"commit_on_finish"}]' OR behaviours @> '[{"key":"build_verify"}]');
