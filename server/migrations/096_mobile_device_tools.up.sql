-- Mobile device tools: the shared Android phone behind the mobile_* tool set.
--
-- Two things land here.
--
-- 1. blocked_resource on board_tasks. The board already knows two ways to park
--    a task: waiting on a human answer (blocked_session_id) and stopped by a
--    human (neither). A third arrives with the device — waiting on a shared
--    piece of hardware another run is holding — and it needs its own marker
--    because its release is neither an answer nor a drag: a sweeper claims it
--    when the phone frees up. Without the column that sweep would have to
--    guess which blocked tasks were waiting on the device and would resume
--    tasks that are waiting on a person.
--
-- 2. app_package / app_url on deploy targets. The device tools can only open a
--    package a human registered, so the registration needs somewhere to live;
--    the deploy target is where the equivalent web fact (base_url) already is,
--    which keeps "where does this repository's stage build live" one answer
--    instead of two.
ALTER TABLE board_tasks
    ADD COLUMN IF NOT EXISTS blocked_resource TEXT;

-- Partial: the sweep asks only for rows that have one, and parked-on-a-resource
-- is a handful of rows against a table that is mostly not blocked at all.
CREATE INDEX IF NOT EXISTS idx_board_tasks_blocked_resource
    ON board_tasks (blocked_resource, blocked_at)
    WHERE blocked_resource IS NOT NULL;

ALTER TABLE repository_deploy_targets
    ADD COLUMN IF NOT EXISTS app_package TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS app_url     TEXT NOT NULL DEFAULT '';

-- Tool policies are written on agent CREATE only (admin customizations must
-- survive restarts), so existing installs need the tools appended: one guarded
-- UPDATE per tool, skipped when the policy already carries it. Same pattern as
-- 073/074/076/082/087/091.
--
-- qa-agent and mobile-developer are the two roles that test the app on a device.
-- product-manager gets them too, for the same reason it holds the browser set:
-- PM UAT is a human-facing sign-off that has to be able to look at the thing.
UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_launch_app"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_launch_app';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_screenshot"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_screenshot';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_read_ui"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_read_ui';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_tap"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_tap';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_type_text"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_type_text';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_swipe"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_swipe';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_wait_for"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_wait_for';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_press_button"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_press_button';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_rotate"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_rotate';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_unlock_device"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_unlock_device';

UPDATE agents SET tool_policy = jsonb_set(tool_policy, '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) || '["mobile_release_device"]'::jsonb)
WHERE name IN ('mobile-developer', 'qa-agent', 'product-manager')
  AND NOT COALESCE(tool_policy->'allow_tools', '[]'::jsonb) ? 'mobile_release_device';
