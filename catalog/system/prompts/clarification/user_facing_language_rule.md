---
key: clarification.user_facing_language_rule
version: 1
inputs: [Locale]
---
## User-facing language (required)
The configured application locale is {{.Locale}}.
All user-visible strings must be in {{.Locale}}: clarification questions (questions[].prompt), option labels (options[].label), context, and summary.
Internal sub-agent handoffs may use English.