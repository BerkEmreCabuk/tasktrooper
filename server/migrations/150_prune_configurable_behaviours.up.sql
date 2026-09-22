-- Six behaviours stop being per-stage settings. The rule they failed: a
-- behaviour earns a checkbox only when it is a static check code performs and
-- the agent cannot be trusted to do itself. Guidance belongs in the stage's
-- instructions; a decision with one right answer per stage kind belongs in
-- the engine, not in a workflow row.
--
-- Deleted outright — nothing replaces them:
--   no_read_file        never held: read_file was stripped while grep_code,
--                       expand_symbol_context and get_symbol_skeleton kept
--                       returning the same source. A hint dressed as a rule.
--   review_only         enforced nothing. Its one real effect (a wider diff
--                       budget for reviewers) now follows from the stage kind.
--
-- Deleted and replaced by a derived rule in code:
--   hold_for_human_approval only ever meant code_review, and holding pm_uat or
--                           human_uat made humans approve twice
--                           (board/review_gate.go).
--   show_all_criteria       a judging stage needs the settled criteria, a
--                           working stage needs the open ones plus the nudge
--                           to tick them (board/runner.go).
--   criteria_sweep          chasing your own open criteria is what a queue/
--                           work/rework stage is for (board/criteria_sweep.go).

UPDATE workflow_stages
SET behaviours = COALESCE(
    (SELECT jsonb_agg(elem) FROM jsonb_array_elements(behaviours) elem
     WHERE elem->>'key' NOT IN (
         'no_read_file', 'review_only',
         'hold_for_human_approval', 'show_all_criteria', 'criteria_sweep'
     )),
    '[]'::jsonb
)
WHERE behaviours @> '[{"key":"no_read_file"}]'
   OR behaviours @> '[{"key":"review_only"}]'
   OR behaviours @> '[{"key":"hold_for_human_approval"}]'
   OR behaviours @> '[{"key":"show_all_criteria"}]'
   OR behaviours @> '[{"key":"criteria_sweep"}]';

-- dispatch_suspended stays a real flag: an analiz card IS dispatched in done
-- so its architect can decompose it, while a coding type is finished there.
-- Only its dead rows go — the dispatcher hardcodes these three columns and
-- never reads the flag on them.
UPDATE workflow_stages
SET behaviours = COALESCE(
    (SELECT jsonb_agg(elem) FROM jsonb_array_elements(behaviours) elem
     WHERE elem->>'key' <> 'dispatch_suspended'),
    '[]'::jsonb
)
WHERE column_slug IN ('backlog', 'blocked', 'released')
  AND behaviours @> '[{"key":"dispatch_suspended"}]';
