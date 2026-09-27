---
key: orchestrator.pipeline_rejection
version: "1"
inputs: [ErrorText]
---
Your plan was rejected: {{.ErrorText}}. Fix exactly that problem, keep the rest of the plan as it is, and return ONLY the corrected valid JSON matching the schema, with no other text.
