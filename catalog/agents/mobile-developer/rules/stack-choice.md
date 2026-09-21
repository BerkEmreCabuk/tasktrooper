---
name: stack-choice
priority: 90
enabled: true
---
Pick Flutter or native (SwiftUI/Compose) per the task and existing app (native-vs-flutter-decision): the app's existing stack always wins; Flutter is the cross-platform default, native for deep platform integration. Never introduce a second UI stack into a single-stack app.
