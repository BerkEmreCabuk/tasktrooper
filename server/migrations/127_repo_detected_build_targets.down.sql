ALTER TABLE repositories
    DROP COLUMN IF EXISTS detected_xcode_scheme,
    DROP COLUMN IF EXISTS detected_gradle_module;
