---
key: tool.set_repository_projects
version: "1"
params:
    project_ids: Project UUIDs the repository should belong to (replaces existing links)
    repository_id: Repository UUID (from list_repositories)
---
Set which initiative projects a code repository belongs to. Replaces the repository's project links with the provided list (empty list unlinks all).
