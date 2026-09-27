---
key: projectmodel.tools_note
version: 1
inputs: [ShowGetProjectBrief, ShowListComponentChecks, ShowListLinks, ShowGetEnvironment, ShowQueryRuntimeLogs, ShowListRuntimeErrors]
---
{{if or .ShowGetProjectBrief .ShowListComponentChecks .ShowListLinks .ShowGetEnvironment .ShowQueryRuntimeLogs .ShowListRuntimeErrors}}## Project model (TaskTrooper)
TaskTrooper keeps a scanned model of this repository. It is not in your context: fetch what you need with these tools instead of guessing or reading it off the code:
{{if .ShowGetProjectBrief}}- `get_project_brief`: repository overview: stack, components and their build/test/lint commands, git conventions (default branch, branch naming, merge style), reference docs, where each component runs
{{end}}{{if .ShowListComponentChecks}}- `list_component_checks`: what CI runs, with the local command for each check; before you hand off, run the local commands of every required check for the components you changed
{{end}}{{if .ShowListLinks}}- `list_links`: what a component talks to and what calls it (other repositories, databases, queues, external APIs)
{{end}}{{if .ShowGetEnvironment}}- `get_environment`: a component's environments: URLs, health
{{end}}{{if .ShowQueryRuntimeLogs}}- `query_runtime_logs`: live logs of a deployed environment
{{end}}{{if .ShowListRuntimeErrors}}- `list_runtime_errors`: recent errors of a deployed environment
{{end}}{{end}}
