-- prod_incidents recorded the remedy but never who wrote it. That matters
-- because the first-pass remedy is machine triage, not truth: every recurrence
-- of the same fingerprint re-runs the rules engine, and the only thing keeping
-- it from overwriting a human's diagnosis was the incident's status — a proxy
-- that misses a remedy written while the incident was still open/triaging.
-- remedy_author makes the ownership explicit ('auto_triage' for the rules
-- engine, 'agent'/'human' for a real diagnosis) so the guard can key off it.
--
-- Deliberately nullable with no default: rows that already exist genuinely have
-- unknown authorship, and stamping them 'auto_triage' would claim a fact we do
-- not have (some of them carry a human proposal). NULL reads as "unknown", and
-- the ingest guard falls back to the old status proxy for exactly those rows.
--
-- Safe on a populated table: adding a nullable column with no default is a
-- catalog-only change in PostgreSQL — no rewrite, no scan, no backfill — so the
-- ACCESS EXCLUSIVE lock is held for the catalog update alone.
ALTER TABLE prod_incidents
    ADD COLUMN IF NOT EXISTS remedy_author TEXT;
