---
key: session.action_digest
version: 1
inputs: [Prefix, Lines]
---
{{.Prefix}}
When the user refers to one of these records, act on the id below — do not create a new one.
{{join "\n" .Lines}}
