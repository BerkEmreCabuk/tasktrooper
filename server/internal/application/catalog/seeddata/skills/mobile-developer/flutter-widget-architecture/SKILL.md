---
name: flutter-widget-architecture
category: architecture
description: Use when building Flutter screens - compose small stateless widgets, keep layout declarative, separate presentation from business logic, and use the theme not hardcoded styles
tech_stack: Flutter
---

# Flutter Widget Architecture

## Overview

Flutter UI is a tree of widgets. The quality difference is in decomposition: small, focused, mostly-stateless widgets that read like the UI they render, with business logic pushed out of the widget tree.

**Core principle:** Widgets describe what the UI looks like given state. They do not fetch data, hold business rules, or talk to the network — that lives in state/services (see flutter-state-management).

## Rules

- **Prefer `StatelessWidget`.** Reach for `StatefulWidget` only for truly local, ephemeral UI state (an animation controller, a text field's focus). App/business state lives in your state management layer.
- **Small widgets over deep `build` methods.** A `build` longer than ~40 lines or nested 5+ deep is a set of extract-widget opportunities. Extract named widgets, not `Widget _buildX()` helper methods (named widgets get their own rebuild scope and are testable).
- **`const` constructors everywhere possible** — a `const` widget subtree is skipped on rebuild. This is the cheapest performance win in Flutter.
- **Theme, not hardcoded styles.** Colors, text styles, spacing come from `Theme.of(context)` / a design-token file — never scattered `Color(0xFF...)` and magic paddings. This is the Flutter equivalent of "no custom CSS."
- **Keys** only when you need identity across rebuilds (reorderable lists) — don't sprinkle them.

## Worked Example

```dart
// ❌ one giant build, hardcoded style, logic in the widget
Widget build(BuildContext context) {
  return Container(padding: EdgeInsets.all(16), color: Color(0xFF2196F3),
    child: Column(children: [ Text(task.title, style: TextStyle(fontSize: 18)), /* 60 more lines */ ]));
}

// ✅ composed from small const widgets, themed, logic elsewhere
class TaskCard extends StatelessWidget {
  const TaskCard({super.key, required this.task});
  final Task task;
  @override
  Widget build(BuildContext context) => Card(
    child: Padding(
      padding: const EdgeInsets.all(16),
      child: Column(children: [
        TaskTitle(title: task.title),        // extracted, testable atom
        TaskStatusChip(status: task.status), // extracted molecule
      ]),
    ),
  );
}
```

## Common Mistakes

- `StatefulWidget` holding data that came from an API — that's app state, lift it out.
- `Widget _buildHeader()` helper methods instead of real widget classes.
- Hardcoded colors/sizes instead of the theme.
- Missing `const` on static subtrees → needless rebuilds.

## Red Flags

- A `build` method you scroll to read.
- `setState` mutating something a service should own.
- `Color(0xFF...)` / magic paddings outside the theme file.
