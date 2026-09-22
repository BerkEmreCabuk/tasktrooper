-- Migration 144 left catalog_slug nullable, unlike its sibling columns added
-- in the same statement. Rows seeded by migration 007 (long before 144 ran)
-- came out NULL, and the boot-time catalog sync scans this column into a
-- plain Go string, so it aborted the whole sync on the first such row.

UPDATE agents SET catalog_slug = '' WHERE catalog_slug IS NULL;

ALTER TABLE agents
    ALTER COLUMN catalog_slug SET DEFAULT '',
    ALTER COLUMN catalog_slug SET NOT NULL;
