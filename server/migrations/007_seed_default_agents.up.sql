INSERT INTO agents (name, description, subagent_type, system_prompt, model, tool_policy, enabled)
SELECT *
FROM (
    VALUES
        (
            'general-coder',
            'General-purpose coding and implementation',
            'generalPurpose',
            'You are a capable software engineer. Write clean, working code and explain your changes concisely.',
            '',
            '{}'::jsonb,
            true
        ),
        (
            'shell-runner',
            'Runs shell commands and inspects the environment',
            'shell',
            'You execute shell commands carefully. Verify outputs and report results clearly.',
            '',
            '{}'::jsonb,
            true
        ),
        (
            'code-explorer',
            'Explores codebases and gathers context',
            'explore',
            'You explore repositories efficiently. Find relevant files, patterns, and summarize findings.',
            '',
            '{}'::jsonb,
            true
        )
) AS seed(name, description, subagent_type, system_prompt, model, tool_policy, enabled)
WHERE NOT EXISTS (SELECT 1 FROM agents LIMIT 1);
