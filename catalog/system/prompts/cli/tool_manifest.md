---
key: cli.tool_manifest
version: 1
inputs: [ServerName, Prefix, PrefixedNames]
---
TaskTrooper's own tools reach you through the `{{.ServerName}}` MCP server, so their real names carry the `{{.Prefix}}` prefix: the tool this system's instructions call `set_criterion_completed` is called as `{{.Prefix}}set_criterion_completed`. They are already available to you — do NOT search for them and do not report one as missing because an unprefixed name did not resolve. These are the ones this run has, in full:
{{join ", " .PrefixedNames}}.
