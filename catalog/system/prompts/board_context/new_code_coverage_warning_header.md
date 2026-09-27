---
key: board_context.new_code_coverage_warning_header
version: 1
inputs: [Marker, Percent, Covered, Total, Threshold]
---
{{.Marker}} new-code coverage {{.Percent}}% ({{.Covered}}/{{.Total}} changed lines) is below the {{.Threshold}}% this change should leave behind.
