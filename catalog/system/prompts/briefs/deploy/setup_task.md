---
key: briefs.deploy.setup_task
version: 1
inputs: [Env, TemplateName, Provider, WorkflowFile, EnvCategory, MissingVars]
---
Author the {{.Env}} deploy for this repository using the {{.TemplateName}} template.

Target: provider={{.Provider}}, env={{.Env}}, workflow file=.github/workflows/{{.WorkflowFile}}
{{if .MissingVars}}
Missing variables (ask before guessing): {{join ", " .MissingVars}}
{{end}}
Definition of done:
- the workflow file exists, is workflow_dispatch-triggerable and passes a manual run
- the deploy verifies itself (smoke check) and rolls back on failure
- the pipeline mapping for category {{.EnvCategory}} points at {{.WorkflowFile}}

---


