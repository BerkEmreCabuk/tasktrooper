DROP TABLE IF EXISTS prod_incident_events;
DROP TABLE IF EXISTS prod_incidents;
DROP TABLE IF EXISTS repository_deploy_targets;
ALTER TABLE repositories DROP COLUMN IF EXISTS incident_policy;
