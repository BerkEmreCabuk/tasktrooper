---
key: tool.load_deploy_template
version: "1"
params:
    template: Template id (gcp-cloud-run) or provider (gcp_cloud_run)
---
Load a deploy recipe in full: the workflow YAML to write, the secrets it needs, the smoke check and the rollback. Follow it instead of writing a deploy from memory.
