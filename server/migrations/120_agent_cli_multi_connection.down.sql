-- Reverses 120.up.sql. If more than one flavor is connected per tenant at the
-- point this runs, only the most recently connected one survives — a
-- singleton table cannot represent "several", so a rollback back onto it is a
-- one-way pick, done explicitly here rather than left to fail the PRIMARY KEY
-- add below on the first tenant with two rows.
DELETE FROM agent_cli_connection a
USING agent_cli_connection b
WHERE a.tenant_id = b.tenant_id
  AND a.connected_at < b.connected_at;

ALTER TABLE agent_cli_connection DROP CONSTRAINT agent_cli_connection_pkey;
ALTER TABLE agent_cli_connection ADD COLUMN singleton BOOLEAN NOT NULL DEFAULT TRUE CHECK (singleton);
ALTER TABLE agent_cli_connection ADD CONSTRAINT agent_cli_connection_pkey PRIMARY KEY (tenant_id, singleton);
