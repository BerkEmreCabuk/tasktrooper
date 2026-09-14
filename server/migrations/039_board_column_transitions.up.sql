-- Jira-style workflow: allowed transitions between board columns.
CREATE TABLE IF NOT EXISTS board_column_transitions (
    from_slug TEXT NOT NULL,
    to_slug   TEXT NOT NULL,
    PRIMARY KEY (from_slug, to_slug)
);
