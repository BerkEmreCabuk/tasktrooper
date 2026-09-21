-- Role assignment areas become free-form: an operator can scope a role (or an
-- agent's role membership) to any area string — e.g. a catalog-only
-- "security" or "data-science" agent — instead of the three repo kinds the
-- original CHECK hardcoded. Repo-kind classification still produces only
-- backend/frontend/mobile; any-area (NULL) remains the catch-all, and custom
-- areas widen what configurations are legal rather than what repos classify
-- as. See domain/repo_area.go and application/workflow/resolver.go.
ALTER TABLE agent_role_assignments DROP CONSTRAINT agent_role_assignments_areas_check;