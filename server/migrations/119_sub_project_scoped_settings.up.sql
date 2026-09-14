-- A monorepo's sub-project (domain.RepoSubProject, the sub_projects JSONB
-- list on repositories) has been a path+kind pair with nothing else of its
-- own: one deploy target, one pipeline job mapping and one project profile
-- served the whole repository regardless of how many sub-projects it held,
-- so two sub-projects of the same kind silently shared one CI job and there
-- was nowhere to record, say, the frontend's stage URL separately from the
-- backend's.
--
-- sub_project_path threads the same identifier the human already curates in
-- sub_projects ("" = the repository itself, unchanged behavior for every
-- existing row and every non-monorepo repository) through the three tables
-- that need their own answer per sub-project. This is the same shape
-- repository_pipeline_jobs.sub_repo_kind already proved out in migration 046
-- — a second key dimension alongside repository_id — applied to the other
-- two settings tables and refined here from "which kind" to "which actual
-- sub-project", since two sub-projects can share a kind.
ALTER TABLE repository_deploy_targets ADD COLUMN sub_project_path TEXT NOT NULL DEFAULT '';
ALTER TABLE repository_deploy_targets DROP CONSTRAINT repository_deploy_targets_repository_id_env_key;
ALTER TABLE repository_deploy_targets ADD CONSTRAINT repository_deploy_targets_repository_id_env_key
    UNIQUE (tenant_id, repository_id, sub_project_path, env);

ALTER TABLE repository_pipeline_jobs ADD COLUMN sub_project_path TEXT NOT NULL DEFAULT '';
ALTER TABLE repository_pipeline_jobs DROP CONSTRAINT repository_pipeline_jobs_repository_id_sub_repo_kind_catego_key;
ALTER TABLE repository_pipeline_jobs ADD CONSTRAINT repository_pipeline_jobs_repository_id_sub_repo_kind_catego_key
    UNIQUE (tenant_id, repository_id, sub_project_path, sub_repo_kind, category);

ALTER TABLE repository_profile_sections ADD COLUMN sub_project_path TEXT NOT NULL DEFAULT '';
ALTER TABLE repository_profile_sections DROP CONSTRAINT repository_profile_sections_repository_id_section_key;
ALTER TABLE repository_profile_sections ADD CONSTRAINT repository_profile_sections_repository_id_section_key
    UNIQUE (tenant_id, repository_id, sub_project_path, section);

ALTER TABLE repository_profile_proposals ADD COLUMN sub_project_path TEXT NOT NULL DEFAULT '';
ALTER TABLE repository_profile_proposals DROP CONSTRAINT repository_profile_proposals_repository_id_field_slot_key;
ALTER TABLE repository_profile_proposals ADD CONSTRAINT repository_profile_proposals_repository_id_field_slot_key
    UNIQUE (tenant_id, repository_id, sub_project_path, field, slot);

-- "local" joins stage/preprod/prod as a fourth deploy environment. No CHECK
-- constraint exists on repository_deploy_targets.env to update — it has
-- always been free text, validated only in domain.ValidDeployEnv.
