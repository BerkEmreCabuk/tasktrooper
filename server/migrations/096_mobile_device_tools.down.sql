-- Reverses 096. The tool grants are removed from every policy that holds them,
-- not only the roles the up-migration named: an admin may have granted them
-- elsewhere in the meantime, and leaving those behind would point agents at
-- tools this build no longer registers.
UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        COALESCE(tool_policy->'allow_tools', '[]'::jsonb) - 'mobile_launch_app'
            - 'mobile_screenshot' - 'mobile_read_ui' - 'mobile_tap' - 'mobile_type_text'
            - 'mobile_swipe' - 'mobile_wait_for' - 'mobile_press_button' - 'mobile_rotate'
            - 'mobile_unlock_device' - 'mobile_release_device'
    )
WHERE tool_policy->'allow_tools' ?| array['mobile_launch_app', 'mobile_screenshot', 'mobile_read_ui',
        'mobile_tap', 'mobile_type_text', 'mobile_swipe', 'mobile_wait_for',
        'mobile_press_button', 'mobile_rotate', 'mobile_unlock_device', 'mobile_release_device'];

-- Tasks parked on a resource would otherwise be stranded: the column that says
-- what they are waiting for is about to disappear, and nothing else can tell
-- them apart from a task stopped by a human. Send them back to where they were
-- working so the reconciler picks them up.
UPDATE board_tasks
SET board_column          = COALESCE(NULLIF(blocked_origin_column, ''), 'todo'),
    blocked_origin_column = NULL,
    blocked_question      = NULL,
    blocked_at            = NULL,
    blocked_resource      = NULL,
    updated_at            = now()
WHERE blocked_resource IS NOT NULL;

DROP INDEX IF EXISTS idx_board_tasks_blocked_resource;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS blocked_resource;

ALTER TABLE repository_deploy_targets
    DROP COLUMN IF EXISTS app_package,
    DROP COLUMN IF EXISTS app_url;
