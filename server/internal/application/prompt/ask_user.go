package prompt

// AskUserQuestionJSONShape is the ask_user question shape embedded verbatim
// into orchestrator/schemas.go's JSON schemas (see schemas_test.go) — kept
// here rather than in catalog/system since it is data shape, not prose an
// LLM reads as instructions on its own.
const AskUserQuestionJSONShape = `"id": "string",
    "prompt": "string",
    "allow_multiple": false,
    "options": [{"id": "string", "label": "string"}]`
