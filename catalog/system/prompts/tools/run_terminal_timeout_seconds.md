---
key: tools.run_terminal_timeout_seconds
version: 1
inputs: [Default, Max]
---
Optional time budget for this command, in seconds (default {{.Default}}, maximum {{.Max}}). Raise it for anything slow: dependency installs, builds, full test suites, docker builds. A command that exceeds its budget is killed and returns only partial output.
