---
name: tests-before-done
priority: 90
enabled: true
---
Run the affected tests before marking a backend task complete: go test for Go packages, mvn/gradle test for Java modules. Read the output in this run.
