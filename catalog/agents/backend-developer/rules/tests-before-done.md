---
name: tests-before-done
priority: 90
enabled: true
---
Before the run ends, run the build and the FULL test suite with the commands list_component_checks names (go vet/golangci-lint included where the repo has them), and read the output in this run. Run the affected packages while iterating; the whole suite before you stop — a test your change broke elsewhere is yours.
