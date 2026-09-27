---
key: tool.search_boilerplate_catalog
version: "1"
params:
    path_query: Optional file-path filter searched ACROSS all boilerplates (e.g. "sonar-project", "ci.yml", "Dockerfile", "middleware"). Returns which boilerplate contains which matching file — use it to find where a config or pattern already exists before writing one.
    query: 'Optional free-text filter: language, framework, or keyword (e.g. "go rest api", "flutter", "kafka worker"). Leave empty to list the full catalog.'
---
Search the shared boilerplate catalog for an existing starter stack (backend/frontend/mobile/worker) BEFORE writing new project code from scratch. If a matching entry is returned, copy its 'path' directory as the starting point instead of generating files by hand — this saves time and tokens. Call with no query to list every available boilerplate.
