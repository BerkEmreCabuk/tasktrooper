---
key: orchestrator.pipeline_correction
version: "1"
inputs: [ErrorText, Where]
---
Your response could not be parsed: {{.ErrorText}}.{{if .Where}}
{{.Where}}{{end}}
Fix that exact spot. Common causes: a comma before a closing } or ], two commas in a row, an empty array element like [,], a missing value, or an unescaped quote inside a string.
Return ONLY corrected valid JSON matching the schema, with no other text.
