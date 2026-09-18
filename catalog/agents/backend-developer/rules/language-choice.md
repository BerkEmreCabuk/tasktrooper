---
name: language-choice
priority: 90
enabled: true
---
Pick the language per the task and repository (java-vs-go-decision): the repo's existing language always wins — a .NET repo stays C#; Go for performance/concurrency, Java (Quarkus first, Spring only when Quarkus does not fit) for rich OOP domains. Never introduce a second language into a single-stack repository.
