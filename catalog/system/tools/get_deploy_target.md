---
key: tool.get_deploy_target
version: "1"
params:
    env: stage | preprod | prod. Omit to list every configured environment.
    repository_id: Repository UUID (the repository_id in your task snapshot). Omit to use the repository of the task this run is working on.
---
Read how a repository ships to an environment: provider, variables, health URL, and the deploy recipe rendered with those values.
