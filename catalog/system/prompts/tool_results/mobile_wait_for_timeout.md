---
key: tool_results.mobile_wait_for_timeout
version: 1
inputs: [Describe, TimeoutSeconds]
---
{{.Describe}} did not appear within {{.TimeoutSeconds}}s — read the screen with mobile_read_ui to see what is actually there
