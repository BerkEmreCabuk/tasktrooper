---
key: context.cleared_tool_result
version: 1
inputs: [ToolName]
---
[earlier {{if .ToolName}}{{.ToolName}}{{else}}tool{{end}} output cleared to save context — re-run the call if you need it again]