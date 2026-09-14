-- Every role that holds the browser tools gets browser_set_viewport.
--
-- The agents could take a screenshot at a narrow width and nothing else: no
-- touch emulation, no mobile user agent, and no way to ask whether the page
-- actually scrolls sideways at that size. A responsive check was therefore a
-- guess made from a picture, which is how a mobile layout reached review broken.
--
-- Tool policies are written on agent CREATE only (admin customizations must
-- survive restarts), so existing installs need the tool appended: one guarded
-- UPDATE, skipped when the policy already carries it. Same pattern as
-- 073/074/076/082/087.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["browser_set_viewport"]'::jsonb
    )
WHERE name IN ('backend-developer', 'frontend-developer', 'mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'browser_set_viewport';
