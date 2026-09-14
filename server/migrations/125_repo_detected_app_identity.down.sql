ALTER TABLE repositories
    DROP COLUMN IF EXISTS detected_bundle_id,
    DROP COLUMN IF EXISTS detected_package_name;
