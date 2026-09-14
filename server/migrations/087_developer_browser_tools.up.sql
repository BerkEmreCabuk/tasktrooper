-- Developers get the browser tools.
--
-- A developer could prove a build compiled and nothing more: browser_* belonged
-- to qa-agent and product-manager, so a UI task reached code_review having never
-- been rendered. The capability was there the whole time — run_terminal can
-- start a dev server in the same pod, and the browser guard already allows
-- 127.0.0.1 for exactly that flow — only the tool policy was missing.
--
-- Tool policies are written on agent CREATE only (admin customizations must
-- survive restarts), so existing installs need the tools appended: one guarded
-- UPDATE per tool, skipped when the policy already carries it, so re-running
-- never appends a duplicate. Same pattern as 073/074/076/082.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_navigate"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_navigate';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_screenshot"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_screenshot';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_click"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_click';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_fill"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_fill';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_read_dom"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_read_dom';

UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_wait_for"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_wait_for';
