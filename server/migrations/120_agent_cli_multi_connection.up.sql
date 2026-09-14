-- agent_cli_connection stops being a singleton: a tenant may now have several
-- local agent CLIs connected at once (claude_code, cursor_agent, antigravity,
-- opencode), one per row, keyed by flavor instead of by a BOOLEAN(singleton).
--
-- Why the singleton existed and why it no longer has to: it enforced "at most
-- one CLI is connected", reasoned from "the settings page must be able to
-- answer which CLI runs my board". That question no longer has one answer to
-- give — each AGENT already names its own provider (agents.provider_type),
-- so "which CLI runs this board" was always really "which CLI runs THIS
-- agent's tasks", answered per agent already. A tenant with a Claude Code
-- agent and a Cursor agent gained nothing from being forced to disconnect one
-- to use the other; connecting both now lets each agent actually run.
--
-- Multiple connected flavors do not collide on disk: application/agentfs
-- materialises a TASK's catalog into that task's own workspace at dispatch
-- (application/board/runner.go), never into a shared directory two flavors
-- would both write. The connect-time snapshot this table's CatalogPath points
-- at is likewise already namespaced per flavor
-- (<workspaceRoot>/agent-cli/<flavor>), so two snapshots living side by side
-- were never the same files.
ALTER TABLE agent_cli_connection DROP CONSTRAINT agent_cli_connection_pkey;
ALTER TABLE agent_cli_connection DROP COLUMN singleton;
ALTER TABLE agent_cli_connection ADD CONSTRAINT agent_cli_connection_pkey PRIMARY KEY (tenant_id, flavor);
