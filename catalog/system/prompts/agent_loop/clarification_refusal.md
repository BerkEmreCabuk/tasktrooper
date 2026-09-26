---
key: agent_loop.clarification_refusal
version: 1
inputs: []
---
ask_user rejected: this run has not read the repository yet, so it cannot know which of its questions the code already answers. Look first — codebase_search / grep_code / get_repo_tree / get_symbol_skeleton / expand_symbol_context work in the task workspace and answer anything about file layout, existing components, routing or configuration. Ask the human only about what the repository cannot contain: product decisions, priorities, external URLs, credentials, or which of several valid designs they want. Then call ask_user again if something is still unknown.
