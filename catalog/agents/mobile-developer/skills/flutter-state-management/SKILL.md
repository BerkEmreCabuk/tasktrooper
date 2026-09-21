---
name: flutter-state-management
category: architecture
description: Use when a Flutter screen needs app state or data - separate presentation from state, keep business logic out of widgets, and follow the app's existing state solution
tech_stack: Flutter
---
# Flutter State Management

## Overview

Widgets render state; something else owns it. Keeping business logic and data out of the widget tree is what makes Flutter screens testable and rebuilds cheap.

**Core principle:** Follow the app's existing state solution (Riverpod, Bloc, Provider, etc.) — do not introduce a second one. The pattern matters more than the library.

## Rules (library-agnostic)

- **One source of truth per piece of state**, owned by a controller/notifier/bloc — never duplicated across widgets via `setState`.
- **Presentation ↔ state via a thin interface:** the widget watches state and sends events/intents; it never contains the business rule or the network call.
- **`StatefulWidget` is for ephemeral UI only** (animation controllers, focus, scroll) — anything that outlives a rebuild or comes from a service is app state.
- **Handle all async states explicitly:** loading, data, empty, error. A `FutureBuilder`/`AsyncValue` that only renders the happy path ships a spinner-forever bug.
- **Dispose** controllers/subscriptions to avoid leaks.
- **Immutable state objects** (copyWith) so rebuilds are diff-friendly and predictable.

## Worked Example (pattern, not a specific lib)

```dart
// state owner: no widgets, pure logic — unit-testable without pumping a widget
class TaskListController extends StateNotifier<AsyncValue<List<Task>>> {
  TaskListController(this._repo) : super(const AsyncValue.loading());
  final TaskRepository _repo;

  Future<void> load() async {
    state = const AsyncValue.loading();
    try { state = AsyncValue.data(await _repo.list()); }
    catch (e, st) { state = AsyncValue.error(e, st); }
  }
}

// widget: watches state, renders each case, sends intents — no logic
ref.watch(taskListProvider).when(
  loading: () => const AppSpinner(),
  error:   (e, _) => AppErrorView(message: e.toString(), onRetry: controller.load),
  data:    (tasks) => TaskList(tasks: tasks),
);
```

## Common Mistakes

- Business logic / http calls inside `build` or `initState`.
- `setState` juggling data that came from an API.
- Introducing a new state library alongside the app's existing one.
- Ignoring the error/empty branch of an async state.
- Leaked controllers (no `dispose`).

## Red Flags

- A widget imports an http client or repository directly.
- The same state mirrored in two widgets.
- An async view with no error handling.
