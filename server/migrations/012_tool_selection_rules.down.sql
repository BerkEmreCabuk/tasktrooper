UPDATE orchestrator_rules
SET content = 'For tasks that create, read, or modify files in the session workspace, assign agents that can use run_terminal or MCP filesystem tools. Add subtask_rules telling agents not to use web_search for basic local file or shell operations.'
WHERE name = 'local-tools-first';
