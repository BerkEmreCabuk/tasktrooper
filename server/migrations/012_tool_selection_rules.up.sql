UPDATE orchestrator_rules
SET content = 'For local file or shell tasks in the workspace, assign agents with run_terminal or MCP filesystem access and add subtask_rules to avoid web_search for basic local operations. For research, biography, or external factual questions, use a single agent task with subtask_rules to use web_search; leave skill_ids empty (never put tool names in skill_ids).'
WHERE name = 'local-tools-first';
