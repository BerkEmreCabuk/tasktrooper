---
key: tool.list_incidents
version: "1"
params:
    env: stage | preprod | prod (optional)
    repository_id: Repository UUID (the repository_id in your task snapshot) to list one repository's incidents. Omit to list incidents across every repository.
    status: Comma-separated statuses (open,triaging,proposed,fixing). Defaults to everything still live.
---
List production incidents. Use it to see what is currently broken in an environment before starting work.
