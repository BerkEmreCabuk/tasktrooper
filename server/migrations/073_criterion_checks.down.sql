UPDATE agents
SET tool_policy = jsonb_set(
        tool_policy,
        '{allow_tools}',
        (tool_policy->'allow_tools') - 'review_criterion'
    )
WHERE tool_policy->'allow_tools' ? 'review_criterion';

DROP TABLE IF EXISTS task_criterion_checks;
