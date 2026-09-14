ALTER TABLE repository_deploy_targets DROP CONSTRAINT repository_deploy_targets_repository_id_env_key;
ALTER TABLE repository_deploy_targets ADD CONSTRAINT repository_deploy_targets_repository_id_env_key
    UNIQUE (tenant_id, repository_id, env);
ALTER TABLE repository_deploy_targets DROP COLUMN sub_project_path;

ALTER TABLE repository_pipeline_jobs DROP CONSTRAINT repository_pipeline_jobs_repository_id_sub_repo_kind_catego_key;
ALTER TABLE repository_pipeline_jobs ADD CONSTRAINT repository_pipeline_jobs_repository_id_sub_repo_kind_catego_key
    UNIQUE (tenant_id, repository_id, sub_repo_kind, category);
ALTER TABLE repository_pipeline_jobs DROP COLUMN sub_project_path;

ALTER TABLE repository_profile_sections DROP CONSTRAINT repository_profile_sections_repository_id_section_key;
ALTER TABLE repository_profile_sections ADD CONSTRAINT repository_profile_sections_repository_id_section_key
    UNIQUE (tenant_id, repository_id, section);
ALTER TABLE repository_profile_sections DROP COLUMN sub_project_path;

ALTER TABLE repository_profile_proposals DROP CONSTRAINT repository_profile_proposals_repository_id_field_slot_key;
ALTER TABLE repository_profile_proposals ADD CONSTRAINT repository_profile_proposals_repository_id_field_slot_key
    UNIQUE (tenant_id, repository_id, field, slot);
ALTER TABLE repository_profile_proposals DROP COLUMN sub_project_path;
