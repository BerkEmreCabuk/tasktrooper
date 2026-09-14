DELETE FROM app_settings WHERE key = 'active_llm_provider';
DROP TABLE IF EXISTS llm_provider_secrets;
DROP TABLE IF EXISTS llm_provider_configs;
